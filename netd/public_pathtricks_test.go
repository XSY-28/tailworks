package netd_test

import (
	"path"
	"strings"
	"testing"

	"github.com/rosskukulinski/tailworks/netd"
)

// TestPublicPathTricksNeverReachPlatformPaths pins ledger claim C13 ("a
// public request never reaches /_tw or a DO directly") against encoding
// and normalisation tricks, under a catch-all route. Each trick is refused
// or, if it reaches the app, its path does not name a platform path once
// decoded and cleaned (what reaches the app goes to the workload's own
// default export, never the platform's /_tw addressing).
// hostd.TestPublicIngress §2 checks the plain cases through a real hostd
// and workerd.
func TestPublicPathTricksNeverReachPlatformPaths(t *testing.T) {
	e := newPubEnv(t, netd.PublicOptions{Routes: []string{"/*"}, Limits: limits(nil), Enabled: true}, nil)
	c := e.client(defaultSrc())
	tricks := []string{
		"/_tw",
		"/_tw/",
		"/_tw/o/Room/lobby",
		"/%5Ftw/o/Room/lobby", // encoded "_"
		"/%5ftw",
		"/_t%77/o/Room/lobby", // encoded "w"
		"/%5F%74%77/o/Room/lobby",
		"/_tw%2Fo%2FRoom%2Flobby", // encoded slashes
		"/x/../_tw/o/Room/lobby",
		"/x/%2e%2e/_tw/o/Room/lobby",
		"/x/%2E%2E%2F_tw",
		"/./_tw",
		"/%2e/_tw",
		"//_tw/o/Room/lobby",
		"/_tw;/o/Room/lobby",
		"/_tw%00/o",
		"/%255Ftw/o/Room/lobby", // double-encoded: once-decoded it is "/%5Ftw", which is the app's to refuse (H21)
	}
	for _, p := range tricks {
		resp, _ := do(t, c, "GET", e.url(p), nil, nil)
		t.Logf("%-32s %d %s", p, resp.StatusCode, resp.Header.Get("tw-error"))
	}
	// netd hands the app the once-decoded path; the platform wrapper
	// matches "/_tw" and "/_tw/..." on it. A request that reached the app
	// must not be one of those.
	e.upMu.Lock()
	defer e.upMu.Unlock()
	for _, r := range e.seen {
		if clean := path.Clean("/" + r.URL.Path); clean == "/_tw" || strings.HasPrefix(clean, "/_tw/") {
			t.Errorf("a public request reached the app at a platform path: %q (raw %q)", r.URL.Path, r.URL.EscapedPath())
		}
	}
}
