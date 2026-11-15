package hostd

import (
	"net/http"
	"time"
)

// Stopping pool refills for teardown. `twd down --purge` lists every object
// and kills it; killing a pool node used to let maintainPools top the pool
// up again before hostd stopped, and that new node, missing from the list,
// stayed on the tailnet as a device (e2e-multi, e2e-linux-vm: an untracked
// "<prefix>-pool-…" left after down, on master too).

// poolCreateWait bounds the wait for in-flight pool creates (each has a
// 30 s context).
const poolCreateWait = 35 * time.Second

// StopPools makes maintainPools a no-op from now on and waits (up to
// poolCreateWait) until no pool node is being created, so a list of objects
// taken afterwards includes every pool node. It reports whether creates
// were still in flight when it gave up.
func (h *Host) StopPools() (stillCreating int) {
	h.mu.Lock()
	h.poolsStopped = true
	h.mu.Unlock()
	deadline := time.Now().Add(poolCreateWait)
	for {
		h.mu.Lock()
		n := len(h.poolBusy)
		h.mu.Unlock()
		if n == 0 || time.Now().After(deadline) {
			return n
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// poolsStopHandler is POST /v1/pools/stop (push API; the local socket for
// twd down).
func (h *Host) poolsStopHandler(w http.ResponseWriter, r *http.Request) {
	n := h.StopPools()
	writeJSON(w, 200, map[string]any{"ok": n == 0, "creating": n})
}
