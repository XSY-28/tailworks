package mgmt

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestWarmKeysRevokedOnCloseInParallel (#245): `tw down` revokes the warm
// pool's keys in parallel. With 200 ms per key DELETE at the fake API, 16
// keys took 3.2 s one by one; the budget (1.6 s) leaves room for a slow
// runner while failing a sequential revoke.
func TestWarmKeysRevokedOnCloseInParallel(t *testing.T) {
	t.Parallel()
	e := newPoolEnv(t, 0, func(c *Config) { c.WarmKeys, c.RevokeWarmKeysOnClose = 0, true })
	ctx := context.Background()
	const n, delay = 16, 200 * time.Millisecond
	now := time.Now()
	var ids []string
	for i := range n {
		id := fmt.Sprintf("kpar%d", i)
		ids = append(ids, id)
		if err := e.store.PutWarmKey(ctx, WarmKey{KeyID: id, TenantID: e.tenant, Pool: "p", Tags: []string{"tag:tw-agent-support"},
			Status: WarmKeyWarm, CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(time.Hour).UnixMilli()}); err != nil {
			t.Fatal(err)
		}
	}
	e.ts.keyDeleteDelay.Store(int64(delay))
	st := time.Now()
	e.m.Close()
	took := time.Since(st)
	for _, id := range ids {
		if !e.ts.wasDeleted(id) {
			t.Errorf("warm key %s not revoked", id)
		}
	}
	if budget := n * delay / 2; took > budget {
		t.Fatalf("Close took %v revoking %d keys (%v each), budget %v: revoked one by one?", took, n, delay, budget)
	}
	t.Logf("Close revoked %d keys (%v per DELETE) in %v", n, delay, took.Round(time.Millisecond))
}
