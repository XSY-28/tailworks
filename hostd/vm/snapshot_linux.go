//go:build linux

package vm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rosskukulinski/twd/guest/proto"
	"github.com/rosskukulinski/twd/hostd/vm/helperapi"
)

// Snapshot restore (vm.Config.Snapshot; off by default — see the ADR
// draft on cross-tenant memory sharing before turning it on).
//
// The base snapshot is a pooled VM: kernel booted, guestd up, the no-tenant
// warm-up workerd run and exited, AppDir empty, waiting for its load. It
// holds no tenant code, no keys and no instance identity (every VM has the
// same guest IP, MAC and vsock CID, each in its own netns and jail; the
// per-instance identity — bundle, MAC key, entropy, boot id, clock — comes
// with the load). The helper snapshots it into a root-only directory and
// kills it; the base lives as long as hostd's holder connection.
//
// A restore is a fresh jail + netns + VMM that loads the base with its
// guest memory served lazily by the helper (userfaultfd, UFFDIO_COPY into
// private pages). The restored guest's vsock connection was reset by the
// snapshot; guestd reconnects and says "pooled", and from there it is
// claimed exactly like a pooled VM (Backend.claim).
type baseSnap struct {
	ready chan struct{}
	conn  *helperapi.Conn
	id    string
	files []string
	info  map[string]any
	err   error
}

// startBase builds the base snapshot in the background (b.base is set
// before the pool starts, so both see it).
func (b *Backend) startBase() {
	bs := b.base
	go func() {
		defer close(bs.ready)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		bs.err = b.makeBase(ctx, bs)
		if bs.err != nil {
			b.cfg.Log.Warn("microVM base snapshot failed; booting instead of restoring", "err", bs.err)
		}
	}()
}

func (b *Backend) makeBase(ctx context.Context, bs *baseSnap) error {
	st := time.Now()
	m, err := b.boot(ctx, bootOpts{name: "base", pool: true})
	if err != nil {
		return err
	}
	defer m.Stop()
	booted := time.Since(st)
	// Belt and braces: nothing of a workload (or the warm-up) in the guest.
	r, err := m.Op(ctx, proto.Msg{Op: "fsscan"})
	if err != nil {
		return err
	}
	if len(r.Found) > 0 || len(r.State) > 0 {
		return fmt.Errorf("base VM is not clean: found %v, state %v", r.Found, r.State)
	}
	if m.info.Jail == nil {
		return errors.New("base VM has no jail id")
	}
	conn, err := helperapi.Dial(b.sock)
	if err != nil {
		return err
	}
	resp, _, err := conn.Do(helperapi.Request{Op: "snapshot-base", Snapshot: &helperapi.SnapshotRequest{Of: m.info.Jail.ID}}, nil, 2*time.Minute)
	if err != nil {
		conn.Close()
		return fmt.Errorf("snapshot-base: %w", err)
	}
	bs.conn, bs.id, bs.files = conn, resp.BaseID, resp.BaseFiles
	bs.info = map[string]any{"base": resp.BaseID, "mem_bytes": resp.BaseBytes, "boot_ms": booted.Milliseconds(),
		"snapshot_ms": resp.Timings, "total_ms": time.Since(st).Milliseconds(), "hugepages": m.info.HugePages}
	b.cfg.Log.Info("microVM base snapshot ready", "info", bs.info)
	return nil
}

// baseID returns the base snapshot's ID once it is built ("" if snapshots
// are off or the base failed). wait: block until it is decided.
func (b *Backend) baseID(ctx context.Context, wait bool) string {
	bs := b.base
	if bs == nil {
		return ""
	}
	if wait {
		select {
		case <-bs.ready:
		case <-ctx.Done():
			return ""
		}
	} else {
		select {
		case <-bs.ready:
		default:
			return ""
		}
	}
	if bs.err != nil {
		return ""
	}
	return bs.id
}

// BaseSnapshot reports the base snapshot (nil if snapshots are off): its
// root-only files (for the key scan; readable when hostd is root) and how
// it was made.
func (b *Backend) BaseSnapshot(ctx context.Context) ([]string, map[string]any, error) {
	if b.base == nil {
		return nil, nil, errors.New("snapshots are off")
	}
	if b.baseID(ctx, true) == "" {
		return nil, nil, fmt.Errorf("no base snapshot: %v", b.base.err)
	}
	return b.base.files, b.base.info, nil
}

func (b *Backend) closeBase() {
	if b.base == nil {
		return
	}
	select {
	case <-b.base.ready:
	case <-time.After(2 * time.Minute):
	}
	if b.base.conn != nil {
		b.base.conn.Close() // the helper removes the base's files
	}
}
