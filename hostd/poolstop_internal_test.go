package hostd

import (
	"context"
	"testing"
	"time"
)

// After StopPools, maintainPools starts no pool node, and StopPools itself
// returns only once in-flight pool creates are done: `twd down --purge`
// lists objects after it, so every pool node is in the list it kills.
func TestStopPools(t *testing.T) {
	h := &Host{
		cfg:        Config{PoolNodes: 1},
		ctx:        context.Background(),
		poolDemand: map[string]time.Time{"tn/w/SupportAgent": time.Now()},
		poolBusy:   map[string]bool{"tn/w/Other": true}, // a create in flight
		workloads:  map[string]*workload{},
		objects:    map[string]*object{},
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		h.mu.Lock()
		delete(h.poolBusy, "tn/w/Other") // the create finished
		h.mu.Unlock()
	}()
	t0 := time.Now()
	if n := h.StopPools(); n != 0 {
		t.Fatalf("StopPools: %d still creating", n)
	}
	if d := time.Since(t0); d < 150*time.Millisecond {
		t.Fatalf("StopPools returned after %v, before the in-flight create finished", d)
	}
	h.maintainPools()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.poolBusy) != 0 {
		t.Fatalf("maintainPools started a pool create after StopPools: %v", h.poolBusy)
	}
	if _, ok := h.poolDemand["tn/w/SupportAgent"]; !ok {
		t.Fatal("maintainPools ran its demand loop after StopPools")
	}
}
