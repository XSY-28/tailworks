// Package proto is the twd-guestd ⇄ twd-hostd contract for the microVM
// tier: the guest config delivered on the config drive, and the
// newline-delimited JSON control channel over vsock.
//
// Nothing in here is secret, and nothing tailnet-related ever crosses it:
// the guest gets workerd's config and bundle, a per-instance egress MAC key
// (platform attribution, not a tailnet credential), and its network
// addresses. Node keys and auth keys stay in host netd.
package proto

// Config drive layout (/dev/vdb): a plain ustar stream (no filesystem), padded
// to a 4 KiB multiple. guestd extracts it into AppDir.
const (
	// GuestConfigFile is the JSON GuestConfig inside the config tar.
	GuestConfigFile = "guest.json"
	// AppDir is where the generated workerd instance dir is extracted.
	AppDir = "/run/twd/app"
	// DODir is the guest's DO storage directory: the persistent volume
	// (GuestConfig.Volume) mounted there, else guest tmpfs.
	DODir = "/run/twd/do"
	// VolumeDev is the persistent volume's device (the third drive).
	VolumeDev = "/dev/vdc"
)

// Defaults for the guest network (every VM lives in its own network
// namespace on the host, so all of them can use the same addresses).
const (
	GatewayIP   = "192.168.72.1"
	GuestIP     = "192.168.72.2"
	PrefixLen   = 24
	IngressPort = 8080 // workerd ingress (cfBlobHeader x-twd-cf); only host netd dials it
	EgressObj   = 3128 // facet egress (platform Egress, MAC'd object attribution)
	EgressWL    = 3129 // tenant service egress (workload identity)
	HostAPI     = 3130 // local host API (ensure/spawn/ready/kill/audit)
	// VsockPort is the host port guestd connects to (CID 2). Firecracker
	// forwards it to the unix socket <vsock uds>_<VsockPort> on the host.
	VsockPort = 1024
)

// GuestConfig is guest.json.
type GuestConfig struct {
	// UnixNano is host wall-clock time when the drive was written. The
	// guest has no trustworthy clock at boot; the platform's egress MAC
	// timestamps need one within ±2 min.
	UnixNano int64  `json:"unixNano"`
	IP       string `json:"ip"`
	Gateway  string `json:"gateway"`
	Prefix   int    `json:"prefix"`
	MTU      int    `json:"mtu,omitempty"`
	// Workerd is the argv after the binary (config path relative to AppDir).
	WorkerdArgs []string `json:"workerdArgs"`
	// HealthPath is requested on 127.0.0.1:IngressPort with HealthCF as
	// the x-twd-cf header; 200 means ready.
	HealthCF string `json:"healthCF"`
	// Debug enables the probe op (connectivity tests).
	Debug bool `json:"debug,omitempty"`
	// Volume is the block device holding DO storage (ext4, created and
	// decrypted on the host), mounted at DODir. "" = tmpfs (not persistent).
	Volume string `json:"volume,omitempty"`

	// Pool: this VM is a warm-pool (or snapshot-base) VM. Its config drive
	// carries no tenant code: only the network config above and the
	// warm-up workerd config (WarmupArgs, run in AppDir). guestd runs that
	// workerd once until healthy (faulting in the workerd binary and guest
	// memory, the cost of a cold start), kills it, empties AppDir and
	// reports "pooled". It then waits for exactly one "load" from the host
	// (the tenant bundle, see Load), and from there on behaves like a
	// config-drive VM. A pooled VM is never loaded twice.
	Pool           bool     `json:"pool,omitempty"`
	WarmupArgs     []string `json:"warmupArgs,omitempty"`
	WarmupHealthCF string   `json:"warmupHealthCF,omitempty"`
}

// WarmupMarker is a file in the warm-up app (pool config drive): fsscan
// reports AppDir's contents, and a pooled VM must show none of it (nor
// anything else) before its load.
const WarmupMarker = "twd-warmup.txt"

// Load is the host's one-time injection into a pooled (or restored) VM:
// the tenant bundle (sent before it in "chunk" messages: Data at Offset,
// Total bytes in all; a tar like the config drive's, without guest.json)
// plus what the config drive would have said.
type Load struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"` // hex, of the whole bundle tar
	// UnixNano: host wall-clock time now (a pooled VM may have idled for
	// hours; a restored one resumes with the base snapshot's clock).
	UnixNano    int64    `json:"unixNano"`
	WorkerdArgs []string `json:"workerdArgs"`
	HealthCF    string   `json:"healthCF"`
	Debug       bool     `json:"debug,omitempty"`
	// Volume is the DO volume's device ("" = tmpfs). The host has swapped
	// the encrypted volume in behind it already; VolumeSectors is its size
	// in 512-byte sectors, which guestd waits to see before mounting (the
	// placeholder it booted with is smaller).
	Volume        string `json:"volume,omitempty"`
	VolumeSectors int64  `json:"volumeSectors,omitempty"`
	// Entropy (host crypto/rand) is mixed into the guest kernel's pool and
	// the CRNG is reseeded from it before workerd starts: copies restored
	// from one snapshot share the kernel RNG state, and this guest kernel
	// (6.1, aarch64 DeviceTree) has no VMGenID driver to reseed it.
	Entropy []byte `json:"entropy"`
	// BootID replaces /proc/sys/kernel/random/boot_id (identical in every
	// copy of a snapshot otherwise).
	BootID string `json:"bootId"`
}

// Msg is one control-channel message (either direction).
//
// guest → host: Type "boot" (once, on connect), "ready" (workerd healthy),
// "workerd-exit", "log", "pooled" (a pool VM is warm and waits for its
// load; sent again on every new connection while it waits, e.g. after a
// snapshot restore), or "resp" (answer to an op, same ID).
// host → guest: Op "probe", "stats", "fsscan", "ping", "shutdown" (stop
// workerd, sync and unmount the volume: the host may then release it),
// "fsbench" (write+fsync latency on the DO volume), and for pool VMs
// "chunk" (bundle bytes) then "load" (Load: once).
type Msg struct {
	Type string `json:"type,omitempty"`
	ID   int64  `json:"id,omitempty"`
	Op   string `json:"op,omitempty"`

	// boot / ready
	UptimeMS  int64  `json:"uptimeMs,omitempty"`
	WorkerdMS int64  `json:"workerdMs,omitempty"`
	ExitCode  int    `json:"exitCode,omitempty"`
	Restarts  int    `json:"restarts,omitempty"`
	Text      string `json:"text,omitempty"`
	Error     string `json:"error,omitempty"`
	// Stages (boot / ready) are guest-side stage timestamps in ms since the
	// guest kernel started (CLOCK_BOOTTIME): "init" (guestd is PID 1),
	// "mounts", "config", "clock", "net", "volume", "workerd_spawn"
	// (exec returned), "workerd_listen" (ingress accepts TCP), "healthy".
	Stages map[string]float64 `json:"stages,omitempty"`

	// probe
	Targets []string      `json:"targets,omitempty"`
	Results []ProbeResult `json:"results,omitempty"`

	// stats
	Stats *Stats `json:"stats,omitempty"`

	// fsbench: N writes of 4 KiB + fdatasync on DODir
	N     int    `json:"n,omitempty"`
	Bench *Bench `json:"bench,omitempty"`

	// fsscan: files whose name looks like tailscale state; and every file
	// under the guest's state dirs (AppDir, DODir, /tmp, /run/twd) with
	// its size, so a test can check a pooled VM holds no workload's state.
	Found   []string         `json:"found,omitempty"`
	Scanned int              `json:"scanned,omitempty"`
	State   map[string]int64 `json:"state,omitempty"`

	// chunk / load (pool VMs)
	Data   []byte `json:"data,omitempty"`
	Offset int64  `json:"offset,omitempty"`
	Total  int64  `json:"total,omitempty"`
	Load   *Load  `json:"load,omitempty"`
}

// ProbeResult is the outcome of one TCP connect from inside the guest.
type ProbeResult struct {
	Target string `json:"target"`
	Result string `json:"result"` // connected | refused | timeout | unreachable | error
	Detail string `json:"detail,omitempty"`
	MS     int64  `json:"ms"`
}

// Bench is fsbench's result (microseconds).
type Bench struct {
	N          int    `json:"n"`
	FS         string `json:"fs"` // the mount's source and type
	WriteP50US int64  `json:"writeP50us"`
	WriteP95US int64  `json:"writeP95us"`
	SyncP50US  int64  `json:"fsyncP50us"`
	SyncP95US  int64  `json:"fsyncP95us"`
}

// Stats is guest memory accounting.
type Stats struct {
	MemTotalKB     int64 `json:"memTotalKB"`
	MemAvailableKB int64 `json:"memAvailableKB"`
	WorkerdRSSKB   int64 `json:"workerdRssKB"`
	UptimeMS       int64 `json:"uptimeMs"`
	// DO storage: the mount's source ("/dev/vdc ext4" or "tmpfs") and usage.
	DOFS     string `json:"doFS,omitempty"`
	DOUsedKB int64  `json:"doUsedKB,omitempty"`
}
