package mgmt

import (
	"context"
	"database/sql"
	"errors"
)

// queryer is what *sql.DB and *sql.Tx share for reads.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// AuditChainSnapshot reads a chain's records and its head in one read
// transaction. AppendAudit writes the record and moves the head in one
// transaction; reading them in two separate queries (as VerifyChain once
// did) let an append commit in between, and an intact chain then failed
// verification with "chain head mismatch" (#93).
//
// Postgres: REPEATABLE READ, so both reads see the snapshot taken at the
// first one. SQLite: the store has a single connection, so no append can
// run while the transaction holds it.
func (s *SQLStore) AuditChainSnapshot(ctx context.Context, chain string) ([]AuditRecord, int64, string, error) {
	var opts *sql.TxOptions
	if s.d == dialectPostgres {
		opts = &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	}
	tx, err := s.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, 0, "", err
	}
	defer tx.Rollback()
	var seq int64
	var head string
	err = tx.QueryRowContext(ctx, s.q("SELECT seq, hash FROM audit_heads WHERE chain=?"), chain).Scan(&seq, &head)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, 0, "", err
	}
	recs, err := s.listAudit(ctx, tx, AuditFilter{Chain: chain})
	if err != nil {
		return nil, 0, "", err
	}
	return recs, seq, head, nil
}
