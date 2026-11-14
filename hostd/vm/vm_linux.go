//go:build linux

package vm

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rosskukulinski/twd/guest/proto"
	"github.com/rosskukulinski/twd/hostd/sandbox"
	"github.com/rosskukulinski/twd/hostd/vm/helper"
	"github.com/rosskukulinski/twd/hostd/vm/helperapi"
	"github.com/rosskukulinski/twd/netd"
	"golang.org/x/sys/unix"
)

// Config configures the Firecracker backend.
type Config struct {
	// Helper is twd-vmhelper's socket (installed by `sudo twd host setup
	// --microvm`): the privileged netns/TAP/jailer/dm-crypt half. If it is
	// "" and this process is root, an in-process helper is used instead
	// (tests; Firecracker and Jailer name its binaries then).
	Helper      string
	Firecracker string // in-process helper only (default /usr/local/bin/firecracker)
	Jailer      string // in-process helper only (default /usr/local/bin/jailer)
	Kernel      string // uncompressed guest kernel (vmlinux)
	Rootfs      string // guest image from guest/build-image.sh (mounted read-only by every VM)
	RunDir      string // per-VM dirs: config drive, console log
	MemMiB      int    // guest memory (default 512)
	VCPUs       int    // default 2 (1 vCPU: workerd's threads contend; p50 latency ~30 ms nested)
	MTU         int    // default 1500
	BootArgs    string // kernel cmdline (default below)
	// HugePages backs guest memory with 2 MiB pages (needs free hugetlbfs
	// pages on the host: vm.nr_hugepages). Far fewer stage-2 faults, which
	// dominate workerd start-up under nested virtualization (measured in
	// Lima: InstanceStart → workerd ready 3.7 s without, 0.43 s with).
	// "auto" (the default, "") uses them when enough are free at boot.
	HugePages string // "" | "auto" | "on" | "off"
	// ReadyTimeout bounds boot → workerd healthy in the guest (default 30 s).
	ReadyTimeout time.Duration
	// Pool is the warm pool's size: pre-booted, warm VMs without tenant
	// code that Boot() claims instead of booting (see pool_linux.go). 0 =
	// auto (half the huge-page reservation's VMs, at most 2; off without
	// huge pages), < 0 = off.
	Pool int
	// PoolBudgetMiB caps the guest RAM of all of this backend's VMs for
	// refills (default: the huge-page reservation, vm.nr_hugepages × 2 MiB).
	PoolBudgetMiB int
	// Snapshot (off by default; twd-hostd -vm-snapshot): restore VMs from a
	// base snapshot (a pooled VM: warm, no tenant code, no keys) instead of
	// booting them — for pool refills, and on demand when the pool is
	// empty. Guest memory is served lazily (userfaultfd). See
	// snapshot_linux.go and the ADR draft on cross-tenant memory sharing.
	Snapshot bool
	Log      *slog.Logger
}

// DefaultBootArgs: serial console, no PCI probing, panic → reboot (which
// Firecracker turns into a VM exit), twd-guestd as init, read-only root.
const DefaultBootArgs = "console=ttyS0 reboot=k panic=1 quiet ro rootfstype=ext4 init=/sbin/twd-guestd"

// Backend is the Firecracker microVM backend.
type Backend struct {
	cfg    Config
	seq    atomic.Int64
	ownDir bool
	sock   string
	// in-process helper (root only)
	srv     *helper.Server
	srvLn   *net.UnixListener
	srvBase string
	via     string
	pool    *pool
	base    *baseSnap
}

// Available reports whether this host can run microVMs: KVM, the guest
// kernel and image, and either the helper (as a normal user) or root.
func Available(cfg Config) error {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return fmt.Errorf("no /dev/kvm: %w", err)
	}
	for _, p := range []string{cfg.Kernel, cfg.Rootfs} {
		if _, err := os.Stat(p); err != nil {
			return err
		}
	}
	if cfg.Helper != "" {
		r, err := helperapi.Ping(cfg.Helper)
		if err != nil {
			return fmt.Errorf("microVM helper at %s: %w (run `sudo twd host setup --microvm`)", cfg.Helper, err)
		}
		if r.Version != helperapi.Version {
			return fmt.Errorf("microVM helper at %s speaks protocol %d, want %d (re-run `sudo twd host setup --microvm`)", cfg.Helper, r.Version, helperapi.Version)
		}
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("the microVM tier needs the twd-vmhelper service (run `sudo twd host setup --microvm`)")
	}
	return nil
}

// New returns a Backend.
func New(cfg Config) (*Backend, error) {
	if cfg.Firecracker == "" {
		cfg.Firecracker = "/usr/local/bin/firecracker"
	}
	if cfg.Jailer == "" {
		cfg.Jailer = "/usr/local/bin/jailer"
	}
	if cfg.MemMiB == 0 {
		cfg.MemMiB = 512
	}
	if cfg.VCPUs == 0 {
		cfg.VCPUs = 2
	}
	if cfg.MTU == 0 {
		cfg.MTU = 1500
	}
	if cfg.BootArgs == "" {
		cfg.BootArgs = DefaultBootArgs
	}
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = 30 * time.Second
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if err := Available(cfg); err != nil {
		return nil, err
	}
	b := &Backend{cfg: cfg, sock: cfg.Helper, via: "twd-vmhelper " + cfg.Helper}
	if cfg.RunDir == "" {
		d, err := os.MkdirTemp("/tmp", "twd-vm-")
		if err != nil {
			return nil, err
		}
		b.cfg.RunDir, b.ownDir = d, true
	}
	if err := os.MkdirAll(b.cfg.RunDir, 0o700); err != nil {
		return nil, err
	}
	if b.sock == "" {
		if err := b.startInProcessHelper(); err != nil {
			b.Close()
			return nil, err
		}
	}
	if cfg.Snapshot {
		b.base = &baseSnap{ready: make(chan struct{})}
	}
	b.pool = newPool(b)
	if b.base != nil {
		b.startBase()
	}
	return b, nil
}

// startInProcessHelper runs the privileged helper inside this (root)
// process, on a private socket, with its own jail dir and resource names.
func (b *Backend) startInProcessHelper() error {
	inst := fmt.Sprintf("p%d", os.Getpid())
	b.srvBase = filepath.Join("/var/lib", "twd-vm-"+inst)
	srv, err := helper.New(helper.Config{Firecracker: b.cfg.Firecracker, Jailer: b.cfg.Jailer,
		JailBase: filepath.Join(b.srvBase, "jail"), StateDir: b.srvBase, UIDBase: 3910000000 + (os.Getpid()%1000)*1000,
		MaxVMs: 1000, Instance: inst, Log: b.cfg.Log.With("component", "vmhelper")})
	if err != nil {
		return fmt.Errorf("in-process microVM helper: %w", err)
	}
	b.sock = filepath.Join(b.cfg.RunDir, "helper.sock")
	os.Remove(b.sock)
	ln, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: b.sock, Net: "unixpacket"})
	if err != nil {
		return err
	}
	os.Chmod(b.sock, 0o600)
	b.srv, b.srvLn, b.via = srv, ln, "in-process helper (root)"
	go srv.Serve(ln)
	return nil
}

// Close stops the in-process helper (if any) and removes the backend's run
// directory if it created it. Running VMs must be stopped first.
func (b *Backend) Close() error {
	if b.pool != nil {
		b.pool.close()
	}
	b.closeBase()
	if b.srv != nil {
		b.srvLn.Close()
		b.srv.Close()
		os.RemoveAll(b.srvBase)
	}
	if b.ownDir {
		return os.RemoveAll(b.cfg.RunDir)
	}
	return nil
}

// Name implements sandbox.Backend.
func (b *Backend) Name() string { return "firecracker" }

// Via says how VMs are launched (helper service or in-process).
func (b *Backend) Via() string { return b.via }

var (
	gatewayIP = netip.MustParseAddr(proto.GatewayIP)
	guestIP   = netip.MustParseAddr(proto.GuestIP)
	gwMAC     = net.HardwareAddr{0x06, 0x00, 0xc0, 0xa8, 0x48, 0x01}
)

const guestMAC = "06:00:c0:a8:48:02"

// Machine is one running Firecracker microVM.
type Machine struct {
	b       *Backend
	id      string
	dir     string
	log     *slog.Logger
	conn    *helperapi.Conn
	pid     int
	vn      *vnet
	tapFD   int
	vsock   net.Listener
	ingress http.Handler
	volume  bool

	info    sandbox.Info
	start   time.Time
	done    chan struct{}
	ready   chan struct{}
	readyOK atomic.Bool
	// pool VMs: closed when the guest first says "pooled" (warm, waiting)
	pooled   chan struct{}
	pooledOK atomic.Bool
	pooledAt time.Time
	stopped  atomic.Bool
	exitErr  error
	disks    []string

	mu      sync.Mutex
	ctl     net.Conn
	enc     *json.Encoder
	pending map[int64]chan proto.Msg
	nextID  int64
	events  []proto.Msg
	lastErr string
	cleanup sync.Once
	// the helper's time from VMM exit to volume released (exit event), and
	// its other exit stats (restored VMs: uffd faults served)
	releaseMS float64
	exitStats map[string]float64
}

// Boot implements sandbox.Backend. With a warm pool, it claims a pooled VM
// (booted earlier, warm, holding no tenant code) and injects the workload
// into it; otherwise (pool off or empty) it cold-boots a VM with the
// workload on its config drive.
func (b *Backend) Boot(ctx context.Context, s sandbox.Spec) (sandbox.VM, error) {
	if b.pool != nil {
		m := b.pool.take()
		b.pool.kick()
		if m != nil {
			vm, err := b.claim(ctx, m, s)
			if err == nil || helperapi.IsCode(err, helperapi.CodeVolumeBusy) || helperapi.IsCode(err, helperapi.CodeVolumeFenced) ||
				helperapi.IsCode(err, helperapi.CodeVolumeKey) || helperapi.IsCode(err, helperapi.CodeVolumeBad) || ctx.Err() != nil {
				return vm, err
			}
			// The pooled VM failed (it is discarded): boot one cold.
			b.cfg.Log.Warn("pooled microVM failed its claim; booting cold", "spec", s.ID, "err", err)
		}
	}
	if b.base != nil {
		// Snapshots on and no pooled VM: restore one now and claim it.
		t0 := time.Now()
		if id := b.baseID(ctx, true); id != "" {
			m, err := b.boot(ctx, bootOpts{name: s.ID, restore: id, log: s.Log})
			if err == nil {
				return b.claimAt(ctx, m, s, t0, true)
			}
			b.cfg.Log.Warn("microVM restore failed; booting cold", "spec", s.ID, "err", err)
		}
	}
	m, err := b.boot(ctx, bootOpts{name: s.ID, spec: &s, volume: s.Volume, handlers: s.Handlers, log: s.Log})
	if err != nil {
		return nil, err // not a typed nil *Machine
	}
	return m, nil
}

// bootOpts: one VM boot. A cold boot carries the workload (spec, on the
// config drive); a pool boot (pool) carries none: its config drive is the
// warm-up app, its DO drive a placeholder (volume slot), its stack has no
// listeners, and it is ready when the guest says "pooled".
type bootOpts struct {
	name     string
	spec     *sandbox.Spec
	volume   *sandbox.Volume
	handlers map[uint16]http.Handler
	log      *slog.Logger
	pool     bool
	restore  string // restore this base snapshot (a pooled VM) instead of booting
}

func (b *Backend) boot(ctx context.Context, o bootOpts) (vmi *Machine, err error) {
	t0 := time.Now()
	n := b.seq.Add(1)
	id := fmt.Sprintf("%d-%s-%d", os.Getpid(), o.name, n)
	m := &Machine{b: b, id: id, dir: filepath.Join(b.cfg.RunDir, id), tapFD: -1,
		done: make(chan struct{}), ready: make(chan struct{}), pooled: make(chan struct{}), pending: map[int64]chan proto.Msg{}}
	m.info.Stages = map[string]float64{}
	m.info.Source = "cold"
	if o.pool {
		m.info.Source = "pool"
	}
	if o.restore != "" {
		m.info.Source, o.pool = "restore", true
	}
	stage := func(name string) { m.info.Stages[name] = float64(time.Since(t0).Microseconds()) / 1000 }
	m.log = b.cfg.Log.With("vm", id)
	if o.log != nil {
		m.log = o.log.With("vm", id)
	}
	defer func() {
		if err != nil {
			m.teardown()
		}
	}()
	os.RemoveAll(m.dir)
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return nil, err
	}
	// 1. Config drive: the instance dir + guest.json, as a raw tar (a pool
	// VM's: the warm-up app, no tenant code).
	drive := filepath.Join(m.dir, "config.tar")
	if o.pool { // (a restore uses the base's own; the jail needs the file)
		err = writePoolDrive(drive, b.cfg.MTU)
	} else {
		err = writeConfigDrive(drive, *o.spec, b.cfg.MTU)
	}
	if err != nil {
		return nil, fmt.Errorf("config drive: %w", err)
	}
	m.disks = []string{b.cfg.Rootfs, drive}
	stage("config_drive")

	// 2. The files the helper puts in the jail, as open fds.
	var files []*os.File
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	for _, p := range []string{b.cfg.Kernel, b.cfg.Rootfs, drive} {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	console, err := os.OpenFile(filepath.Join(m.dir, "console.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	files = append(files, console)
	m.info.Console = console.Name()
	req := &helperapi.StartRequest{Name: o.name, MemMiB: b.cfg.MemMiB, VCPUs: b.cfg.VCPUs, HugePages: b.cfg.HugePages,
		BootArgs: b.cfg.BootArgs, MTU: b.cfg.MTU, GuestMAC: guestMAC, GuestCID: 3, VsockPort: proto.VsockPort, VolumeSlot: o.pool && o.restore == "",
		Restore: o.restore}
	if o.pool && b.pool != nil && b.pool.hugeOnly {
		req.HugePages = "on" // a pool VM never falls back to 4 KiB pages
	}
	if v := o.volume; v != nil {
		f, format, err := openVolume(v)
		if err != nil {
			return nil, fmt.Errorf("volume %s: %w", v.Name, err)
		}
		files = append(files, f)
		req.Volume = &helperapi.Volume{Name: v.Name, Epoch: v.Epoch, Key: v.Key, Format: format}
		m.disks = append(m.disks, v.Path)
		m.volume = true
		m.info.Volume = v.Name
	}

	// 3. The helper: netns + TAPs, jail, volume, jailer → Firecracker,
	// API config, InstanceStart.
	m.conn, err = helperapi.Dial(b.sock)
	if err != nil {
		return nil, fmt.Errorf("microVM helper: %w", err)
	}
	stage("helper_dial")
	hs := time.Since(t0)
	resp, fds, err := m.conn.Do(helperapi.Request{Op: "start", Start: req}, files, 90*time.Second)
	req.Volume = nil // drop our reference to the key copy
	if err != nil {
		return nil, fmt.Errorf("microVM start: %w; console: %s", err, m.consoleTail(1500))
	}
	if len(fds) != 2 {
		for _, fd := range fds {
			unix.Close(fd)
		}
		return nil, fmt.Errorf("microVM helper returned %d fds", len(fds))
	}
	m.tapFD = fds[0]
	vf := os.NewFile(uintptr(fds[1]), "vsock")
	m.vsock, err = net.FileListener(vf)
	vf.Close()
	if err != nil {
		return nil, err
	}
	stage("helper_start")
	for k, v := range resp.Timings { // the helper's, relative to its own start
		m.info.Stages["helper."+k] = float64(hs.Microseconds())/1000 + v
	}
	m.pid = resp.PID
	m.start = time.Unix(0, resp.StartedAt)
	m.info.Stages["instance_start"] = float64(m.start.Sub(t0).Microseconds()) / 1000
	m.info.PID = resp.PID
	m.info.HugePages = resp.HugePages
	m.info.VolumeNew = resp.VolumeNew
	m.info.VolumeAttachMS = resp.Timings["volume"]
	if j := resp.Jail; j != nil {
		m.info.Jail = &sandbox.Jail{ID: j.ID, Chroot: j.Chroot, Cgroup: j.Cgroup, CPUMax: j.CPUMax, Seccomp: j.Seccomp,
			Netns: j.Netns, UID: j.UID, GID: j.GID, MemMax: j.MemMax}
	}
	m.vn, err = newVnet(m.tapFD, uint32(b.cfg.MTU), gwMAC, gatewayIP, guestIP, proto.PrefixLen, o.handlers)
	if err != nil {
		return nil, err
	}
	m.ingress = m.ingressProxy()
	stage("vnet")
	go m.acceptCtl()
	go m.watch()
	if b.pool != nil {
		b.pool.track(m)
	}

	// 4. Wait for guestd to report workerd healthy (a pool VM: warm and
	// waiting for its load).
	want, what := m.ready, "ready"
	if o.pool {
		want, what = m.pooled, "pooled"
	}
	rctx, cancel := context.WithTimeout(ctx, b.cfg.ReadyTimeout)
	defer cancel()
	select {
	case <-want:
	case <-m.done:
		return nil, fmt.Errorf("microVM exited during boot: %v", m.exitErr)
	case <-rctx.Done():
		m.mu.Lock()
		le := m.lastErr
		m.mu.Unlock()
		return nil, fmt.Errorf("microVM not %s after %v; events %v; last %s; console: %s", what, b.cfg.ReadyTimeout, m.eventTypes(), le, m.consoleTail(3000))
	}
	m.info.Total = time.Since(t0)
	stage(what)
	m.log.Info("microVM "+what, "boot_ms", m.info.Boot.Milliseconds(), "ready_ms", m.info.Ready.Milliseconds(),
		"total_ms", m.info.Total.Milliseconds(), "jail", resp.Jail != nil, "volume", m.info.Volume, "volume_ms", m.info.VolumeAttachMS)
	return m, nil
}

// claim turns a pooled VM into the workload's: attach its volume (the
// helper swaps it in behind the DO drive), serve its gateway handlers,
// inject the bundle and config over vsock (guestd reseeds, sets the clock,
// extracts, mounts), and wait for the tenant's workerd to be healthy. The
// VM is single-use: on any error it is destroyed, never returned to the
// pool.
func (b *Backend) claim(ctx context.Context, m *Machine, s sandbox.Spec) (vmi sandbox.VM, err error) {
	return b.claimAt(ctx, m, s, time.Now(), false)
}

// claimAt is claim with stage times relative to t0 (Boot()'s start: an
// on-demand restore happened in between, recorded as "restore").
func (b *Backend) claimAt(ctx context.Context, m *Machine, s sandbox.Spec, t0 time.Time, restored bool) (vmi sandbox.VM, err error) {
	restoreMS := float64(time.Since(t0).Microseconds()) / 1000
	m.mu.Lock()
	boot := m.info.Stages
	m.info.Stages = map[string]float64{}
	for k, v := range boot {
		m.info.Stages["pool."+k] = v
	}
	m.info.PoolIdle = time.Since(m.pooledAt)
	if restored {
		m.info.Stages["restore"] = restoreMS
	}
	m.mu.Unlock()
	stage := func(name string) {
		m.mu.Lock()
		m.info.Stages[name] = float64(time.Since(t0).Microseconds()) / 1000
		m.mu.Unlock()
	}
	if s.Log != nil {
		m.log = s.Log.With("vm", m.id)
	}
	defer func() {
		if err != nil {
			m.Stop()
		}
	}()
	stage("claim")
	ld := &proto.Load{UnixNano: time.Now().UnixNano(), WorkerdArgs: s.WorkerdArgs, HealthCF: s.HealthCF, Debug: s.Debug,
		Entropy: make([]byte, 64), BootID: newUUID()}
	rand.Read(ld.Entropy)
	if v := s.Volume; v != nil {
		f, format, err := openVolume(v)
		if err != nil {
			return nil, fmt.Errorf("volume %s: %w", v.Name, err)
		}
		av := &helperapi.Volume{Name: v.Name, Epoch: v.Epoch, Key: v.Key, Format: format}
		resp, _, err := m.conn.Do(helperapi.Request{Op: "attach", Attach: av}, []*os.File{f}, 90*time.Second)
		f.Close()
		av.Key = nil
		if err != nil {
			return nil, err
		}
		m.mu.Lock()
		m.volume = true
		m.disks = append(m.disks, v.Path)
		m.info.Volume, m.info.VolumeNew, m.info.VolumeAttachMS = v.Name, resp.VolumeNew, resp.Timings["volume"]+resp.Timings["patch"]
		m.mu.Unlock()
		ld.Volume, ld.VolumeSectors = proto.VolumeDev, resp.VolumeSectors
		stage("volume_attach")
	}
	if err := m.vn.serve(s.Handlers); err != nil {
		return nil, err
	}
	bundle, err := bundleTar(s.AppDir)
	if err != nil {
		return nil, fmt.Errorf("bundle: %w", err)
	}
	sum := sha256.Sum256(bundle)
	ld.Size, ld.SHA256 = int64(len(bundle)), hex.EncodeToString(sum[:])
	ictx, cancel := context.WithTimeout(ctx, b.cfg.ReadyTimeout)
	defer cancel()
	// Chunks are sent back to back and acknowledged; load goes after all.
	const chunk = 1 << 20
	errs := make(chan error, len(bundle)/chunk+1)
	nch := 0
	for off := 0; off < len(bundle); off += chunk {
		end := min(off+chunk, len(bundle))
		nch++
		go func(off, end int) {
			_, err := m.Op(ictx, proto.Msg{Op: "chunk", Offset: int64(off), Total: int64(len(bundle)), Data: bundle[off:end]})
			errs <- err
		}(off, end)
	}
	for i := 0; i < nch; i++ {
		if err := <-errs; err != nil {
			return nil, fmt.Errorf("inject: %w", err)
		}
	}
	ld.UnixNano = time.Now().UnixNano() // now, not at claim start: the guest's clock
	if _, err := m.Op(ictx, proto.Msg{Op: "load", Load: ld}); err != nil {
		return nil, fmt.Errorf("inject: %w; console: %s", err, m.consoleTail(1500))
	}
	stage("inject")
	select {
	case <-m.ready:
	case <-m.done:
		return nil, fmt.Errorf("microVM exited during claim: %v", m.exitErr)
	case <-ictx.Done():
		m.mu.Lock()
		le := m.lastErr
		m.mu.Unlock()
		return nil, fmt.Errorf("claimed microVM not ready after %v; last %s; console: %s", b.cfg.ReadyTimeout, le, m.consoleTail(3000))
	}
	stage("ready")
	m.mu.Lock()
	m.info.Total = time.Since(t0)
	m.info.Ready = m.info.Total
	m.mu.Unlock()
	m.log.Info("pooled microVM claimed", "spec", s.ID, "total_ms", m.info.Total.Milliseconds(), "idle_s", int(m.info.PoolIdle.Seconds()),
		"volume", m.info.Volume, "volume_ms", m.info.VolumeAttachMS)
	return m, nil
}

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// openVolume opens the volume image, creating it (sparse) if missing.
// format reports that it is new (or was created but never formatted).
func openVolume(v *sandbox.Volume) (*os.File, bool, error) {
	if len(v.Key) != 32 {
		return nil, false, errors.New("no volume key")
	}
	size := int64(v.SizeMiB) << 20
	if size == 0 {
		size = 1 << 30
	}
	if err := os.MkdirAll(filepath.Dir(v.Path), 0o700); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(v.Path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, false, err
	}
	if st.Size() == 0 {
		if err := f.Truncate(size); err != nil {
			f.Close()
			return nil, false, err
		}
	}
	head := make([]byte, 4096)
	n, _ := f.ReadAt(head, 0)
	blank := true
	for _, c := range head[:n] {
		if c != 0 {
			blank = false
			break
		}
	}
	return f, blank, nil
}

// watch turns the helper's exit event (or a lost helper connection) into
// the VM's exit.
func (m *Machine) watch() {
	var why string
	select {
	case ev := <-m.conn.Events():
		why = ev.Error
		m.mu.Lock()
		m.releaseMS = ev.Timings["volume_release"]
		m.exitStats = ev.Timings
		m.mu.Unlock()
	case <-m.conn.Closed():
		why = "helper connection closed"
	}
	if !m.stopped.Load() {
		if why == "" {
			why = "killed"
		}
		m.exitErr = fmt.Errorf("firecracker exited: %s; console: %s", why, m.consoleTail(1500))
		m.log.Warn("microVM exited", "why", why)
	}
	m.teardown()
	close(m.done)
}

func (m *Machine) consoleTail(n int) string {
	b, _ := os.ReadFile(filepath.Join(m.dir, "console.log"))
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return netd.Redact(string(bytes.TrimSpace(b)))
}

// ---- control channel ----

func (m *Machine) acceptCtl() {
	for {
		c, err := m.vsock.Accept()
		if err != nil {
			return
		}
		m.mu.Lock()
		if m.ctl != nil {
			m.ctl.Close()
		}
		m.ctl, m.enc = c, json.NewEncoder(c)
		m.mu.Unlock()
		go m.readCtl(c)
	}
}

func (m *Machine) readCtl(c net.Conn) {
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var msg proto.Msg
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		m.mu.Lock()
		if msg.Type == "resp" {
			if ch := m.pending[msg.ID]; ch != nil {
				delete(m.pending, msg.ID)
				ch <- msg
			}
			m.mu.Unlock()
			continue
		}
		if len(m.events) < 256 {
			m.events = append(m.events, msg)
		}
		m.mu.Unlock()
		switch msg.Type {
		case "boot":
			m.info.Boot = time.Since(m.start)
			m.setGuestStages(msg.Stages)
		case "pooled":
			m.setGuestStages(msg.Stages)
			if m.pooledOK.CompareAndSwap(false, true) {
				m.mu.Lock()
				m.pooledAt = time.Now()
				m.mu.Unlock()
				close(m.pooled)
			}
		case "ready":
			if m.info.Source == "cold" {
				m.info.Ready = time.Since(m.start)
			}
			m.info.GuestUptimeAtReady = time.Duration(msg.UptimeMS) * time.Millisecond
			m.setGuestStages(msg.Stages)
			if m.readyOK.CompareAndSwap(false, true) {
				close(m.ready)
			}
		case "workerd-exit":
			m.log.Warn("guest workerd exited", "code", msg.ExitCode, "restarts", msg.Restarts, "err", msg.Error)
			m.mu.Lock()
			m.lastErr = fmt.Sprintf("workerd exit %d %s", msg.ExitCode, msg.Error)
			m.mu.Unlock()
		}
	}
}

func (m *Machine) setGuestStages(st map[string]float64) {
	if len(st) == 0 {
		return
	}
	m.mu.Lock()
	if m.info.GuestStages == nil {
		m.info.GuestStages = map[string]float64{}
	}
	for k, v := range st {
		m.info.GuestStages[k] = v
	}
	m.mu.Unlock()
}

func (m *Machine) eventTypes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var s []string
	for _, e := range m.events {
		s = append(s, e.Type)
	}
	return s
}

// Op implements sandbox.VM.
func (m *Machine) Op(ctx context.Context, msg proto.Msg) (proto.Msg, error) {
	ch := make(chan proto.Msg, 1)
	m.mu.Lock()
	if m.enc == nil {
		m.mu.Unlock()
		return proto.Msg{}, errors.New("guest agent not connected")
	}
	m.nextID++
	msg.ID = m.nextID
	m.pending[msg.ID] = ch
	err := m.enc.Encode(msg)
	m.mu.Unlock()
	if err != nil {
		return proto.Msg{}, err
	}
	select {
	case r := <-ch:
		if r.Error != "" {
			return r, errors.New(r.Error)
		}
		return r, nil
	case <-ctx.Done():
		return proto.Msg{}, ctx.Err()
	case <-m.done:
		return proto.Msg{}, errors.New("microVM exited")
	}
}

// ---- ingress ----

func (m *Machine) ingressProxy() http.Handler {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return m.vn.dialGuest(ctx, proto.IngressPort)
		},
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     30 * time.Second,
	}
	target := &url.URL{Scheme: "http", Host: "guest"}
	return &httputil.ReverseProxy{
		Transport:     tr,
		FlushInterval: -1,
		ErrorLog:      nil,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if r.Context().Err() != nil {
				return
			}
			netd.WriteError(w, &netd.Error{Code: netd.CodeUpstreamUnavailable, Detail: "microVM: " + err.Error()})
		},
	}
}

// Ingress implements sandbox.VM.
func (m *Machine) Ingress() http.Handler { return m.ingress }

// DialGuest implements sandbox.VM.
func (m *Machine) DialGuest(ctx context.Context) (net.Conn, error) {
	return m.vn.dialGuest(ctx, proto.IngressPort)
}

// ---- lifecycle ----

// ExitStats are the helper's stats from the VM's exit (volume release ms;
// for a restored VM, uffd faults and pages copied). nil before the exit.
func (m *Machine) ExitStats() map[string]float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return maps.Clone(m.exitStats)
}

// Done implements sandbox.VM.
func (m *Machine) Done() <-chan struct{} { return m.done }

// Err implements sandbox.VM.
func (m *Machine) Err() error { return m.exitErr }

// Info implements sandbox.VM.
func (m *Machine) Info() sandbox.Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.info
	i.Stages, i.GuestStages = maps.Clone(i.Stages), maps.Clone(i.GuestStages)
	return i
}

// Refused implements sandbox.VM.
func (m *Machine) Refused() []string { return m.vn.refusedFlows() }

// Disks implements sandbox.VM.
func (m *Machine) Disks() []string { return m.disks }

// RSSKB implements sandbox.VM: the firecracker process's RSS.
func (m *Machine) RSSKB() int64 {
	if m.pid > 0 {
		return procRSSKB(m.pid)
	}
	return 0
}

// Kill implements sandbox.VM: the helper SIGKILLs the VMM (no clean
// shutdown; the guest's volume is left as a power cut would leave it).
func (m *Machine) Kill() {
	if m.conn != nil {
		m.conn.Do(helperapi.Request{Op: "kill"}, nil, 5*time.Second)
	}
}

// Stop implements sandbox.VM. With a volume, the guest first stops workerd
// and unmounts it (so every write is on the host), then the VMM is killed
// (arm64 Firecracker has no ACPI shutdown) and the helper releases the
// volume, which can then be attached to the next VM. Stop returns after
// that release.
func (m *Machine) Stop() {
	if m.volume && m.readyOK.Load() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		st := time.Now()
		_, err := m.Op(ctx, proto.Msg{Op: "shutdown"})
		cancel()
		m.log.Info("guest released its volume", "ms", time.Since(st).Milliseconds(), "err", err)
	}
	m.stopped.Store(true)
	kt := time.Now()
	m.Kill()
	select {
	case <-m.done:
		m.mu.Lock()
		rel := m.releaseMS
		m.mu.Unlock()
		m.log.Info("microVM stopped", "kill_to_exit_ms", time.Since(kt).Milliseconds(), "helper_volume_release_ms", rel, "exit", m.ExitStats())
	case <-time.After(15 * time.Second):
		m.log.Warn("microVM did not exit after kill")
	}
	m.teardown()
	if os.Getenv("TWD_KEEP_INSTANCES") == "" {
		os.RemoveAll(m.dir)
	}
}

// teardown aborts every flow and drops the helper session (idempotent).
func (m *Machine) teardown() {
	m.cleanup.Do(func() {
		if m.vn != nil {
			m.vn.close()
		}
		if m.tapFD >= 0 {
			unix.Close(m.tapFD)
		}
		if m.vsock != nil {
			m.vsock.Close()
		}
		m.mu.Lock()
		if m.ctl != nil {
			m.ctl.Close()
		}
		m.mu.Unlock()
		if m.conn != nil {
			m.conn.Close()
		}
	})
}

// MemoryDump implements sandbox.VM via a Firecracker full snapshot (taken
// by the helper inside the jail; we get the memory file as an fd).
func (m *Machine) MemoryDump(ctx context.Context, dir string) (string, error) {
	_, fds, err := m.conn.Do(helperapi.Request{Op: "memdump"}, nil, 60*time.Second)
	if err != nil {
		return "", err
	}
	if len(fds) != 1 {
		for _, fd := range fds {
			unix.Close(fd)
		}
		return "", errors.New("memdump: no fd")
	}
	src := os.NewFile(uintptr(fds[0]), "mem")
	defer src.Close()
	mem := filepath.Join(dir, m.id+".mem")
	out, err := os.OpenFile(mem, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(out, src)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return mem, err
}

// ---- config drive ----

func writeConfigDrive(path string, s sandbox.Spec, mtu int) error {
	gc := proto.GuestConfig{UnixNano: time.Now().UnixNano(), IP: proto.GuestIP, Gateway: proto.GatewayIP, Prefix: proto.PrefixLen,
		MTU: mtu, WorkerdArgs: s.WorkerdArgs, HealthCF: s.HealthCF, Debug: s.Debug}
	if s.Volume != nil {
		gc.Volume = proto.VolumeDev
	}
	return writeDrive(path, gc, func(tw *tar.Writer) error { return tarDir(tw, s.AppDir) })
}

// The warm-up app on a pool VM's config drive: twd's own, no tenant code.
// One request through a Durable Object with SQLite and KV storage faults in
// the code paths the platform runtime uses (workerd, V8, the isolate, SQLite),
// on top of the workerd binary itself. It listens on loopback only.
const (
	warmupCapnp = `using Workerd = import "/workerd/workerd.capnp";
const config :Workerd.Config = (
  services = [ (name = "main", worker = .w), (name = "do", disk = (path = "/run/twd/app/warmdo", writable = true)) ],
  sockets = [ (name = "in", address = "127.0.0.1:8080", service = "main", http = (cfBlobHeader = "x-twd-cf")) ],
);
const w :Workerd.Worker = (
  compatibilityDate = "2026-04-01",
  modules = [ (name = "w.js", esModule = embed "w.js") ],
  durableObjectNamespaces = [ (className = "W", uniqueKey = "twd-warmup", enableSql = true) ],
  durableObjectStorage = (localDisk = "do"),
  bindings = [ (name = "NS", durableObjectNamespace = "W") ],
);
`
	warmupJS = `import { DurableObject } from "cloudflare:workers";
export class W extends DurableObject {
  async fetch(req) {
    const sql = this.ctx.storage.sql;
    sql.exec("CREATE TABLE IF NOT EXISTS t (k TEXT PRIMARY KEY, v TEXT)");
    sql.exec("INSERT OR REPLACE INTO t VALUES (?, ?)", "k", "v");
    await this.ctx.storage.put("k", { n: 1 });
    const v = await this.ctx.storage.get("k");
    return new Response("warm " + sql.exec("SELECT count(*) AS n FROM t").one().n + " " + JSON.stringify(v));
  }
}
export default {
  async fetch(req, env) {
    const r = await env.NS.get(env.NS.idFromName("w")).fetch("http://w/");
    return new Response(await r.text() + " " + JSON.stringify(req.cf ?? {}));
  }
};
`
)

// writePoolDrive writes a pool VM's config drive: the network config and
// the warm-up app (plus proto.WarmupMarker, which fsscan must not find
// after the warm-up). No tenant code, no keys.
func writePoolDrive(path string, mtu int) error {
	gc := proto.GuestConfig{UnixNano: time.Now().UnixNano(), IP: proto.GuestIP, Gateway: proto.GatewayIP, Prefix: proto.PrefixLen,
		MTU: mtu, Pool: true, WarmupArgs: []string{"serve", "config.capnp", "--experimental"}, WarmupHealthCF: "{}"}
	return writeDrive(path, gc, func(tw *tar.Writer) error {
		if err := tw.WriteHeader(&tar.Header{Name: "warmdo/", Mode: 0o700, Typeflag: tar.TypeDir}); err != nil {
			return err
		}
		for name, body := range map[string]string{"config.capnp": warmupCapnp, "w.js": warmupJS, proto.WarmupMarker: "twd warm-up app (no tenant code)\n"} {
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
				return err
			}
			if _, err := io.WriteString(tw, body); err != nil {
				return err
			}
		}
		return nil
	})
}

// bundleTar is the workload's instance dir as a tar (a pool VM's load).
func bundleTar(dir string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tarDir(tw, dir); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeDrive(path string, gc proto.GuestConfig, body func(*tar.Writer) error) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	gb, _ := json.Marshal(gc)
	if err := tw.WriteHeader(&tar.Header{Name: proto.GuestConfigFile, Mode: 0o600, Size: int64(len(gb)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	tw.Write(gb)
	if err := body(tw); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if pad := buf.Len() % 4096; pad != 0 {
		buf.Write(make([]byte, 4096-pad))
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

func tarDir(tw *tar.Writer, root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			return tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel) + "/", Mode: 0o700, Typeflag: tar.TypeDir})
		}
		if !d.Type().IsRegular() {
			return nil // sockets etc.
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Mode: 0o600, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err = tw.Write(b)
		return err
	})
}

// PoolStats reports the warm pool (nil: no pool).
func (b *Backend) PoolStats() *PoolStats {
	if b.pool == nil {
		return nil
	}
	s := b.pool.stats()
	return &s
}
