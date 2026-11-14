//go:build e2ereal

package coldid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/rosskukulinski/twd/internal/tsapi"
	"github.com/rosskukulinski/twd/netd"
)

const prodControl = "https://controlplane.tailscale.com"

// Only test tailnet 1. Never Ross's personal tailnet.
var allowedTailnets = map[string]bool{"tail7906d5.ts.net": true}

// TestColdIdentityReal runs the cold-identity breakdown on tailnet 1 with
// production control. Nodes are ttest-fi-<run>-*, ephemeral, tag:ttest; no
// policy is read or written; cleanup deletes exactly the devices and keys
// this run created (by ID).
func TestColdIdentityReal(t *testing.T) {
	keyFile, tailnet := os.Getenv("TWD_E2E_API_KEY_FILE"), os.Getenv("TWD_E2E_TAILNET")
	if keyFile == "" || tailnet == "" {
		t.Skip("TWD_E2E_API_KEY_FILE / TWD_E2E_TAILNET not set")
	}
	if !allowedTailnets[tailnet] {
		t.Fatalf("refusing to run against tailnet %q (not in the allow list)", tailnet)
	}
	apiKey, err := tsapi.LoadEnvFile(keyFile, "TS_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	api := &tsapi.Client{Tailnet: tailnet, APIKey: apiKey}
	b := make([]byte, 3)
	rand.Read(b)
	run := hex.EncodeToString(b)
	prefix := "ttest-fi-" + run
	tracker := tsapi.NewTracker(api)
	minter := tsapi.MintSource(api, slog.New(netd.NewRedactingHandler(slog.NewTextHandler(io.Discard, nil))))
	n, _ := strconv.Atoi(os.Getenv("TWD_COLDID_N"))
	if n <= 0 {
		n = 20
	}
	e := env{name: "real", controlURL: prodControl, tailnet: tailnet, apiKeyFile: keyFile, tags: []string{"tag:ttest"},
		prefix: prefix, n: n, gap: 2 * time.Second, track: tracker.Track, dial: os.Getenv("TWD_COLDID_DIAL"),
		callerKey: func(ctx context.Context) (string, error) {
			return minter.AuthKey(ctx, netd.KeyRequest{ObjectID: "caller", Hostname: prefix + "-caller", Tags: []string{"tag:ttest"}, Ephemeral: true})
		}}
	if e.dial == "ready" {
		e.name += "-dialready" // the Phase 1 name
	} else if e.dial != "" && e.dial != "netmap" {
		e.name += "-dial" + e.dial
	}
	if os.Getenv("TWD_COLDID_WARM") != "" {
		e.name += "-warm"
	}
	if os.Getenv("TWD_COLDID_PREJOINED") != "" {
		e.prejoined = true
		e.name += "-prejoined"
	}
	var keyIDs []string
	e.onMint = func(id string) { keyIDs = append(keyIDs, id) }
	// Registered first, so it runs last: after the registry and caller close.
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		rep := tracker.Cleanup(cctx, prefix)
		for _, id := range keyIDs {
			api.DeleteKey(cctx, id) // consumed keys 404, which is fine
		}
		minter.RevokeAll(cctx)
		t.Logf("cleanup: tracked %d, deleted %d, left %d, untracked %v, keys %d, errors %v", rep.Tracked, rep.Deleted, rep.Left, rep.Untracked, len(keyIDs), rep.Errors)
		if err := rep.Err(); err != nil {
			t.Error(err)
		}
	})
	ss := runColdIdentity(t, e)
	if len(ss) < e.n*8/10 {
		t.Errorf("only %d/%d samples", len(ss), e.n)
	}
	if len(ss) > 0 {
		report(t, e, ss)
	}
}
