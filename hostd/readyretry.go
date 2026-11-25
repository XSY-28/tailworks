package hostd

import (
	"context"
	"time"

	"github.com/rosskukulinski/tailworks/hostd/mgmtapi"
)

// retryReady re-sends an object's ready report after mgmt refused it with
// lease_lost, after a backoff (1, 2, 4, 8, 16 s; 5 tries), refreshing the
// lease epoch from mgmt's assignments before each try (issue #210).
//
// The case that needs it is retryNode's race one step later. A fresh hostd
// is bringing up the previous deployment's node when a `tw deploy` bumps
// the lease epoch. The join itself succeeded, but the ready report still
// carried the old epoch and mgmt refused it. The node is ready on this host
// while mgmt's row stays "minted". The new deployment adopts the ready node
// and never reports it again, so `tw deploy` ends in identity_pending.
//
// It stops when the report is accepted, when the object is no longer the
// ready node it was (killed, or restarted: gen moved), when the workload
// is no longer active here, or on any error other than lease_lost.
func (h *Host) retryReady(o *object, req mgmtapi.ReadyRequest, gen uint64) {
	const maxTries = 5
	w := h.workloadOf(o)
	if w == nil {
		return
	}
	for try := 1; try <= maxTries; try++ {
		select {
		case <-h.ctx.Done():
			return
		case <-time.After(time.Second << (try - 1)):
		}
		if !w.isActive() {
			return
		}
		h.refreshEpoch(w)
		h.mu.Lock()
		current := o.Status == statusReady && o.nodeGen == gen
		h.mu.Unlock()
		if !current {
			return
		}
		req.Epoch = w.epochNow()
		ctx, cancel := context.WithTimeout(h.ctx, 15*time.Second)
		err := h.mgmt.Ready(ctx, o.ID, req)
		cancel()
		h.noteReport(o, req.StableNodeID, err) // egressgate.go (#331)
		if err == nil {
			h.log.Info("mgmt ready retried", "object", o.ID, "attempt", try, "epoch", req.Epoch)
			return
		}
		e, ok := mgmtapi.AsError(err)
		if ok && e.Code == mgmtapi.CodeKilled {
			h.killLocal(o.ID, "killed during join")
			return
		}
		if !ok || e.Code != mgmtapi.CodeLeaseLost {
			h.log.Warn("mgmt ready retry failed", "object", o.ID, "attempt", try, "err", err)
			return
		}
	}
	h.log.Warn("mgmt ready still refused with lease_lost; giving up", "object", o.ID, "tries", maxTries)
}

// refreshEpoch raises w's lease epoch to the one in mgmt's assignments for
// this host (best effort: an unreachable mgmt leaves it as it was).
func (h *Host) refreshEpoch(w *workload) {
	as, err := h.mgmt.Assignments(h.ctx, h.cfg.HostID)
	if err != nil {
		return
	}
	for _, a := range as {
		if a.TenantID == w.tenantID && a.Workload == w.name {
			w.mu.Lock()
			if a.Epoch > w.epoch {
				w.epoch = a.Epoch
			}
			w.mu.Unlock()
		}
	}
}
