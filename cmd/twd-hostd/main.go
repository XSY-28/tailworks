// twd-hostd is the twd host agent (process tier): it registers with
// twd-mgmt, heartbeats, pulls assignments, and runs each deployment as one
// supervised workerd process with per-object tailnet nodes (netd library).
//
//	twd-hostd -mgmt unix:///Users/me/.twd/run/mgmt.sock -host-id h1 -data-dir ~/.twd/hostd
//
// The push API (mgmt → hostd) and the operator's status live on
// <run-dir>/hostd.sock:
//
//	POST /v1/push/kill {objectID, reason}   POST /v1/push/assignments   GET /v1/status
//
// This host's own tailnet node serves the two pushes only, on -push-port,
// and only to mgmt's node (WhoIs: tag:twd-mgmt, pinned to the node -mgmt
// resolves to).
//
// The agent itself is internal/hostdrun; this binary parses the flags. The
// single-process `twd up` runs the same runtime embedded in mgmt's process.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rosskukulinski/twd/hostd"
	"github.com/rosskukulinski/twd/hostd/image"
	"github.com/rosskukulinski/twd/hostd/vm/helperapi"
	"github.com/rosskukulinski/twd/internal/hostdrun"
	"github.com/rosskukulinski/twd/internal/obs"
	"github.com/rosskukulinski/twd/internal/tsapi"
	"github.com/rosskukulinski/twd/netd"
	"tailscale.com/tsnet"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "twd-hostd:", netd.Redact(err.Error()))
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "join" {
		return runJoin(os.Args[2:])
	}
	home, _ := os.UserHomeDir()
	var (
		mgmtAddr   = flag.String("mgmt", "", "twd-mgmt address: http(s)://twd-mgmt.<tailnet> over this host's node (default), or unix:///path with -local-dev")
		localDev   = flag.Bool("local-dev", false, "talk to mgmt over its unix socket with the per-host token instead of this host's own tailnet node (twd dev, hermetic tests)")
		tsDir      = flag.String("tsnet-dir", "", "state of this host's own tailnet node (default <data-dir>/tsnet)")
		tsControl  = flag.String("tsnet-control-url", "", "operator tailnet control URL for this host's node (required unless -local-dev)")
		joinFile   = flag.String("join-token-file", "", "TS_AUTHKEY=… file with the single-use join key mgmt minted (twd host add / twd up); waited for when the node has no state yet. Deleted after use")
		pushPort   = flag.Int("push-port", hostdrun.DefaultPushPort, "port on this host's node where mgmt pushes kills and assignment changes")
		metricsPrt = flag.Int("metrics-port", obs.DefaultMetricsPort, "port on this host's node serving GET /metrics (Prometheus; who may scrape is a tailnet policy grant); 0 = off")
		hostID     = flag.String("host-id", "", "this host's ID (default: hostname)")
		dataDir    = flag.String("data-dir", filepath.Join(home, ".twd", "hostd"), "node state, DO storage, objects.json")
		runDir     = flag.String("run-dir", "", "sockets and generated configs (short path; default a new /tmp dir)")
		workerd    = flag.String("workerd", "", "workerd binary (default: $TWD_WORKERD, ~/.twd/bin/workerd, or PATH)")
		hb         = flag.Duration("heartbeat", 3*time.Second, "heartbeat / assignment poll period")
		fenceAfter = flag.Duration("fence-after", hostd.DefaultFenceAfter, "stop serving stateful workloads after this long without a successful heartbeat (≤ 40 s, below mgmt's 45 s lease; negative disables)")
		ephemeral  = flag.Bool("ephemeral", false, "join nodes as ephemeral (dev/test)")
		prefix     = flag.String("hostname-prefix", "", "prefix for every node hostname")
		controlURL = flag.String("control-url", "", "override every tenant's control URL (testing)")
		capacity   = flag.Int("capacity-nodes", 1000, "advertised node capacity")
		tombstone  = flag.Duration("kill-tombstone", 10*time.Second, "keep killed nodes RST-ing new connections this long")
		logLevel   = flag.String("log-level", "info", "debug|info|warn|error")
		tsnetLogs  = flag.Bool("tsnet-logs", false, "forward tsnet logs at debug level")
		tier       = flag.String("default-tier", "process", "tier for deployments and tenants that name none: process | microvm")
		vmKernel   = flag.String("vm-kernel", hostdrun.VMAsset(home, "vmlinux"), "microVM tier: guest kernel (Firecracker on Linux, Virtualization.framework on macOS)")
		vmRootfs   = flag.String("vm-rootfs", hostdrun.VMAsset(home, "rootfs.ext4"), "microVM tier: guest image from guest/build-image.sh")
		vmHelper   = flag.String("vm-helper", helperapi.DefaultSocket, "microVM tier: twd-vmhelper socket (sudo twd host setup --microvm)")
		vmFC       = flag.String("firecracker", "/usr/local/bin/firecracker", "microVM tier, as root without the helper service: firecracker binary")
		vmJailer   = flag.String("jailer", "/usr/local/bin/jailer", "microVM tier, as root without the helper service: jailer binary")
		vmVolMiB   = flag.Int("vm-volume-mib", 1024, "microVM tier: size of new DO volumes (sparse)")
		vmMem      = flag.Int("vm-mem-mib", 512, "microVM tier: guest memory")
		vmCPUs     = flag.Int("vm-vcpus", 2, "microVM tier: guest vCPUs")
		vmHuge     = flag.String("vm-hugepages", "auto", "microVM tier: back guest RAM with 2 MiB pages: auto | on | off")
		vzVMM      = flag.String("vz-vmm", "", "microVM tier on macOS: the twd-vz VMM, signed with the virtualization entitlement (default: next to twd-hostd)")
		vmRosetta  = flag.Bool("vm-rosetta", false, "microVM tier on macOS (Apple silicon): share Rosetta for Linux into guests so x86_64 binaries run")
		vmSnap     = flag.Bool("vm-snapshot", true, "microVM tier: when the warm pool is empty, restore a VM from a base snapshot of a warm VM without tenant code or keys (private, lazily loaded guest memory) instead of booting one (ADR-030; Durable Object VMs only, containers cold-boot)")
		vmSnapFill = flag.Bool("vm-snapshot-refill", false, "microVM tier: also refill the warm pool by restoring the base snapshot (faster refills, slower claims) instead of booting")
		vmRotate   = flag.Duration("vm-snapshot-rotate", time.Hour, "microVM tier: rebuild the base snapshot this often (and at every start); idle pool VMs restored from the old base are discarded; < 0 = never")
		vmPool     = flag.Int("vm-pool", 0, "microVM tier: warm pool size (pre-booted VMs without tenant code; single use; Durable Object VMs only, containers cold-boot): 0 = auto (≤ 2, within the huge-page reservation), -1 = off")
		sandboxF   = flag.String("sandbox", "auto", "process-tier workerd/Node sandbox: auto (seatbelt on macOS, landlock on Linux) | seatbelt | landlock | none")
		insecureSH = flag.Bool("insecure-shared-host", false, "without a process sandbox, still run process-tier workloads of more than one tenant (tenants are NOT isolated from each other)")
		sbDeny     = flag.String("sandbox-deny", "", "comma-separated extra paths no workerd may read, write or connect to (twd up: its state and run dirs)")
		nodeBin    = flag.String("node", "", "Node process tier (runtime \"node\"): node binary (default: $TWD_NODE, or node on PATH)")
		tokenFile  = flag.String("mgmt-token-file", filepath.Join(home, ".twd", "hostd", "mgmt.token"), "per-host token file minted by twd-mgmt (sent as Twd-Host-Token on the unix socket; \"\" = none)")
		poolNodes  = flag.Int("pool-nodes", hostdrun.DefaultPoolNodes, "pre-joined nodes to keep per hot class (one with a create here in the last hour): a create takes one and renames it (~0.2 s) instead of joining a new node; each is a device; 0 = off")
		portMap    = flag.Bool("port-mapping", false, "use UPnP/NAT-PMP/PCP for this host's tailnet nodes (off by default: it adds ~250 ms to every new object node's join and has every node ask the router for a mapping; turn on for hosts behind hard NATs that need router-assisted direct paths)")
		pathEvery  = flag.Duration("path-every", hostd.DefaultPathEvery, "smart placement: measure and report tailnet paths to assignments' placement targets this often (negative disables)")
		siblings   = flag.Bool("allow-sibling-egress", false, "let objects reach other objects hosted here over the tailnet (hairpin; single-host tests/demos)")
		ctrOff     = flag.Bool("no-containers", false, "don't offer containers (ctx.container) even with a microVM backend")
		ctrKernel  = flag.String("vm-container-kernel", "", "containers: guest kernel with erofs (guest/build-kernel.sh; default -vm-kernel)")
		ctrCache   = flag.String("container-image-cache", "", "containers: converted image cache (default <data-dir>/images)")
		ctrImages  = flag.String("container-image-dir", "", "containers: dir of image tarballs with an images.json index (tests and hosts without a registry; deployments with pinned images pull from mgmt's registry instead)")
		ctrPublic  = flag.String("container-public-images", "", "containers: comma-separated image refs cached once for all tenants (public bases; -container-image-dir only: pinned images take their scope from the pin)")
		ctrImgMB   = flag.Int64("container-image-max-mib", image.DefaultMaxImageBytes>>20, "containers: cap on one image's uncompressed layers while converting it (also 128 layers, 1 Mi entries); an image over it fails to convert")
		ctrCacheMB = flag.Int64("container-image-cache-max-mib", 0, "containers: cap on the converted image cache; past it the least recently used images not in use are removed (0 = no cap)")
		ctrIdle    = flag.Duration("container-idle", hostd.DefaultContainerIdle, "containers: default inactivity timeout (setInactivityTimeout overrides, ≤ 6 h)")
		ctrMaxLife = flag.Duration("container-max-lifetime", hostd.DefaultContainerMaxLife, "containers: hard max lifetime of a container")
		ctrGrace   = flag.Duration("container-stop-grace", hostd.DefaultContainerGrace, "containers: cap on destroy()'s SIGTERM → SIGKILL grace")
		ctrMax     = flag.Int("container-max", 0, "containers: max running on this host (0 = unlimited)")
		ctrMemMiB  = flag.Int("container-mem-mib", 0, "containers: max total guest memory on this host (0 = unlimited)")
		ctrVCPU    = flag.Float64("container-vcpu", 0, "containers: max total vCPU on this host (0 = unlimited)")
		ctrInet    = flag.String("container-internet", "direct", "containers: how start({enableInternet}) reaches a workload's egress.internet hosts: direct (this host's network, public addresses only) or off")
	)
	flag.Parse()
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(*logLevel)); err != nil {
		return err
	}
	log := slog.New(netd.NewRedactingHandler(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
	o := hostdrun.Options{MgmtAddr: *mgmtAddr, LocalDev: *localDev, MgmtTokenFile: *tokenFile,
		TSNetDir: *tsDir, TSNetControlURL: *tsControl, JoinTokenFile: *joinFile, PushPort: *pushPort, PortMapping: *portMap,
		MetricsPort: *metricsPrt,
		HostID:      *hostID, DataDir: *dataDir, RunDir: *runDir, Workerd: *workerd,
		Heartbeat: *hb, FenceAfter: *fenceAfter, Ephemeral: *ephemeral, HostnamePrefix: *prefix, ControlURL: *controlURL, CapacityNodes: *capacity,
		KillTombstone: *tombstone, TSNetLogs: *tsnetLogs, PoolNodes: *poolNodes, PathEvery: *pathEvery, DefaultTier: *tier,
		VM: hostdrun.VMOptions{Kernel: *vmKernel, Rootfs: *vmRootfs, Firecracker: *vmFC, Jailer: *vmJailer, Helper: *vmHelper,
			MemMiB: *vmMem, VCPUs: *vmCPUs, VolumeMiB: *vmVolMiB, HugePages: *vmHuge, ContainerKernel: *ctrKernel, VzVMM: *vzVMM, Rosetta: *vmRosetta,
			Pool: *vmPool, NoSnapshot: !*vmSnap, SnapRefill: *vmSnapFill, Rotate: *vmRotate},
		Sandbox: *sandboxF, InsecureSharedHost: *insecureSH, SandboxDeny: hostdrun.SplitList(*sbDeny), NodeBin: *nodeBin,
		AllowSiblingEgress: *siblings,
		NoContainers:       *ctrOff, ContainerImageCache: *ctrCache, ContainerImageDir: *ctrImages, ContainerPublicImages: hostdrun.SplitList(*ctrPublic),
		ContainerImageMaxMiB: *ctrImgMB, ContainerImageCacheMaxMiB: *ctrCacheMB,
		ContainerIdle: *ctrIdle, ContainerMaxLife: *ctrMaxLife, ContainerGrace: *ctrGrace,
		ContainerMax: *ctrMax, ContainerMemMiB: *ctrMemMiB, ContainerVCPU: *ctrVCPU, ContainerInternet: *ctrInet, Log: log}
	rt, err := hostdrun.New(context.Background(), o)
	if err != nil {
		return err
	}
	if err := rt.Start(context.Background()); err != nil {
		rt.Close()
		return err
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info("shutting down (node state kept for rejoin)")
	return rt.Close()
}

// runJoin is `twd-hostd join --token-file <file> [--token <key>] --control-url … --tsnet-dir …`:
// join the operator tailnet once with the single-use key `twd host add`
// minted, save the node state and exit. The service then starts without a key.
func runJoin(args []string) error {
	home, _ := os.UserHomeDir()
	fs := flag.NewFlagSet("twd-hostd join", flag.ContinueOnError)
	hostID := fs.String("host-id", "", "this host's ID (default: hostname)")
	dataDir := fs.String("data-dir", filepath.Join(home, ".twd", "hostd"), "hostd state (the node state goes to <data-dir>/tsnet)")
	tsDir := fs.String("tsnet-dir", "", "node state dir (default <data-dir>/tsnet)")
	control := fs.String("control-url", "https://controlplane.tailscale.com", "operator tailnet control URL")
	tokenFile := fs.String("token-file", "", "TS_AUTHKEY=… file with the join key (twd host add --json > …)")
	token := fs.String("token", "", "the join key itself (prefer --token-file; argv is visible to other users)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *hostID == "" {
		h, _ := os.Hostname()
		*hostID = h
	}
	if *tsDir == "" {
		*tsDir = filepath.Join(*dataDir, "tsnet")
	}
	key := *token
	if *tokenFile != "" {
		k, err := tsapi.LoadEnvFile(*tokenFile, "TS_AUTHKEY")
		if err != nil {
			return err
		}
		key = k
	}
	if key == "" {
		return errors.New("twd-hostd join needs --token-file or --token")
	}
	if err := os.MkdirAll(*tsDir, 0o700); err != nil {
		return err
	}
	ts := &tsnet.Server{Hostname: hostdrun.HostNodeName(*hostID), Dir: *tsDir, ControlURL: *control, AdvertiseTags: []string{hostdrun.HostTag}, AuthKey: key,
		Logf: func(string, ...any) {}, UserLogf: func(string, ...any) {}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	st, err := ts.Up(ctx)
	if err != nil {
		return fmt.Errorf("join: %w", err)
	}
	ts.Close()
	if *tokenFile != "" {
		// M1: keep the registration secret for the first registration;
		// the join key itself is single use.
		hostdrun.PersistRegistrationSecret(*tokenFile, *tsDir)
		os.Remove(*tokenFile)
	}
	fmt.Printf("joined as %s (%s); node state in %s. Start twd-hostd without a key now.\n", strings.TrimSuffix(st.Self.DNSName, "."), st.TailscaleIPs, *tsDir)
	return nil
}
