package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rosskukulinski/twd/mgmt"
)

// Store maintenance subcommands (ADR-041 §3, §8):
//
//	twd-mgmt migrate --check  [-state-dir D | -db DSN | -db-file F] [-json]
//	twd-mgmt migrate --apply  …   apply pending migrations, contract ones included, then exit
//	twd-mgmt restore-fence    …   after restoring the store from a backup: lease epochs jump
//
// They work on the store directly and never serve; run them with every
// replica stopped (--apply on a contract migration) or at any time (--check).

func runMaintenance(args []string) (bool, error) {
	if len(args) == 0 || (args[0] != "migrate" && args[0] != "restore-fence") {
		return false, nil
	}
	home, _ := os.UserHomeDir()
	fs := flag.NewFlagSet("twd-mgmt "+args[0], flag.ContinueOnError)
	stateDir := fs.String("state-dir", filepath.Join(home, ".twd", "mgmt"), "state directory (the default SQLite store is <state-dir>/mgmt.db)")
	dsn := fs.String("db", "", "store: SQLite path or postgres://… DSN (default <state-dir>/mgmt.db)")
	dsnFile := fs.String("db-file", "", "read the store DSN from this file")
	check := fs.Bool("check", false, "migrate: report what opening the store would do; change nothing")
	apply := fs.Bool("apply", false, "migrate: apply every pending migration, contract migrations included (stop the other replicas first)")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return true, err
	}
	d := *dsn
	if *dsnFile != "" {
		b, err := os.ReadFile(*dsnFile)
		if err != nil {
			return true, err
		}
		d = strings.TrimSpace(string(b))
	}
	if d == "" {
		d = filepath.Join(*stateDir, "mgmt.db")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out := func(v any, human func()) {
		if *asJSON {
			json.NewEncoder(os.Stdout).Encode(v)
			return
		}
		human()
	}
	switch args[0] {
	case "migrate":
		if *check == *apply {
			return true, fmt.Errorf("migrate: pass exactly one of --check or --apply")
		}
		if *check {
			st, err := mgmt.CheckSchema(ctx, d)
			out(st, func() { printSchema(st) })
			return true, err
		}
		s, err := mgmt.OpenStore(ctx, d) // applies expand migrations
		if err != nil {
			return true, err
		}
		defer s.Close()
		applied, err := s.ApplyDeferredContract(ctx)
		if err != nil {
			return true, err
		}
		st, err := s.SchemaStatus(ctx)
		if err != nil {
			return true, err
		}
		out(map[string]any{"schema": st, "appliedContract": applied}, func() {
			printSchema(st)
			if len(applied) > 0 {
				fmt.Println("applied contract migrations:", strings.Join(applied, ", "))
			}
		})
		return true, nil
	default: // restore-fence: no migration (the restored store keeps its schema)
		gen, err := mgmt.RestoreFenceDSN(ctx, d)
		if err != nil {
			return true, err
		}
		out(map[string]any{"restoreGeneration": gen, "epochJump": mgmt.RestoreEpochJump}, func() {
			fmt.Printf("restore fence: every lease epoch moved up by %d (restore generation %d)\n", mgmt.RestoreEpochJump, gen)
		})
		return true, nil
	}
}

func printSchema(st mgmt.SchemaStatus) {
	fmt.Printf("schema: applied %s, this twd-mgmt knows up to %s\n", st.Applied, st.Latest)
	if len(st.Pending) > 0 {
		fmt.Println("pending:", strings.Join(st.Pending, ", "))
	}
	if len(st.PendingContract) > 0 {
		fmt.Println("pending contract (applied once every replica runs this version, or with migrate --apply):", strings.Join(st.PendingContract, ", "))
	}
	if len(st.Ahead) > 0 {
		fmt.Println("ahead (applied by a newer twd-mgmt):", strings.Join(st.Ahead, ", "))
	}
	if st.MinBinary != "" {
		fmt.Println("minimum twd-mgmt:", st.MinBinary)
	}
}
