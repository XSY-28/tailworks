package mgmt

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// Warm-key statuses. A key is taken exactly once (an atomic conditional
// UPDATE) and never returns to "warm".
const (
	WarmKeyWarm    = "warm"
	WarmKeyTaken   = "taken"
	WarmKeyExpired = "expired"
)

// WarmKey is a pre-minted, single-use, preauthorized key waiting in a
// tenant's pool (RFC §4: warm keys per tag set). Times are store-clock ms.
// ExpiresAt is the key's own expiry at the API minus a safety margin; the
// pool stops handing a key out WarmKeyTakeMargin before it.
type WarmKey struct {
	KeyID     string
	TenantID  string
	Pool      string // PoolName(tags, ephemeral)
	Tags      []string
	Ephemeral bool
	Status    string
	CreatedAt int64
	ExpiresAt int64
	TakenBy   string
	TakenAt   int64
}

// warmKeyStore is implemented by stores that can hold the warm-key pool.
type warmKeyStore interface {
	// PutWarmKey inserts a warm key (and the intents carried by ctx: the
	// refill's mint record commits with it).
	PutWarmKey(ctx context.Context, k WarmKey) error
	// TakeWarmKey marks one warm key of (tenant, pool) taken by objectID in
	// one conditional write, the one closest to expiry among those whose
	// ExpiresAt > minExpires. ok=false if none.
	TakeWarmKey(ctx context.Context, tenantID, pool, objectID string, now, minExpires int64) (k WarmKey, ok bool, err error)
	// ExpireWarmKeys marks every warm key with ExpiresAt ≤ before expired
	// (one conditional write) and returns them.
	ExpireWarmKeys(ctx context.Context, before int64) ([]WarmKey, error)
	// CountWarmKeys counts the warm keys of a pool with ExpiresAt > minExpires.
	CountWarmKeys(ctx context.Context, tenantID, pool string, minExpires int64) (int, error)
	// ListWarmKeys lists keys by status ("" = all).
	ListWarmKeys(ctx context.Context, status string) ([]WarmKey, error)
	DeleteWarmKey(ctx context.Context, tenantID, keyID string) error
	// PoolDemand counts the tenant's objects created at or after since, by
	// tag set (as stored) and ephemeral flag.
	PoolDemand(ctx context.Context, tenantID string, since int64) ([]PoolCount, error)
	// ObjectsWithKey lists the IDs of the tenant's objects whose key_id is keyID.
	ObjectsWithKey(ctx context.Context, tenantID, keyID string) ([]string, error)
}

// PoolCount is one PoolDemand row.
type PoolCount struct {
	Tags      []string
	Ephemeral bool
	N         int
}

var _ warmKeyStore = (*SQLStore)(nil)

const warmCols = "key_id, tenant_id, pool, tags, ephemeral, status, created_at, expires_at, taken_by, taken_at"

func scanWarm(sc interface{ Scan(...any) error }) (WarmKey, error) {
	var k WarmKey
	var tags string
	var eph int64
	err := sc.Scan(&k.KeyID, &k.TenantID, &k.Pool, &tags, &eph, &k.Status, &k.CreatedAt, &k.ExpiresAt, &k.TakenBy, &k.TakenAt)
	json.Unmarshal([]byte(tags), &k.Tags)
	k.Ephemeral = eph != 0
	return k, err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *SQLStore) PutWarmKey(ctx context.Context, k WarmKey) error {
	if k.Status == "" {
		k.Status = WarmKeyWarm
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, s.q("INSERT INTO warm_keys ("+warmCols+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"),
			k.KeyID, k.TenantID, k.Pool, jsonStr(k.Tags), b2i(k.Ephemeral), k.Status, k.CreatedAt, k.ExpiresAt, k.TakenBy, k.TakenAt)
		return err
	})
}

func (s *SQLStore) TakeWarmKey(ctx context.Context, tenantID, pool, objectID string, now, minExpires int64) (WarmKey, bool, error) {
	skip := ""
	if s.d == dialectPostgres {
		skip = " FOR UPDATE SKIP LOCKED" // concurrent takers pick different rows instead of queueing on one
	}
	k, err := scanWarm(s.db.QueryRowContext(ctx, s.q(`UPDATE warm_keys SET status=?, taken_by=?, taken_at=?
		WHERE tenant_id=? AND key_id = (SELECT key_id FROM warm_keys WHERE tenant_id=? AND pool=? AND status=? AND expires_at>?
		                ORDER BY expires_at LIMIT 1`+skip+`)
		  AND status=?
		RETURNING `+warmCols), WarmKeyTaken, objectID, now, tenantID, tenantID, pool, WarmKeyWarm, minExpires, WarmKeyWarm))
	if errors.Is(err, sql.ErrNoRows) {
		return WarmKey{}, false, nil
	}
	return k, err == nil, err
}

func (s *SQLStore) ExpireWarmKeys(ctx context.Context, before int64) ([]WarmKey, error) {
	rows, err := s.db.QueryContext(ctx, s.q("UPDATE warm_keys SET status=? WHERE status=? AND expires_at<=? RETURNING "+warmCols),
		WarmKeyExpired, WarmKeyWarm, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WarmKey
	for rows.Next() {
		k, err := scanWarm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *SQLStore) CountWarmKeys(ctx context.Context, tenantID, pool string, minExpires int64) (int, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, s.q("SELECT COUNT(*) FROM warm_keys WHERE tenant_id=? AND pool=? AND status=? AND expires_at>?"),
		tenantID, pool, WarmKeyWarm, minExpires).Scan(&n)
	return int(n), err
}

func (s *SQLStore) ListWarmKeys(ctx context.Context, status string) ([]WarmKey, error) {
	q, args := "SELECT "+warmCols+" FROM warm_keys", []any{}
	if status != "" {
		q, args = q+" WHERE status=?", append(args, status)
	}
	rows, err := s.db.QueryContext(ctx, s.q(q+" ORDER BY created_at"), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WarmKey
	for rows.Next() {
		k, err := scanWarm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *SQLStore) DeleteWarmKey(ctx context.Context, tenantID, keyID string) error {
	_, err := s.db.ExecContext(ctx, s.q("DELETE FROM warm_keys WHERE tenant_id=? AND key_id=?"), tenantID, keyID)
	return err
}

func (s *SQLStore) PoolDemand(ctx context.Context, tenantID string, since int64) ([]PoolCount, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT tags, ephemeral, COUNT(*) FROM objects
		WHERE tenant_id=? AND created_at>=? GROUP BY tags, ephemeral`), tenantID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PoolCount
	for rows.Next() {
		var tags string
		var eph, n int64
		if err := rows.Scan(&tags, &eph, &n); err != nil {
			return nil, err
		}
		var c PoolCount
		json.Unmarshal([]byte(tags), &c.Tags)
		c.Ephemeral, c.N = eph != 0, int(n)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLStore) ObjectsWithKey(ctx context.Context, tenantID, keyID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, s.q("SELECT id FROM objects WHERE tenant_id=? AND key_id=?"), tenantID, keyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PoolName is the canonical pool of a tag set: sorted, deduplicated tags
// plus the ephemeral flag. A pooled key is only ever handed to an object
// whose PoolName is identical, so it carries exactly the object's tags.
func PoolName(tags []string, ephemeral bool) string {
	return strings.Join(canonTags(tags), ",") + map[bool]string{true: "|ephemeral", false: "|persistent"}[ephemeral]
}

func canonTags(tags []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}
