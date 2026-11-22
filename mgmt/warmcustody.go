package mgmt

import (
	"context"
	"time"
)

// Warm keys in custody (issue #158, ledger H25). A pooled key is a
// preauthorized join key: anyone who reads mgmt's custody can join a node
// with it until it expires (WarmKeyTTL). So the count is visible (GET
// /v1/tenants/{id} `warmKeys`, shown by `tw status`), and a mgmt that stops
// with Config.RevokeWarmKeysOnClose (what `tw up` runs, so `tw down`)
// revokes them at the API and deletes their custody entries, instead of
// leaving them there until they expire. `tw tenant rm` revokes the tenant's
// keys before its custody is shredded (purgeTenant).

// warmKeysHeld counts tenantID's pooled keys whose custody entry mgmt may
// still hold: warm ones, expired ones whose revocation hasn't succeeded yet,
// and ones being adopted right now.
func (m *Mgmt) warmKeysHeld(ctx context.Context, tenantID string) int {
	ws, ok := m.store.(warmKeyStore)
	if !ok {
		return 0
	}
	ks, err := ws.ListWarmKeys(ctx, "")
	if err != nil {
		return 0
	}
	n := 0
	for _, k := range ks {
		if k.TenantID == tenantID {
			n++
		}
	}
	return n
}

// revokeWarmOnClose revokes every unused pooled key, and retries the
// revocations that failed before. Close calls it after the pool loop has
// stopped, so nothing refills behind it.
func (m *Mgmt) revokeWarmOnClose() {
	ws, ok := m.store.(warmKeyStore)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	n, err := m.drainWarm(ctx, "")
	if err != nil {
		m.log.Warn("shutdown: warm keys not revoked; they expire at the API", "err", err)
	}
	failed, _ := ws.ListWarmKeys(ctx, WarmKeyExpired)
	for _, k := range failed {
		if t, err := m.store.GetTenant(ctx, k.TenantID); err == nil {
			m.revokeWarm(ctx, t, k)
			n++
		}
	}
	if n > 0 {
		m.audit(ctx, AuditRecord{Kind: "revoke"}, map[string]any{"warmKeys": n, "reason": "mgmt stopped: warm keys revoked"})
		m.log.Info("shutdown: warm keys revoked", "n", n)
	}
}
