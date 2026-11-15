package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rosskukulinski/twd/hostd"
	"github.com/rosskukulinski/twd/hostd/bundle"
	"github.com/rosskukulinski/twd/internal/d1mig"
)

// D1 in twd dev (ADR-d1): dev stores live in the in-memory mgmt, so every
// `twd dev` starts with empty databases. After each (re)deploy, dev
// applies each d1_databases entry's pending migrations (wrangler's
// migrations_dir layout), through the dev host's store admin op, as
// `wrangler d1 migrations apply --local` would.

type devD1 struct {
	Binding  string   `json:"binding"`
	Database string   `json:"database"`
	Applied  []string `json:"applied"`
}

func devD1Migrate(ctx context.Context, h *hostd.Host, tenantID string, a *app) ([]devD1, error) {
	wb, err := os.ReadFile(filepath.Join(a.Dir, "wrangler.jsonc"))
	if err != nil {
		return nil, nil
	}
	dbs, err := bundle.D1Databases(wb)
	if err != nil {
		return nil, err
	}
	var out []devD1
	for _, db := range dbs {
		exec := func(ctx context.Context, qs []d1mig.Query) ([]d1mig.Result, error) {
			arg, _ := json.Marshal(map[string]any{"queries": qs})
			b, err := h.StoreAdmin(ctx, tenantID, a.Name, "d1", db.DatabaseName, "query", arg)
			if err != nil {
				return nil, err
			}
			var rs []d1mig.Result
			if json.Unmarshal(b, &rs) != nil {
				var one d1mig.Result
				json.Unmarshal(b, &one)
				return nil, fmt.Errorf("D1_ERROR: %s", one.Error)
			}
			return rs, nil
		}
		done, err := d1mig.Apply(ctx, exec, filepath.Join(a.Dir, db.MigrationsDir), db.MigrationsTable)
		if err != nil {
			return out, fmt.Errorf("d1 %s (%s): %w", db.Binding, db.DatabaseName, err)
		}
		out = append(out, devD1{Binding: db.Binding, Database: db.DatabaseName, Applied: done})
	}
	return out, nil
}
