package main

import (
	"context"
	"slices"

	"fmt"
	"github.com/rosskukulinski/twd/hostd/mgmtapi"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rosskukulinski/twd/hostd"
	"github.com/rosskukulinski/twd/internal/policy"
	"github.com/rosskukulinski/twd/internal/procsb"
	"github.com/rosskukulinski/twd/internal/tsapi"
	"github.com/rosskukulinski/twd/mgmt"
	"github.com/rosskukulinski/twd/netd"
)

// TenantRef is one tenant this twd serves: the operator tenant (`twd up`)
// or one added with `twd tenant add`. Each has its own tailnet, credential
// file (path only: an OAuth client, or a Headscale API key), control URL, netd registry, workerd processes, state
// dir and registration throttle; nothing is shared between them.
type TenantRef struct {
	Tailnet    string `json:"tailnet"`
	TenantID   string `json:"tenantID"`
	MintMode   string `json:"mintMode,omitempty"`   // oauth | headscale (older state: apikey, wif or "")
	APIKeyFile string `json:"apiKeyFile,omitempty"` // headscale
	ControlURL string `json:"controlURL"`
	AddedAt    string `json:"addedAt,omitempty"`
	// OAuthClientFile is an oauth tenant's client file (path only).
	OAuthClientFile string `json:"oauthClientFile,omitempty"`
}

// tenants lists the operator tenant first, then the added ones. Linked
// to a remote mgmt, it is mgmt's tenant list (fetched once per command).
func (s *state) tenants() []TenantRef {
	if s.remote() {
		if s.remoteTenants == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			ts, _ := listAll[mgmt.Tenant](ctx, s.mgmt(), "/v1/tenants", 0)
			s.remoteTenants = []TenantRef{}
			for _, t := range ts {
				s.remoteTenants = append(s.remoteTenants, TenantRef{Tailnet: t.Tailnet, TenantID: t.ID, MintMode: t.MintMode, ControlURL: t.ControlURL})
			}
		}
		return s.remoteTenants
	}
	out := []TenantRef{}
	if s.Cfg.TenantID != "" {
		out = append(out, TenantRef{Tailnet: s.Cfg.Tailnet, TenantID: s.Cfg.TenantID, MintMode: s.Cfg.MintMode, APIKeyFile: s.Cfg.APIKeyFile, ControlURL: s.Cfg.ControlURL,
			OAuthClientFile: s.Cfg.OAuthClientFile})
	}
	return append(out, s.Cfg.Tenants...)
}

// tenant resolves --tenant (a tailnet, a tenant ID or the tailnet's first
// label); "" is the operator tenant.
func (s *state) tenant(ref string) (TenantRef, error) {
	ts := s.tenants()
	if ref == "" {
		if len(ts) == 0 {
			if s.remote() {
				return TenantRef{}, fail("no_tenant", "the linked mgmt "+s.Link.Endpoint+" has no tenant yet").withFix("finish `twd up` on the host")
			}
			return TenantRef{}, fail("not_up", "no tenant yet in "+s.Dir).withFix("twd up")
		}
		return ts[0], nil
	}
	ref = strings.TrimSuffix(ref, ".")
	for _, t := range ts {
		label, _, _ := strings.Cut(t.Tailnet, ".")
		if ref == t.Tailnet || ref == t.TenantID || ref == label {
			return t, nil
		}
	}
	var names []string
	for _, t := range ts {
		names = append(names, t.Tailnet)
	}
	return TenantRef{}, fail("unknown_tenant", fmt.Sprintf("no tenant %q", ref), "tenants: "+strings.Join(names, ", ")).
		withFix("twd tenant ls", "twd tenant add <tailnet> --oauth-client-file <file>   (on the host)")
}

func tenantFlag(f *flags) *string {
	return f.String("tenant", "", "tenant: its tailnet, tenant ID or tailnet label (default: the `twd up` tenant)")
}

// objectsOf lists a tenant's objects (every page; max > 0 caps the count).
func (s *state) objectsOf(ctx context.Context, tenantID string) ([]mgmt.Object, error) {
	return s.objectsOfN(ctx, tenantID, 0)
}

func (s *state) objectsOfN(ctx context.Context, tenantID string, max int) ([]mgmt.Object, error) {
	return listAll[mgmt.Object](ctx, s.mgmt(), "/v1/objects?tenant="+url.QueryEscape(tenantID), max)
}

func cmdTenant(args []string) error {
	// Global flags (--state-dir X, --json) may precede the subcommand.
	sub := -1
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			sub = i
			break
		}
		if (a == "--state-dir" || a == "-state-dir") && i+1 < len(args) {
			i++
		}
	}
	if sub < 0 {
		return fail("bad_request", "twd tenant needs a subcommand: add | ls | set | rm", "usage: twd "+usageOf("tenant"))
	}
	rest := append(append([]string{}, args[:sub]...), args[sub+1:]...)
	switch args[sub] {
	case "add":
		return cmdTenantAdd(rest)
	case "ls", "list":
		return cmdTenantLs(rest)
	case "set":
		return cmdTenantSet(rest)
	case "rm", "remove", "delete":
		return cmdTenantRm(rest)
	}
	return fail("bad_request", fmt.Sprintf("unknown subcommand %q", args[sub]), "usage: twd "+usageOf("tenant"))
}

type tenantAddResult struct {
	Tailnet    string             `json:"tailnet"`
	TenantID   string             `json:"tenantID"`
	ControlURL string             `json:"controlURL"`
	Existing   bool               `json:"existing"`
	PolicyOK   bool               `json:"policyOK"`
	PolicyDiff string             `json:"policySnippet,omitempty"`
	Applied    bool               `json:"policyApplied"`
	Timings    map[string]float64 `json:"timingsMs"`
}

// cmdTenantAdd adds a second (third, …) tenant to this host: it checks
// the tailnet's credential (an OAuth client, or a Headscale API key) works,
// registers the tenant with mgmt (its own throttle and mint queue) and
// records it in twd.json. The credential file's path is stored, never the
// secret.
func cmdTenantAdd(args []string) error {
	f := newFlags("tenant add")
	oauthFile := f.String("oauth-client-file", "", "this tailnet's OAuth client (mint mode \"oauth\"): 0600 file with TS_OAUTH_CLIENT_ID=… and TS_OAUTH_CLIENT_SECRET=tskey-client-… (Auth Keys write, tag tag:twd); read at use, never copied")
	keyFile := f.String("api-key-file", "", "--control=headscale only: 0600 file with HEADSCALE_API_KEY=…; read at use, never copied")
	profile := f.String("control", controlTailscale, "control-plane profile: tailscale, or headscale (ADR-015: a Headscale server at --control-url; the tailnet argument is its dns.base_domain)")
	apply := f.Bool("policy-apply", false, "apply the base policy snippet (validated first, written with If-Match); default: print it")
	control := f.String("control-url", prodControl, "coordination server for this tailnet's nodes")
	rpm := f.Int("registrations-per-minute", 0, "this tenant's node registration throttle (default 200/min)")
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail("bad_request", "twd tenant add needs one tailnet", "usage: twd "+usageOf("tenant"))
	}
	tailnet := strings.TrimSuffix(pos[0], ".")
	if err := checkControlProfile(*profile, *control, "tenant add"); err != nil {
		return err
	}
	hs := *profile == controlHeadscale
	switch {
	case hs && *oauthFile != "":
		return fail("profile_unsupported", "the Headscale profile mints with a Headscale API key: OAuth clients are a Tailscale feature").
			withFix("twd tenant add " + tailnet + " --control=headscale --control-url <server_url> --api-key-file <file with HEADSCALE_API_KEY=…>")
	case hs && *keyFile == "":
		_, err := headscaleKeyFile("")
		return err
	case !hs && *keyFile != "":
		return fail("mint_mode_removed", "twd no longer mints with a Tailscale API key (--api-key-file is for --control=headscale only)").
			withFix("twd tenant add " + tailnet + " --oauth-client-file <file>   (an OAuth client of that tailnet: Auth Keys write, tag tag:twd)")
	case !hs && *oauthFile == "":
		return fail("bad_request", "twd tenant add needs --oauth-client-file (an OAuth client of "+tailnet+")").
			withFix("twd tenant add " + tailnet + " --oauth-client-file <file>")
	}
	s, err := openState(*f.stateDir)
	if err != nil {
		return err
	}
	// A second tenant needs a process sandbox (or the explicit opt-out):
	// without one, every tenant's code shares one uid.
	if sb, _ := hostd.ResolveSandbox(hostd.SandboxAuto); sb == hostd.SandboxNone && !s.Cfg.InsecureSharedHost {
		why := "no process sandbox on " + runtime.GOOS
		if runtime.GOOS == "linux" {
			why = "Landlock is unavailable on this kernel"
		}
		return fail("sandbox_unavailable", "this host has no process sandbox ("+why+"), so its process tier must not serve more than one tenant",
			"twd doctor explains the sandbox check; tenants of a shared host need Seatbelt (macOS) or Landlock (Linux)").
			withFix("use a kernel with Landlock, deploy with tier \"microvm\", or (tenants NOT isolated) twd down && twd up --insecure-shared-host")
	}
	if s.remote() {
		return fail("remote_unsupported", "twd tenant add runs on the host that runs twd up (this machine is linked to "+s.Link.Endpoint+")")
	}
	if err := s.requireUp(); err != nil {
		return err
	}
	// Defence in depth (Linux): sockets this user can reach that would let
	// a process act as someone else. Container engines are root-equivalent
	// for twd itself: refuse. tailscaled's socket matters only if some
	// workload could reach it: every workload under Landlock+seccomp is
	// denied AF_UNIX, except Node apps listening on a unix socket.
	if hs := procsb.ScanHostSockets(); !hs.Empty() && !s.Cfg.InsecureSharedHost {
		fix := "run twd as a dedicated user that can't reach those sockets and isn't in the docker/podman/lxd groups, or (tenants NOT isolated) twd down && twd up --insecure-shared-host"
		if len(hs.Hard) > 0 {
			return fail("sandbox_unavailable", "this user can reach "+strings.Join(hs.Hard, ", ")+": root-equivalent for twd-hostd itself, so this host must not serve more than one tenant",
				"twd doctor lists them under host_sockets").withFix(fix)
		}
		if why := tailscaledExposed(s, hs.Tailscaled); why != "" {
			return fail("sandbox_unavailable", "this user can reach "+hs.Tailscaled+" (this host's tailnet identity) and "+why+", so this host must not serve more than one tenant",
				"twd doctor lists it under host_sockets").withFix(fix + "; or enable Landlock and use TCP listen mode for Node apps")
		}
	}
	t0 := time.Now()
	res := tenantAddResult{Tailnet: tailnet, ControlURL: *control, Timings: map[string]float64{}}
	absKey, _ := filepath.Abs(*keyFile)
	absOAuth, _ := filepath.Abs(*oauthFile)
	var creds tsapi.OAuthCredentials
	if !hs {
		absKey = ""
		if creds, err = tsapi.LoadOAuthFile(absOAuth); err != nil {
			if tsapi.IsInsecureFile(err) {
				return fail("credential_file_insecure", absOAuth+" is readable by other users", err.Error()).withFix("chmod 600 " + absOAuth + "   then twd tenant add again")
			}
			return fail("bad_request", "can't read the OAuth client from "+absOAuth, netd.Redact(err.Error())).
				withFix("the file needs TS_OAUTH_CLIENT_ID=<id> and TS_OAUTH_CLIENT_SECRET=tskey-client-… (mode 0600)")
		}
	} else {
		absOAuth = ""
	}
	cred, mode := absOAuth, mgmt.MintOAuth
	if hs {
		cred, mode = absKey, mgmt.MintHeadscale
	}
	for _, t := range s.tenants() {
		if (t.APIKeyFile == cred || t.OAuthClientFile == cred) && t.Tailnet != tailnet {
			return fail("state_conflict", "that credential file already serves tenant "+t.Tailnet,
				"a credential belongs to one tailnet; each tenant needs its own").withFix("twd tenant add " + tailnet + " --oauth-client-file <this tailnet's OAuth client file>")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var api controlAPI
	var pol []byte
	policyReadable := true
	if hs {
		if api, pol, err = headscaleClient(ctx, absKey, *control, tailnet); err != nil {
			return err
		}
	} else {
		src := &tsapi.OAuthSource{BaseURL: s.Cfg.TSAPIURL, Load: func() (tsapi.OAuthCredentials, error) { return tsapi.LoadOAuthFile(absOAuth) }}
		tc := &tsapi.Client{Tailnet: tailnet, Tokens: src, BaseURL: s.Cfg.TSAPIURL}
		if _, err := src.Token(ctx); err != nil {
			return fail("oauth_exchange_failed", "the OAuth client "+creds.ClientID+" can't get an access token", netd.Redact(err.Error())).
				withFix("check the client secret in " + absOAuth + " (a deleted or regenerated client fails here)")
		}
		if pol, _, err = tc.GetPolicyHuJSON(ctx); err != nil {
			// Reading the policy needs policy_file:read; without it the
			// tag ownership is proven by the first mint instead.
			pol, policyReadable = nil, false
		}
		api = tc
	}
	res.Timings["api_check"] = ms(time.Since(t0))

	tid := tenantIDFor(tailnet)
	var existing mgmt.Tenant
	if err := s.mgmt().Do(ctx, "GET", "/v1/tenants/"+tid, nil, &existing); err == nil {
		if existing.Tailnet != tailnet {
			return fail("state_conflict", fmt.Sprintf("tenant ID %s is taken by tailnet %s", tid, existing.Tailnet))
		}
		res.Existing = true
	} else {
		body := map[string]any{"tenantID": tid, "tailnet": tailnet, "controlURL": *control, "mintMode": mgmt.MintHeadscale, "apiKeyFile": absKey}
		if !hs {
			body = map[string]any{"tenantID": tid, "tailnet": tailnet, "controlURL": *control, "mintMode": mgmt.MintOAuth, "oauthClientFile": absOAuth, "oauthClientID": creds.ClientID}
		}
		if *rpm > 0 {
			body["registrationsPerMinute"] = *rpm
		}
		var tr struct {
			TenantID string `json:"tenantID"`
		}
		if err := s.mgmt().Do(ctx, "POST", "/v1/tenants", body, &tr); err != nil {
			return err
		}
		tid = tr.TenantID
	}
	res.TenantID = tid
	if tid != s.Cfg.TenantID {
		found := false
		for i, t := range s.Cfg.Tenants {
			if t.TenantID == tid {
				s.Cfg.Tenants[i].APIKeyFile, s.Cfg.Tenants[i].OAuthClientFile, s.Cfg.Tenants[i].ControlURL, found = absKey, absOAuth, *control, true
				s.Cfg.Tenants[i].MintMode = mode
			}
		}
		if !found {
			s.Cfg.Tenants = append(s.Cfg.Tenants, TenantRef{Tailnet: tailnet, TenantID: tid, MintMode: mode,
				APIKeyFile: absKey, OAuthClientFile: absOAuth, ControlURL: *control, AddedAt: time.Now().UTC().Format(time.RFC3339)})
		}
		if err := s.save(); err != nil {
			return err
		}
	}
	res.Timings["tenant_ready"] = ms(time.Since(t0))

	snip, popts := profileSnippet(*profile)
	miss, err := policy.Missing(pol, snip)
	if err != nil {
		return err
	}
	res.PolicyOK = miss.Empty() || !policyReadable
	if !res.PolicyOK {
		res.PolicyDiff = miss.HuJSON()
		if *apply {
			ar, err := policy.ApplyWith(ctx, api, snip, popts)
			if err != nil {
				return fail("policy_apply_failed", "could not apply the policy snippet", err.Error()).withFix("add it in "+policyHome(*profile)+":", res.PolicyDiff)
			}
			res.Applied, res.PolicyOK = ar.Changed, true
		}
	}
	res.Timings["total"] = ms(time.Since(t0))
	emit(f, res, func() {
		verb := "added"
		if res.Existing {
			verb = "already served"
		}
		how := "OAuth client " + creds.ClientID + ", mint mode oauth"
		if hs {
			how = "Headscale API key, mint mode headscale"
		}
		fmt.Printf("✓ tenant   %s %s (tenant %s, %s) · control %s\n", tailnet, verb, tid, how, *control)
		fmt.Printf("  isolated: its own netd registry, workerd processes, state dir (0700) and registration throttle\n")
		switch {
		case res.Applied:
			fmt.Printf("✓ policy   applied tag:twd tagOwners (validated, If-Match)\n")
		case res.PolicyOK && !policyReadable:
			fmt.Printf("✓ policy   not checked (the OAuth client can't read the policy); the first mint proves tag:twd ownership\n")
		case res.PolicyOK:
			fmt.Printf("✓ policy   tag:twd tagOwners present\n")
		default:
			fmt.Printf("› policy   not applied (re-run with --policy-apply, or add in %s):\n", policyHome(*profile))
			for _, l := range strings.Split(strings.TrimSpace(res.PolicyDiff), "\n") {
				fmt.Printf("    %s\n", l)
			}
		}
		if *profile == controlHeadscale {
			fmt.Printf("› profile  %s\n", headscaleProfileNote)
		}
		fmt.Printf("deploy to it with `twd deploy --tenant %s`\n", tailnet)
	})
	return nil
}

type tenantLsEntry struct {
	TenantRef
	Operator bool           `json:"operator"`
	Objects  map[string]int `json:"objectsByStatus"`
	Rate     float64        `json:"registrationsPerMinute"`
	Queue    int            `json:"mintQueue"`
}

func cmdTenantLs(args []string) error {
	f := newFlags("tenant ls")
	if _, err := f.parse(args); err != nil {
		return err
	}
	s, err := openState(*f.stateDir)
	if err != nil {
		return err
	}
	if err := s.requireUp(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out := []tenantLsEntry{}
	for i, t := range s.tenants() {
		e := tenantLsEntry{TenantRef: t, Operator: i == 0, Objects: map[string]int{}}
		var tn struct {
			Q    int     `json:"mintQueue"`
			Rate float64 `json:"effectiveRegistrationsPerMinute"`
		}
		s.mgmt().Do(ctx, "GET", "/v1/tenants/"+t.TenantID, nil, &tn)
		e.Rate, e.Queue = tn.Rate, tn.Q
		if objs, err := s.objectsOf(ctx, t.TenantID); err == nil {
			for _, o := range objs {
				e.Objects[o.Status]++
			}
		}
		out = append(out, e)
	}
	emit(f, out, func() {
		fmt.Printf("  %-24s %-18s %-9s %-14s %s\n", "TAILNET", "TENANT", "ROLE", "THROTTLE", "OBJECTS")
		for _, e := range out {
			role := "tenant"
			if e.Operator {
				role = "operator"
			}
			fmt.Printf("  %-24s %-18s %-9s %-14s %v\n", e.Tailnet, e.TenantID, role, fmt.Sprintf("%.0f/min", e.Rate), e.Objects)
		}
	})
	return nil
}

// tailscaledExposed says why a workload on this host could reach the system
// tailscaled's socket ("" if none can): the sandbox isn't Landlock+seccomp,
// or a running Node app listens on a unix socket. It asks twd-hostd.
func tailscaledExposed(s *state, sock string) string {
	if sock == "" {
		return ""
	}
	if sb, _ := hostd.ResolveSandbox(hostd.SandboxAuto); sb != hostd.SandboxLandlock {
		return "the process sandbox (" + sb + ") doesn't deny unix sockets"
	}
	if s == nil || !s.supervisorAlive() {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var hv hostd.StatusView
	if err := s.hostd().Do(ctx, "GET", "/v1/status", nil, &hv); err != nil {
		return "twd-hostd is not answering, so running workloads can't be checked"
	}
	if !strings.HasPrefix(hv.Sandbox, hostd.SandboxLandlock) {
		return "twd-hostd runs with sandbox " + hv.Sandbox
	}
	for _, w := range hv.Workloads {
		if w.Node != nil && w.Node.Listen == "unix" {
			return "Node app " + w.TenantID + "/" + w.Workload + " listens on a unix socket (that mode keeps AF_UNIX sockets)"
		}
	}
	return ""
}

// cmdTenantSet is `twd tenant set <tenant> [--max-objects N] [--registrations-per-minute N]
// [--pool P] [--tier process|microvm] [--oauth-client-file FILE] …`:
// a JSON Merge Patch (PATCH /v1/tenants/{id}); "-" clears a field.
// --oauth-client-file switches the tenant to mint mode oauth (how a tenant
// an older twd left in apikey or wif mode mints again).
func cmdTenantSet(args []string) error {
	f := newFlags("tenant set")
	maxObj := f.Int("max-objects", 0, "object quota")
	rpm := f.Int("registrations-per-minute", 0, "node registration throttle")
	pool := f.String("pool", "", "host pool the tenant's deployments are placed on (\"-\" = any host)")
	tier := f.String("tier", "", "default isolation tier: process | microvm (\"-\" = unset)")
	oauthFile := f.String("oauth-client-file", "", "switch the tenant to mint mode oauth with this 0600 OAuth client file (on this host; read at use, never copied)")
	maxCtr := f.Int("max-containers", 0, "containers (ADR-032): running containers quota")
	maxCtrMem := f.Int("max-container-mem-mib", 0, "containers: total guest memory quota (MiB)")
	maxCtrCPU := f.Float64("max-container-vcpu", 0, "containers: total vCPU quota (fractional, e.g. 2.5)")
	pubIngress := f.String("public-ingress", "", "public (Funnel) ingress gate for twd.jsonc \"public\" routes: allow | deny (ADR-029)")
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail("bad_request", "twd tenant set needs one tenant", "usage: twd "+usageOf("tenant"))
	}
	s, err := openState(*f.stateDir)
	if err != nil {
		return err
	}
	if err := s.requireUp(); err != nil {
		return err
	}
	tn, err := s.tenant(pos[0])
	if err != nil {
		return err
	}
	patch := map[string]any{}
	val := func(v string) any {
		if v == "-" {
			return nil
		}
		return v
	}
	if *maxObj > 0 {
		patch["maxObjects"] = *maxObj
	}
	if *rpm > 0 {
		patch["registrationsPerMinute"] = *rpm
	}
	if *pool != "" {
		patch["pool"] = val(*pool)
	}
	if *tier != "" {
		patch["tier"] = val(*tier)
	}
	absOAuth := ""
	if *oauthFile != "" {
		if s.remote() {
			return fail("remote_unsupported", "--oauth-client-file names a file on the host: run twd tenant set there (this machine is linked to "+s.Link.Endpoint+")")
		}
		absOAuth, _ = filepath.Abs(*oauthFile)
		if _, err := tsapi.LoadOAuthFile(absOAuth); err != nil {
			if tsapi.IsInsecureFile(err) {
				return fail("credential_file_insecure", absOAuth+" is readable by other users", err.Error()).withFix("chmod 600 " + absOAuth)
			}
			return fail("bad_request", "can't read the OAuth client from "+absOAuth, netd.Redact(err.Error()))
		}
		patch["mintMode"], patch["oauthClientFile"], patch["oauthClientID"] = mgmt.MintOAuth, absOAuth, nil
	}
	if *maxCtr > 0 {
		patch["maxContainers"] = *maxCtr
	}
	if *maxCtrMem > 0 {
		patch["maxContainerMemMiB"] = *maxCtrMem
	}
	if *maxCtrCPU > 0 {
		patch["maxContainerVCPU"] = *maxCtrCPU
	}
	switch *pubIngress {
	case "":
	case "allow", "deny":
		patch["publicIngress"] = *pubIngress
	default:
		return fail("bad_request", "--public-ingress must be allow or deny", "usage: twd "+usageOf("tenant"))
	}
	if len(patch) == 0 {
		return fail("bad_request", "nothing to set", "usage: twd "+usageOf("tenant"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var t mgmt.Tenant
	if err := s.mgmt().DoHeaders(ctx, "PATCH", "/v1/tenants/"+tn.TenantID, patch, &t,
		map[string]string{"Content-Type": "application/merge-patch+json", "Twd-Actor": actor()}); err != nil {
		return err
	}
	if absOAuth != "" && !s.remote() {
		// Keep twd.json in step: the supervisor denies the client file to
		// every sandbox, and twd up reads the mode from here.
		if tn.TenantID == s.Cfg.TenantID {
			s.Cfg.MintMode, s.Cfg.OAuthClientFile, s.Cfg.OAuthClientID, s.Cfg.APIKeyFile = mgmt.MintOAuth, absOAuth, t.OAuthClientID, ""
		}
		for i := range s.Cfg.Tenants {
			if s.Cfg.Tenants[i].TenantID == tn.TenantID {
				s.Cfg.Tenants[i].MintMode, s.Cfg.Tenants[i].OAuthClientFile, s.Cfg.Tenants[i].APIKeyFile = mgmt.MintOAuth, absOAuth, ""
			}
		}
		if err := s.save(); err != nil {
			return err
		}
	}
	emit(f, t, func() {
		gate := t.PublicIngress
		if gate == "" {
			gate = "deny"
		}
		fmt.Printf("✓ tenant   %s (%s) updated · mint mode %s · %d objects max · %d registrations/min · pool %q · tier %q · containers ≤ %d, ≤ %d MiB, ≤ %g vCPU · public ingress %s\n",
			t.Tailnet, t.ID, t.MintMode, t.MaxObjects, t.RegistrationsPerMinute, t.Pool, t.Tier, t.MaxContainers, t.MaxContainerMemMiB, t.MaxContainerVCPU, gate)
	})
	return nil
}

// cmdTenantRm is `twd tenant rm <tenant> [--force]` (DELETE /v1/tenants/{id}):
// refused while objects are live unless --force kills and purges them.
func cmdTenantRm(args []string) error {
	f := newFlags("tenant rm")
	force := f.Bool("force", false, "kill every live object (cascade) and purge before deleting")
	yes := yesFlag(f)
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail("bad_request", "twd tenant rm needs one tenant", "usage: twd "+usageOf("tenant"))
	}
	if err := confirmDestroy(f, *yes, "removing tenant "+pos[0]+" stops serving it from this mgmt and deletes its deployments, queues and stores", pos[0], "twd tenant rm "+pos[0]); err != nil {
		return err
	}
	s, err := openState(*f.stateDir)
	if err != nil {
		return err
	}
	if err := s.requireUp(); err != nil {
		return err
	}
	tn, err := s.tenant(pos[0])
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	q := ""
	if *force {
		q = "?force=true"
	}
	var res struct {
		TenantID           string   `json:"tenantID"`
		Killed             []string `json:"killed"`
		Purged             []string `json:"purged"`
		DeploymentsStopped []string `json:"deploymentsStopped"`
	}
	if err := s.mgmt().DoHeaders(ctx, "DELETE", "/v1/tenants/"+tn.TenantID+q, nil, &res, map[string]string{"Twd-Actor": actor()}); err != nil {
		if me, ok := mgmtapi.AsError(err); ok && me.Code == "tenant_has_live_objects" {
			return fail("tenant_has_live_objects", me.Message).withFix("twd tenant rm " + pos[0] + " --force --yes")
		}
		return err
	}
	// Forget it locally too.
	if !s.remote() {
		if s.Cfg.TenantID == tn.TenantID {
			s.Cfg.TenantID = ""
		}
		s.Cfg.Tenants = slices.DeleteFunc(s.Cfg.Tenants, func(t TenantRef) bool { return t.TenantID == tn.TenantID })
		s.save()
	}
	res.TenantID = tn.TenantID
	emit(f, res, func() {
		fmt.Printf("✓ tenant   %s (%s) removed · %d objects killed · %d forgotten · %d deployments stopped\n", tn.Tailnet, tn.TenantID, len(res.Killed), len(res.Purged), len(res.DeploymentsStopped))
	})
	return nil
}
