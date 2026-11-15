// twd-mgmt is twd's management plane (M2): tenants, deployments, the object
// registry with lineage, epoch-fenced namespace leases, the mint broker
// (API-key or WIF), the per-tenant registration throttle, spawn checks,
// kill, the reaper and the hash-chained audit log.
//
// It serves the /v1 API (api/openapi.yaml) on its own tsnet node on the
// operator tailnet, authenticated by WhoIs; the twd CLI on the same machine
// reaches it through the node's loopback proxy (<state-dir>/loopback.json).
// Only with -local-dev (twd dev, hermetic tests) does it also serve a unix
// socket guarded by the per-host token. In WIF mode it is also an OIDC
// issuer, served via Funnel (and/or a local listener).
//
// The daemon itself is internal/mgmtd; this binary parses the flags. The
// single-process `twd up` runs the same runtime embedded with the host agent.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/rosskukulinski/twd/internal/version"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rosskukulinski/twd/internal/mgmtd"
	"github.com/rosskukulinski/twd/internal/obs"
	"github.com/rosskukulinski/twd/internal/tsapi"
	"github.com/rosskukulinski/twd/mgmt"
	"github.com/rosskukulinski/twd/netd"
)

func main() {
	if version.Asked(os.Args) {
		fmt.Println(version.String("twd-mgmt"))
		return
	}
	if handled, err := runMaintenance(os.Args[1:]); handled { // maintenance.go
		if err != nil {
			fmt.Fprintln(os.Stderr, "twd-mgmt:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "twd-mgmt:", err)
		os.Exit(1)
	}
}

func run() error {
	home, _ := os.UserHomeDir()
	var (
		stateDir     = flag.String("state-dir", filepath.Join(home, ".twd", "mgmt"), "state directory (0700): db, custody, issuer key")
		localDev     = flag.Bool("local-dev", false, "ALSO serve the API on a unix socket guarded by the per-host token (twd dev, hermetic tests). Normal operation is tsnet only")
		socket       = flag.String("socket", filepath.Join(home, ".twd", "run", "mgmt.sock"), "-local-dev: the unix socket path")
		tokenFile    = flag.String("host-token-file", filepath.Join(home, ".twd", "hostd", "mgmt.token"), "-local-dev: per-host bearer token for the unix socket (0600; minted here, read by twd-hostd and the twd CLI; sent as Twd-Host-Token)")
		rotateToken  = flag.Bool("rotate-host-token", false, "mint a fresh host token at start (twd up does this)")
		dbDSN        = flag.String("db", "", "store: SQLite path (default <state-dir>/mgmt.db) or postgres://… DSN")
		dbDSNFile    = flag.String("db-file", "", "read the store DSN from this file (keeps a Postgres password off argv)")
		tsapiURL     = flag.String("tsapi-url", tsapi.DefaultBaseURL, "Tailscale API base URL")
		issuerURL    = flag.String("issuer-url", "", "WIF: public https issuer origin (default with -funnel: https://<tsnet name>)")
		issuerListen = flag.String("issuer-listen", "", "WIF: also serve the issuer on this local address (plain HTTP, e.g. behind a proxy)")
		funnel       = flag.Bool("funnel", false, "WIF: serve the issuer publicly via Funnel on the ISSUER's own tsnet node (-issuer-tsnet-hostname); mgmt's own node never uses Funnel")
		issHostname  = flag.String("issuer-tsnet-hostname", "", "WIF: the issuer's own tsnet node (e.g. twd-issuer, tag "+mgmt.IssuerTag+"): Funnel-only, serves discovery + JWKS")
		issDir       = flag.String("issuer-tsnet-dir", "", "WIF: the issuer node's state dir (default <state-dir>/issuer-tsnet; twd up joins it)")
		issAuthFile  = flag.String("issuer-tsnet-authkey-file", "", "WIF: TS_AUTHKEY=… for the issuer node's first join (default: the state must already be logged in)")
		tsHostname   = flag.String("tsnet-hostname", "", "serve the API on the operator tailnet as this tsnet node (e.g. twd-mgmt); required unless -local-dev")
		noLoopback   = flag.Bool("no-loopback", false, "don't publish the node's loopback proxy for the local twd CLI (<state-dir>/loopback.json)")
		versionFlag  = flag.String("version", "", "deprecated: the version is built in (internal/version); a non-empty value overrides it for GET /v1/status")
		tsControl    = flag.String("tsnet-control-url", "", "operator tailnet control URL (required with -tsnet-hostname)")
		tsAuthFile   = flag.String("tsnet-authkey-file", "", "file with the operator-tailnet auth key for the first join (TS_AUTHKEY=…); without it the node state must already be logged in (twd up does the interactive login)")
		tsTags       = flag.String("tsnet-tags", mgmt.MgmtTag, "comma-separated tags the mgmt node advertises")
		tsService    = flag.String("tsnet-service", "", "also host this Tailscale Service (e.g. svc:twd-mgmt) on :80, advertised only while this replica's store answers: point every host at http://<label>.<tailnet> and run each replica with its own -tsnet-hostname (e.g. twd-mgmt-1). Policy needs autoApprovers.services for the mgmt tag and grants to the svc: name")
		tsTLS        = flag.Bool("tsnet-tls", true, "also serve the API on :443 with the node's HTTPS certificate when the tailnet has HTTPS enabled (plain :80 is always served)")
		admins       = flag.String("admins", "", "comma-separated tailnet logins admitted as admins even without the "+mgmt.AdminCap+" capability (override; the normal path is the cap, granted to "+mgmt.MgmtTag+")")
		custodyKind  = flag.String("custody", "file", "custody backend: file (AES-GCM in <state-dir>/custody) or setec")
		setecURL     = flag.String("setec-url", "", "setec server URL (with -custody setec)")
		reapEvery    = flag.Duration("reap-interval", mgmtd.DefaultReapInterval, "reaper interval")
		delDelay     = flag.Duration("device-delete-delay", 0, "defer kill tier 2 (device delete) this long after the tier-1 push, so the host's tombstone answers new connections with object_killed (keep < hostd -kill-tombstone)")
		warmKeys     = flag.Int("warm-keys", mgmtd.DefaultWarmKeys, "warm-key pool: pre-minted single-use keys per (tenant, tag set), so a create skips the CreateKey call (sized from the recent create rate, capped here; 0 = off)")
		warmKeyTTL   = flag.Duration("warm-key-ttl", mgmt.DefaultWarmKeyTTL, "-warm-keys: a pooled key's expiry; unused keys are revoked shortly before it")
		metricsPort  = flag.Int("metrics-port", obs.DefaultMetricsPort, "port on mgmt's own tailnet node serving GET /metrics (Prometheus; who may scrape is a tailnet policy grant); 0 = off")
		verbose      = flag.Bool("v", false, "debug logging")
		registryDir  = flag.String("registry-dir", "", "OCI registry blob store (ADR-032 D3; default <state-dir>/registry; \"off\" disables the registry). Replicas serving the registry must share it")
		regInsecure  = flag.String("registry-insecure", "", "comma-separated upstream registry hosts mirrored over plain HTTP (tests: a local upstream)")
		noMirror     = flag.Bool("no-mirror", false, "refuse external image refs at deploy (only twd deploy builds and public bases); mgmt then makes no registry egress")
		aliasFile    = flag.String("image-aliases", "", "JSON file [{from, to, arch}] replacing the default image alias map (cloudflare/sandbox → twd.local arm64 rebuilds)")
	)
	flag.Parse()
	lvl := slog.LevelInfo
	if *verbose {
		lvl = slog.LevelDebug
	}
	log := slog.New(netd.NewRedactingHandler(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if v := os.Getenv("TWD_TEST_WALL_CLOCK_OFFSET"); v != "" {
		// Test-only (mm-loop S8): simulate a wrong machine clock.
		d, err := time.ParseDuration(v)
		if err != nil || !*localDev {
			return fmt.Errorf("TWD_TEST_WALL_CLOCK_OFFSET is a test hook: it needs -local-dev and a duration (got %q)", v)
		}
		mgmt.SetTestWallClockOffset(d)
		log.Warn("TEST HOOK: wall clock offset", "offset", d)
	}
	o := mgmtd.Options{StateDir: *stateDir, LocalDev: *localDev, Socket: *socket, HostTokenFile: *tokenFile, RotateHostToken: *rotateToken,
		DB: *dbDSN, DBFile: *dbDSNFile, TSAPIURL: *tsapiURL,
		IssuerURL: *issuerURL, IssuerListen: *issuerListen, Funnel: *funnel,
		IssuerTSNetHostname: *issHostname, IssuerTSNetDir: *issDir, IssuerTSNetAuthKeyFile: *issAuthFile,
		TSNetHostname: *tsHostname, TSNetControlURL: *tsControl, TSNetAuthKeyFile: *tsAuthFile, TSNetService: *tsService, TSNetTLS: *tsTLS,
		NoLoopback: *noLoopback, Version: mgmtVersion(*versionFlag), Custody: *custodyKind, SetecURL: *setecURL,
		ReapInterval: *reapEvery, DeviceDeleteDelay: *delDelay, WarmKeys: *warmKeys, WarmKeyTTL: *warmKeyTTL,
		RegistryDir: *registryDir, NoMirror: *noMirror, ImageAliasesFile: *aliasFile, Log: log,
		MetricsPort: *metricsPort}
	// Like the flags: "" means no tags at all (Options nil means the default).
	o.TSNetTags = []string{}
	if *tsTags != "" {
		o.TSNetTags = strings.Split(*tsTags, ",")
	}
	if *admins != "" {
		o.Admins = strings.Split(*admins, ",")
	}
	if *regInsecure != "" {
		o.RegistryInsecure = strings.Split(*regInsecure, ",")
	}
	rt, err := mgmtd.Start(ctx, o)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
	case err := <-rt.Errs():
		log.Error("listener failed", "err", err)
	}
	return rt.Close()
}

// mgmtVersion is the version mgmt reports: the built-in one, unless the
// deprecated -version flag overrides it (older `twd up` passes it).
func mgmtVersion(flagV string) string {
	if flagV != "" && version.IsDev(version.Version) {
		return flagV
	}
	return version.Version
}
