package hostd

import (
	"github.com/rosskukulinski/tailworks/hostd/mgmtapi"
	"github.com/rosskukulinski/tailworks/netd"
)

// Worker internet egress (ADR-049): a Worker's (or Node
// app's, or Durable Object's) fetch() to a destination that is not on the
// tailnet leaves from this host's own network through netd's internet
// path, after the SSRF guard, never as the object's tailnet node. The
// workload's tailnet.egress.internet (bundle.InternetEgress) can turn it
// off or limit it to an allowlist; Config.WorkerInternet nil turns it off
// for the whole host.

// internetConfig is the tenant registry's internet path, or nil when this
// host has none.
func (h *Host) internetConfig() *netd.InternetConfig {
	wi := h.cfg.WorkerInternet
	if wi == nil {
		return nil
	}
	return &netd.InternetConfig{
		Dial:      wi.Dial,
		Resolve:   wi.Resolve,
		HostAddrs: wi.HostAddrs,
		Policy:    h.internetPolicy,
		OnFlow:    netd.FirstFlowOnly(h.auditInternetFlow),
	}
}

// internetPolicy is the active deployment's tailnet.egress.internet for
// the workload that netd object id belongs to. An object hostd doesn't
// know is refused (fail closed).
func (h *Host) internetPolicy(netdID string) netd.InternetPolicy {
	h.mu.Lock()
	o := h.byNetd[netdID]
	var w *workload
	if o != nil {
		w = h.workloads[o.TenantID+"/"+o.Workload]
	}
	h.mu.Unlock()
	if w == nil {
		return netd.InternetPolicy{Off: true}
	}
	w.mu.Lock()
	app := w.app
	w.mu.Unlock()
	if app == nil {
		return netd.InternetPolicy{Off: true}
	}
	return netd.InternetPolicy{Off: app.Internet.WorkerOff(), Allow: app.Internet.WorkerAllow()}
}

// auditInternetFlow records the first internet flow per object and
// destination (kind egress; refusals are audited as deny by serveEgress).
func (h *Host) auditInternetFlow(f netd.InternetFlow) {
	h.mu.Lock()
	o := h.byNetd[f.Object]
	h.mu.Unlock()
	id := f.Object
	if o != nil {
		id = o.ID
	}
	h.log.Info("internet egress", "object", id, "dst", f.Host, "addr", f.Addr.String())
	h.audit.add(mgmtapi.AuditRecord{ObjectID: id, Kind: "egress", Dst: f.Host, Detail: mgmtapi.Note("internet via this host's network (" + f.Addr.String() + ")"), TraceID: newTraceID()})
}
