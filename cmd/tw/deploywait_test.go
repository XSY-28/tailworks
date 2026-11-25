package main

import (
	"net/http"
	"slices"
	"testing"

	"github.com/rosskukulinski/tailworks/mgmt"
)

func TestPlacementCommandsReportCrashloop(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		args []string
		run  func([]string) error
	}{
		{"promote", "/v1/tenants/tn_1/workloads/app/promotions", []string{"app", "preview"}, cmdPromote},
		{"rollback", "/v1/tenants/tn_1/workloads/app/rollbacks", []string{"app"}, cmdRollback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fakeLinkedMgmt(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == tc.path:
					if r.URL.Query().Get("dryRun") == "true" {
						writeJSON(w, mgmt.PreflightResult{OK: true})
						return
					}
					writeJSON(w, map[string]any{"deployment": mgmt.DeploymentView{DeploymentID: "dep_1"}})
				case r.Method == http.MethodGet && r.URL.Path == "/v1/deployments/dep_1":
					writeJSON(w, mgmt.DeploymentView{DeploymentID: "dep_1", Status: "crashloop", Error: "attempt 3\nworkerd exited"})
				default:
					t.Errorf("unexpected management API request: %s %s", r.Method, r.URL)
					http.NotFound(w, r)
				}
			})
			// A crashloop response takes precedence even when the wait budget is exhausted.
			err := tc.run(append([]string{"--state-dir", dir, "--json", "--wait=0s"}, tc.args...))
			if cliCode(err) != "deploy_crashloop" {
				t.Fatalf("tw %s returned %v, want deploy_crashloop", tc.name, err)
			}
			got := asCLIErr(err)
			if !slices.Equal(got.Lines, []string{"attempt 3", "workerd exited"}) || !slices.Equal(got.Fix, []string{"tw logs app"}) {
				t.Fatalf("tw %s error detail = %q, fix = %q", tc.name, got.Lines, got.Fix)
			}
		})
	}
}
