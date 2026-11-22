package tsnetlc_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rosskukulinski/tailworks/internal/tsnetlc"
	"github.com/rosskukulinski/tailworks/netd/nettest"
	"tailscale.com/client/local"
	"tailscale.com/tsnet"
)

const dialDelay = 200 * time.Millisecond

// stray leaves the kind of LocalAPI connection that held Close on real
// hosts: a connection the transport dialed for a request that no longer
// needs it (here: one given up on during a slow dial) is pooled with no
// request on it, so the server sees it as new, "active" for 5 s.
func stray(t *testing.T, ts *tsnet.Server, lc *local.Client) {
	t.Helper()
	tsnetlc.CloseIdle(ts) // the next request dials
	ctx, cancel := context.WithTimeout(context.Background(), dialDelay/4)
	defer cancel()
	if _, err := lc.StatusWithoutPeers(ctx); err == nil {
		t.Fatal("the request outlived its context")
	}
	time.Sleep(2 * dialDelay) // the dial completes, into the idle pool
}

// TestCloseDrainsLocalAPIConns (#245): tsnetlc.Close closes a node whose
// LocalAPI client left an unused connection in its pool without waiting
// out the LocalAPI server's 5 s shutdown grace. Plain ts.Close on the same
// state does wait: what tw-hostd's node did on about half of the
// `systemctl --user restart tw` runs on a real host.
func TestCloseDrainsLocalAPIConns(t *testing.T) {
	ctl, err := nettest.StartControl()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctl.Close)
	tsnetlc.SetDialDelay(dialDelay)
	t.Cleanup(func() { tsnetlc.SetDialDelay(0) })
	for _, c := range []struct {
		name     string
		close    func(*tsnet.Server) error
		min, max time.Duration
	}{
		{"ts.Close (holds)", (*tsnet.Server).Close, 4 * time.Second, time.Minute},
		{"tsnetlc.Close", tsnetlc.Close, 0, 2500 * time.Millisecond},
	} {
		ts := &tsnet.Server{Hostname: "tsnetlc", Dir: filepath.Join(t.TempDir(), "n"), ControlURL: ctl.URL, AuthKey: nettest.DummyKey,
			Ephemeral: true, Logf: func(string, ...any) {}, UserLogf: func(string, ...any) {}}
		lc, err := tsnetlc.Client(ts)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err = ts.Up(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		stray(t, ts, lc)
		st := time.Now()
		c.close(ts)
		took := time.Since(st)
		tsnetlc.Forget(ts)
		t.Logf("%s: %v", c.name, took.Round(time.Millisecond))
		if took < c.min || took > c.max {
			t.Errorf("%s took %v, want %v–%v", c.name, took.Round(time.Millisecond), c.min, c.max)
		}
	}
}
