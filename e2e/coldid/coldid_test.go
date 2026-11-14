//go:build e2e || e2ereal

// Package coldid measures a new object's cold identity: create → the
// object's own node reachable from a caller, stage by stage. It drives the
// real code path: twd-mgmt (in process, SQLite, its v1 API on a unix
// socket, exactly as hostd calls it) mints through the mint worker and
// releases the key over the fenced credential long-poll; a netd Registry
// joins the node with that key (hostd's KeySource loop); a separate caller
// node watches its netmap for the new peer, then dials it.
//
// Every stage is a timestamp, from three sources: mgmt's "mint spans" /
// "credential released" log lines, netd's JoinSpans (IPN bus + controlclient
// log markers), and the caller (IPN bus PeersChanged, the peer's
// LastHandshake, the first HTTP 200 and its path).
//
//	make e2e-coldid        # hermetic (testcontrol + local DERP)
//	make e2e-coldid-real   # tailnet 1, production control
package coldid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rosskukulinski/twd/mgmt"
	"github.com/rosskukulinski/twd/netd"
	"github.com/rosskukulinski/twd/netd/nettest"
	"tailscale.com/ipn"
)

// env is what differs between the hermetic and the real run.
type env struct {
	name       string // "local" | "real"
	controlURL string
	tailnet    string
	apiBase    string // the Tailscale API for mgmt's broker ("" = production)
	apiKeyFile string
	tags       []string
	prefix     string // class slug: object IDs/hostnames are <prefix>-<id10>
	callerKey  func(ctx context.Context) (string, error)
	track      func(stableNodeID, label string) // real: ID-based cleanup
	n          int                              // objects to create (sequentially)
	gap        time.Duration                    // pause between creates
	mutate     func(*mgmt.Config)               // optional (A/B experiments)
	// dialAfterReady: the caller waits for the object's node to be ready
	// (what a client told "ready" by twd does) instead of dialing as soon as
	// the peer is in its netmap (what a client polling MagicDNS does).
	dialAfterReady bool
	onMint         func(keyID string) // every key mgmt minted (cleanup)
}

// sample is one object's timeline, every field ms after the create call.
type sample struct {
	Object string `json:"object"`
	// mgmt
	CreateRPC   float64 `json:"createRPC"`   // POST /v1/objects returned
	MintStart   float64 `json:"mintStart"`   // mintOne began (queued)
	Throttle    float64 `json:"throttle"`    // claim + throttle token + re-claim done
	CreateKey   float64 `json:"createKey"`   // CreateKey returned
	Minted      float64 `json:"minted"`      // custody put + fenced commit done
	CreateKeyMS float64 `json:"createKeyMs"` // the API call alone
	Released    float64 `json:"released"`    // credential released (mgmt side)
	// netd (the object's own node)
	NetdCreate   float64 `json:"netdCreate"`
	Key          float64 `json:"key"`
	Started      float64 `json:"started"`
	Login        float64 `json:"login"`
	RegisterSent float64 `json:"registerSent"`
	Registered   float64 `json:"registered"`
	SelfNetmap   float64 `json:"selfNetmap"`
	Running      float64 `json:"running"`
	Ready        float64 `json:"ready"`
	// the caller
	PeerSeen     float64 `json:"peerSeen"`     // the new node is in the caller's netmap
	PeerDERP     float64 `json:"peerDERP"`     // …with a home DERP region
	PeerEps      float64 `json:"peerEps"`      // …with endpoints
	Handshake    float64 `json:"handshake"`    // first WireGuard handshake (peer LastHandshake)
	FirstOK      float64 `json:"firstOK"`      // first HTTP 200 over the object's own node
	FirstPath    string  `json:"firstPath"`    // "direct" | "derp:<region>" at the first 200
	Direct       float64 `json:"direct"`       // first time CurAddr was set (0 = not within the window)
	HTTPAttempts int     `json:"httpAttempts"` // GETs until the first 200
}

// logCapture keeps every mgmt/netd record so spans can be read back.
type logCapture struct {
	mu   sync.Mutex
	recs []capRec
	buf  bytes.Buffer // text copy for the secret scan
}

type capRec struct {
	t     time.Time
	msg   string
	attrs map[string]any
}

type capHandler struct {
	c     *logCapture
	attrs []slog.Attr
}

func (h capHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h capHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return capHandler{h.c, append(append([]slog.Attr(nil), h.attrs...), a...)}
}
func (h capHandler) WithGroup(string) slog.Handler { return h }
func (h capHandler) Handle(_ context.Context, r slog.Record) error {
	m := map[string]any{}
	for _, a := range h.attrs {
		m[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool { m[a.Key] = a.Value.Resolve().Any(); return true })
	h.c.mu.Lock()
	h.c.recs = append(h.c.recs, capRec{t: r.Time, msg: r.Message, attrs: m})
	fmt.Fprintf(&h.c.buf, "%s %s %v\n", r.Time.Format(time.RFC3339Nano), r.Message, m)
	h.c.mu.Unlock()
	return nil
}

func (c *logCapture) find(msg, object string) (capRec, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.recs {
		if r.msg == msg && r.attrs["object"] == object {
			return r, true
		}
	}
	return capRec{}, false
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case int:
		return float64(x)
	}
	return 0
}

func msSince(t0, t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.Sub(t0).Microseconds()) / 1000
}

// peerWatch timestamps the first time each hostname appears in the
// caller's netmap (IPN bus PeersChanged).
type peerWatch struct {
	mu   sync.Mutex
	seen map[string]time.Time
	derp map[string]time.Time // first entry with a home DERP region
	eps  map[string]time.Time // first entry with endpoints
	ips  map[string]string
	cond chan struct{}
}

func (p *peerWatch) run(ctx context.Context, n *nettest.Node) error {
	w, err := n.LC.WatchIPNBus(ctx, ipn.NotifyPeerChanges)
	if err != nil {
		return err
	}
	go func() {
		defer w.Close()
		for {
			nt, err := w.Next()
			if err != nil {
				return
			}
			now := time.Now()
			for _, pn := range nt.PeersChanged {
				if pn == nil {
					continue
				}
				h := pn.Hostinfo.Hostname()
				name := strings.SplitN(pn.Name, ".", 2)[0]
				p.mu.Lock()
				for _, k := range []string{h, name} {
					if k == "" {
						continue
					}
					if _, ok := p.derp[k]; !ok && pn.HomeDERP != 0 {
						p.derp[k] = now
					}
					if _, ok := p.eps[k]; !ok && len(pn.Endpoints) > 0 {
						p.eps[k] = now
					}
					if _, ok := p.seen[k]; !ok {
						p.seen[k] = now
						if len(pn.Addresses) > 0 {
							p.ips[k] = pn.Addresses[0].Addr().String()
						}
					}
				}
				close(p.cond)
				p.cond = make(chan struct{})
				p.mu.Unlock()
			}
		}
	}()
	return nil
}

func (p *peerWatch) wait(ctx context.Context, host string) (time.Time, string, error) {
	for {
		p.mu.Lock()
		t, ok := p.seen[host]
		ip := p.ips[host]
		ch := p.cond
		p.mu.Unlock()
		if ok {
			return t, ip, nil
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return time.Time{}, "", ctx.Err()
		}
	}
}

func runColdIdentity(t *testing.T, e env) []sample {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	dir, err := os.MkdirTemp("/tmp", "twd-coldid-")
	if err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o700)
	t.Cleanup(func() { os.RemoveAll(dir) })

	capt := &logCapture{}
	log := slog.New(netd.NewRedactingHandler(capHandler{c: capt}))
	t.Cleanup(func() {
		capt.mu.Lock()
		defer capt.mu.Unlock()
		if strings.Contains(capt.buf.String(), "tskey-") {
			t.Error("a key reached the logs")
		} else if f := os.Getenv("TWD_COLDID_DUMP"); strings.HasPrefix(f, "/") {
			os.WriteFile(f, capt.buf.Bytes(), 0o600) // redacted tsnet + mgmt logs, for timelines
		}
		for _, r := range capt.recs {
			if id, _ := r.attrs["keyID"].(string); r.msg == "mint spans" && id != "" && e.onMint != nil {
				e.onMint(id)
			}
		}
	})

	store, err := mgmt.OpenStore(ctx, filepath.Join(dir, "mgmt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	custody, err := mgmt.OpenFileCustody(filepath.Join(dir, "custody"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := mgmt.Config{Store: store, Custody: custody, Broker: &mgmt.Broker{BaseURL: e.apiBase}, IDSecret: bytes.Repeat([]byte{7}, 32),
		ReapInterval: time.Hour, Log: log}
	if e.mutate != nil {
		e.mutate(&cfg)
	}
	m, err := mgmt.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)

	sock := filepath.Join(dir, "run", "mgmt.sock")
	ln, err := mgmt.ListenUnix(sock)
	if err != nil {
		t.Fatal(err)
	}
	hostToken := mgmt.NewHostToken()
	srv := &http.Server{Handler: mgmt.LocalHandler((&mgmt.Server{M: m}).Handler(), hostToken)}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	hc := &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
	hostID := "h-" + e.prefix
	call := func(ctx context.Context, method, path string, body, out any) (int, error) {
		var rd io.Reader
		if body != nil {
			bb, _ := json.Marshal(body)
			rd = bytes.NewReader(bb)
		}
		req, _ := http.NewRequestWithContext(ctx, method, "http://twd-mgmt"+path, rd)
		if rd != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Twd-Host-ID", hostID)
		req.Header.Set(mgmt.HostTokenHeader, hostToken)
		resp, err := hc.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		rb, _ := io.ReadAll(resp.Body)
		if resp.StatusCode/100 == 2 && out != nil {
			json.Unmarshal(rb, out)
		} else if resp.StatusCode/100 != 2 {
			return resp.StatusCode, fmt.Errorf("%s %s → %d %s", method, path, resp.StatusCode, rb) // error bodies never carry keys
		}
		return resp.StatusCode, nil
	}
	var tn struct{ TenantID string }
	if _, err := call(ctx, "POST", "/v1/tenants", map[string]any{"tailnet": e.tailnet, "controlURL": e.controlURL,
		"mintMode": "apikey", "apiKeyFile": e.apiKeyFile}, &tn); err != nil {
		t.Fatal(err)
	}
	if _, err := call(ctx, "PUT", "/v1/hosts/"+hostID, map[string]any{"capacityNodes": 64, "tiers": []string{"process"}}, nil); err != nil {
		t.Fatal(err)
	}
	var dep mgmt.DeployResponse
	if _, err := call(ctx, "POST", "/v1/deployments", map[string]any{"tenantID": tn.TenantID, "workload": e.prefix,
		"namespaces": []mgmt.ClassConfig{{Class: e.prefix, Tags: e.tags, Agent: true}}}, &dep); err != nil {
		t.Fatal(err)
	}

	// hostd's KeySource: the fenced credential long-poll (wait=10 s), retried
	// on identity_pending with hostd's backoff.
	keys := netd.MintFunc(func(ctx context.Context, req netd.KeyRequest) (string, error) {
		backoff := 100 * time.Millisecond
		for {
			var c struct{ AuthKey string }
			st, err := call(ctx, "POST", "/v1/objects/"+req.ObjectID+"/credentials?epoch="+strconv.FormatInt(dep.Epoch, 10)+"&wait=10", nil, &c)
			if err == nil {
				return c.AuthKey, nil
			}
			if st != 503 && st != 429 && st != 425 {
				return "", err
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(2*backoff, 2*time.Second)
		}
	})
	reg, err := netd.New(netd.Config{ControlURL: e.controlURL, StateRoot: filepath.Join(dir, "nodes"), Keys: keys, Ephemeral: true,
		Tags: e.tags, Logger: log, TSNetLogs: os.Getenv("TWD_COLDID_DUMP") != "", Upstream: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, o := range reg.Objects() {
			if o.NodeID != "" && e.track != nil {
				e.track(o.NodeID, "obj-"+o.ID)
			}
		}
		reg.Close()
	})

	ck, err := e.callerKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	clog := log.With("object", "caller", "src", "tsnet")
	caller, err := nettest.NewNodeLogf(ctx, e.controlURL, ck, e.prefix+"-caller", filepath.Join(dir, "caller"), func(f string, a ...any) {
		if os.Getenv("TWD_COLDID_DUMP") != "" {
			clog.Debug(netd.Redact(fmt.Sprintf(f, a...)))
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.track != nil {
		if st, err := caller.LC.StatusWithoutPeers(ctx); err == nil && st.Self != nil {
			e.track(string(st.Self.ID), "caller")
		}
	}
	t.Cleanup(func() { caller.Close() })
	pw := &peerWatch{seen: map[string]time.Time{}, derp: map[string]time.Time{}, eps: map[string]time.Time{}, ips: map[string]string{}, cond: make(chan struct{})}
	if err := pw.run(ctx, caller); err != nil {
		t.Fatal(err)
	}
	httpc := caller.HTTPClient(3 * time.Second)

	var out []sample
	for i := 0; i < e.n; i++ {
		s, err := oneObject(ctx, t, e, call, tn.TenantID, reg, caller, pw, httpc, capt, i)
		if err != nil {
			t.Errorf("object %d: %v", i, err)
			continue
		}
		out = append(out, s)
		if i == 0 && os.Getenv("TWD_COLDID_DUMP") != "" {
			capt.mu.Lock()
			for _, l := range strings.Split(capt.buf.String(), "\n") {
				if strings.Contains(l, s.Object) {
					t.Log(l)
				}
			}
			capt.mu.Unlock()
		}
		t.Logf("%-28s derp %6.0f n=%d mint %6.0f  key@netd %6.0f  regSent %6.0f  registered %6.0f  ready %6.0f  peerSeen %6.0f  hs %6.0f  firstOK %6.0f (%s)",
			s.Object, s.PeerDERP, s.HTTPAttempts, s.Minted, s.Key, s.RegisterSent, s.Registered, s.Ready, s.PeerSeen, s.Handshake, s.FirstOK, s.FirstPath)
		if e.gap > 0 {
			time.Sleep(e.gap)
		}
	}
	return out
}

func oneObject(ctx context.Context, t *testing.T, e env, call func(context.Context, string, string, any, any) (int, error), tenant string,
	reg *netd.Registry, caller *nettest.Node, pw *peerWatch, httpc *http.Client, capt *logCapture, i int) (sample, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var s sample
	var cr mgmt.CreateObjectResponse
	t0 := time.Now()
	if _, err := call(ctx, "POST", "/v1/objects", map[string]any{"tenantID": tenant, "workload": e.prefix, "namespace": e.prefix,
		"objectName": fmt.Sprintf("o%03d", i)}, &cr); err != nil {
		return s, err
	}
	s.Object, s.CreateRPC = cr.ObjectID, msSince(t0, time.Now())
	// hostd starts the node right after create returns (async join).
	tn := time.Now()
	no, err := reg.Create(cr.ObjectID, netd.ObjectOptions{Hostname: cr.Hostname, Tags: cr.Tags})
	if err != nil {
		return s, err
	}
	s.NetdCreate = msSince(t0, tn)

	// The caller, concurrently: wait for the peer, then GET until 200.
	type callerRes struct {
		seen, hs, ok, direct time.Time
		path                 string
		attempts             int
		err                  error
	}
	cch := make(chan callerRes, 1)
	go func() {
		var r callerRes
		var ip string
		r.seen, ip, r.err = pw.wait(ctx, cr.Hostname)
		if r.err == nil && e.dialAfterReady {
			r.err = no.Ready(ctx)
		}
		if r.err != nil {
			cch <- r
			return
		}
		// Sample the peer's WireGuard state while the first request runs.
		stop := make(chan struct{})
		var smu sync.Mutex
		go func() {
			tk := time.NewTicker(10 * time.Millisecond)
			defer tk.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tk.C:
				}
				st, err := caller.LC.Status(ctx)
				if err != nil {
					continue
				}
				for _, p := range st.Peer {
					if len(p.TailscaleIPs) == 0 || p.TailscaleIPs[0].String() != ip {
						continue
					}
					smu.Lock()
					if r.hs.IsZero() && !p.LastHandshake.IsZero() && p.LastHandshake.After(t0) {
						r.hs = p.LastHandshake
					}
					if r.direct.IsZero() && p.CurAddr != "" {
						r.direct = time.Now()
					}
					smu.Unlock()
				}
			}
		}()
		for {
			r.attempts++
			resp, err := httpc.Get("http://" + ip + "/")
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == 200 {
					r.ok = time.Now()
					break
				}
			}
			if ctx.Err() != nil {
				r.err = ctx.Err()
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		// Which path carried it, and does it go direct shortly after?
		if st, err := caller.LC.Status(ctx); err == nil {
			for _, p := range st.Peer {
				if len(p.TailscaleIPs) > 0 && p.TailscaleIPs[0].String() == ip {
					if p.CurAddr != "" {
						r.path = "direct"
					} else {
						r.path = "derp:" + p.Relay
					}
				}
			}
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			smu.Lock()
			d := !r.direct.IsZero()
			smu.Unlock()
			if d {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		close(stop)
		smu.Lock()
		defer smu.Unlock()
		cch <- r
	}()

	if err := no.Ready(ctx); err != nil {
		return s, fmt.Errorf("join: %w", err)
	}
	cr2 := <-cch
	if cr2.err != nil {
		return s, fmt.Errorf("caller: %w", cr2.err)
	}
	s.PeerSeen, s.Handshake, s.FirstOK, s.Direct = msSince(t0, cr2.seen), msSince(t0, cr2.hs), msSince(t0, cr2.ok), msSince(t0, cr2.direct)
	s.FirstPath, s.HTTPAttempts = cr2.path, cr2.attempts
	pw.mu.Lock()
	s.PeerDERP, s.PeerEps = msSince(t0, pw.derp[cr.Hostname]), msSince(t0, pw.eps[cr.Hostname])
	pw.mu.Unlock()

	sp := no.Info().Spans
	off := s.NetdCreate
	at := func(v float64) float64 {
		if v == 0 {
			return 0
		}
		return off + v
	}
	s.Key, s.Started, s.Login, s.RegisterSent, s.Registered = at(sp.Key), at(sp.Start), at(sp.Login), at(sp.RegisterSent), at(sp.Registered)
	s.SelfNetmap, s.Running, s.Ready = at(sp.Netmap), at(sp.Running), at(sp.Listening)

	if r, ok := capt.find("mint spans", cr.ObjectID); ok {
		// Reconstruct from the end: the record is logged right after the commit.
		end := msSince(t0, r.t)
		commit, custody, ck := num(r.attrs["commit_ms"]), num(r.attrs["custody_ms"]), num(r.attrs["createkey_ms"])
		s.Minted = end
		s.CreateKey = end - commit - custody
		s.CreateKeyMS = ck
		s.Throttle = s.CreateKey - ck - num(r.attrs["broker_ms"])
		s.MintStart = end - num(r.attrs["attempt_ms"])
	}
	if r, ok := capt.find("credential released", cr.ObjectID); ok {
		s.Released = msSince(t0, r.t)
	}
	return s, nil
}

// ---- reporting ----

type stage struct {
	name     string
	from, to func(sample) float64
	abs      bool // from is the create call itself (0 is a real mark)
}

// delta is the stage's duration in s, or false if a mark is missing.
func (st stage) delta(s sample) (float64, bool) {
	a, z := st.from(s), st.to(s)
	if z == 0 || (a == 0 && !st.abs) {
		return 0, false
	}
	return z - a, true
}

func pctl(v []float64, p float64) float64 {
	if len(v) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	i := int(math.Ceil(p*float64(len(s)))) - 1
	return s[max(0, min(i, len(s)-1))]
}

var stages = []stage{
	{"mgmt: POST /v1/objects (row + audit, enqueue)", func(sample) float64 { return 0 }, func(s sample) float64 { return s.CreateRPC }, true},
	{"mgmt: mint queue → mintOne", func(sample) float64 { return 0 }, func(s sample) float64 { return s.MintStart }, true},
	{"mgmt: claim + throttle token + re-claim", func(s sample) float64 { return s.MintStart }, func(s sample) float64 { return s.Throttle }, false},
	{"mgmt: CreateKey API call", func(s sample) float64 { return s.CreateKey - s.CreateKeyMS }, func(s sample) float64 { return s.CreateKey }, false},
	{"mgmt: custody put + fenced commit → minted", func(s sample) float64 { return s.CreateKey }, func(s sample) float64 { return s.Minted }, false},
	{"hostd: credential long-poll wakes → key in netd", func(s sample) float64 { return s.Minted }, func(s sample) float64 { return s.Key }, false},
	{"netd: tsnet Start (after the key)", func(s sample) float64 { return s.Key }, func(s sample) float64 { return s.Started }, false},
	{"tsnet: Start → doLogin", func(s sample) float64 { return s.Started }, func(s sample) float64 { return s.Login }, false},
	{"tsnet: doLogin → RegisterRequest sent (Noise dial)", func(s sample) float64 { return s.Login }, func(s sample) float64 { return s.RegisterSent }, false},
	{"control: RegisterRequest → response", func(s sample) float64 { return s.RegisterSent }, func(s sample) float64 { return s.Registered }, false},
	{"control: registered → node's first netmap", func(s sample) float64 { return s.Registered }, func(s sample) float64 { return s.SelfNetmap }, false},
	{"tsnet: first netmap → Running → listener", func(s sample) float64 { return s.SelfNetmap }, func(s sample) float64 { return s.Ready }, false},
	{"control: registered → in the caller's netmap", func(s sample) float64 { return s.Registered }, func(s sample) float64 { return s.PeerSeen }, false},
	{"control: node Running → caller's netmap has its home DERP", func(s sample) float64 { return s.Running }, func(s sample) float64 { return s.PeerDERP }, false},
	{"control: node Running → caller's netmap has its endpoints", func(s sample) float64 { return s.Running }, func(s sample) float64 { return s.PeerEps }, false},
	{"caller: own node ready → first HTTP 200 (WireGuard first handshake)", func(s sample) float64 { return s.Ready }, func(s sample) float64 { return s.FirstOK }, false},
	{"**total: create → own node ready (netd)**", func(sample) float64 { return 0 }, func(s sample) float64 { return s.Ready }, true},
	{"**total: create → first 200 on the own node**", func(sample) float64 { return 0 }, func(s sample) float64 { return s.FirstOK }, true},
}

func report(t *testing.T, e env, ss []sample) string {
	var b strings.Builder
	fmt.Fprintf(&b, "cold identity, %s, n=%d\n| stage | p50 ms | p95 ms | max ms |\n|---|---|---|---|\n", e.name, len(ss))
	for _, st := range stages {
		var v []float64
		for _, s := range ss {
			if d, ok := st.delta(s); ok {
				v = append(v, d)
			}
		}
		fmt.Fprintf(&b, "| %s | %.0f | %.0f | %.0f |\n", st.name, pctl(v, .5), pctl(v, .95), pctl(v, 1))
	}
	paths := map[string]int{}
	for _, s := range ss {
		paths[strings.SplitN(s.FirstPath, ":", 2)[0]]++
	}
	fmt.Fprintf(&b, "first-200 path: %v\n", paths)
	root, _ := nettest.FindRepoRoot()
	f := filepath.Join(root, "e2e", "coldid", "results", fmt.Sprintf("%s-%s-%s.json", e.name, time.Now().Format("20060102-150405"), e.prefix))
	os.MkdirAll(filepath.Dir(f), 0o755)
	j, _ := json.MarshalIndent(map[string]any{"env": e.name, "prefix": e.prefix, "tailnet": e.tailnet, "date": time.Now().Format(time.RFC3339),
		"samples": ss, "table": b.String()}, "", "  ")
	os.WriteFile(f, j, 0o644)
	t.Logf("results: %s\n%s", f, b.String())
	return b.String()
}

// TestColdIdentityAggregate pools result files into one p50/p95 table:
//
//	TWD_COLDID_AGG='results/real-*.json' go test -tags e2e -run Aggregate ./e2e/coldid/
//
// Node-side stages pool every file; caller stages are split by dial mode
// (files whose env ends in -dialready vs the rest).
func TestColdIdentityAggregate(t *testing.T) {
	pat := os.Getenv("TWD_COLDID_AGG")
	if pat == "" {
		t.Skip("TWD_COLDID_AGG not set")
	}
	files, _ := filepath.Glob(pat)
	var all, early, ready []sample
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var r struct {
			Env     string   `json:"env"`
			Samples []sample `json:"samples"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(f, err)
		}
		all = append(all, r.Samples...)
		if strings.HasSuffix(r.Env, "-dialready") {
			ready = append(ready, r.Samples...)
		} else {
			early = append(early, r.Samples...)
		}
	}
	cell := func(ss []sample, st stage) string {
		var v []float64
		for _, s := range ss {
			if d, ok := st.delta(s); ok {
				v = append(v, d)
			}
		}
		if len(v) == 0 {
			return "– | –"
		}
		return fmt.Sprintf("%.0f | %.0f", pctl(v, .5), pctl(v, .95))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "files %d, samples %d (dial on netmap %d, dial on ready %d)\n", len(files), len(all), len(early), len(ready))
	fmt.Fprintf(&b, "| stage | p50 | p95 | p50 (dial on netmap) | p95 | p50 (dial on ready) | p95 |\n|---|---|---|---|---|---|---|\n")
	for _, st := range stages {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", st.name, cell(all, st), cell(early, st), cell(ready, st))
	}
	var slowKey, slowReg, slowMap, penalty, penN int
	for _, s := range all {
		if s.CreateKeyMS > 1000 {
			slowKey++
		}
		if s.Registered-s.RegisterSent > 2000 {
			slowReg++
		}
		if s.SelfNetmap-s.Registered > 1500 {
			slowMap++
		}
		if s.FirstOK > 0 && s.Ready > 0 {
			penN++
			if s.FirstOK-s.Ready > 3000 {
				penalty++
			}
		}
	}
	fmt.Fprintf(&b, "outliers: CreateKey >1 s %d/%d; register >2 s %d/%d; registered→netmap >1.5 s %d/%d; first 200 >3 s after ready %d/%d\n",
		slowKey, len(all), slowReg, len(all), slowMap, len(all), penalty, penN)
	t.Log("\n" + b.String())
}
