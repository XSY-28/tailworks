package hostd

// Worker secrets (ADR-worker-secrets), host side. When hostd starts a
// deployment it asks mgmt for the workload's secrets at the lease epoch of
// the assignment (mgmt releases them only to the lease holder). They go
// into the instance only:
//
//   - workerd (process tier): text bindings in config.capnp and the facets'
//     env in twd-config.json, both in the 0700 instance directory;
//   - runtime "node": environment variables of the node process.
//
// They are never logged and never written anywhere else. The microVM tier
// is refused for now: its config drive (the instance directory as a tar)
// is written to the VM's run directory on a cold boot, outside the
// instance directory.

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/rosskukulinski/twd/hostd/bundle"
)

// nodeReservedEnv are the node process variables twd sets itself; neither
// node.env nor a secret may replace them.
var nodeReservedEnv = map[string]bool{
	"PATH":               true,
	"HOME":               true,
	"TMPDIR":             true,
	"NODE_ENV":           true,
	"NODE_USE_ENV_PROXY": true,
	"HTTP_PROXY":         true,
	"HTTPS_PROXY":        true,
	"http_proxy":         true,
	"https_proxy":        true,
	"NO_PROXY":           true,
	"no_proxy":           true,
	"PORT":               true,
	"HOST":               true,
}

// withSecrets returns app with the workload's secrets (a copy; app is
// shared with the workload's state), or app itself when there are none.
func (h *Host) withSecrets(ctx context.Context, w *workload, app *bundle.App, tier string, epoch int64) (*bundle.App, error) {
	vals, err := h.mgmt.WorkloadSecrets(ctx, w.tenantID, w.name, epoch)
	if err != nil {
		return nil, fmt.Errorf("workload secrets: %w", err)
	}
	out, err := applySecrets(app, tier, w.name, vals)
	if err == nil && len(vals) > 0 {
		h.log.Info("workload secrets released", "workload", w.name, "epoch", epoch, "n", len(vals))
	}
	return out, err
}

// applySecrets checks vals against the tier and runtime and returns app
// with them (a copy), or app itself when there are none.
func applySecrets(app *bundle.App, tier, workload string, vals map[string]string) (*bundle.App, error) {
	if len(vals) == 0 {
		return app, nil
	}
	names := make([]string, 0, len(vals))
	for k := range vals {
		names = append(names, k)
	}
	sort.Strings(names)
	if tier == bundle.TierMicroVM {
		return nil, &apiError{Code: "secrets_unsupported_tier", Detail: fmt.Sprintf(
			"workload %q has %d secret(s) (%s), and the microvm tier does not support secrets yet. "+
				"Deploy it with \"tier\": \"process\" in twd.jsonc, or remove them (twd secret rm <NAME>); see docs/secrets.md",
			workload, len(names), strings.Join(names, ", "))}
	}
	for _, k := range names {
		if strings.HasPrefix(k, "TWD_") || strings.HasPrefix(k, "__TWD") {
			return nil, &apiError{Code: "bad_secret_name", Detail: fmt.Sprintf("secret %q uses a name twd reserves (TWD_, __TWD)", k)}
		}
		if app.Runtime == bundle.RuntimeNode && nodeReservedEnv[k] {
			return nil, &apiError{Code: "bad_secret_name", Detail: fmt.Sprintf(
				"secret %q would replace an environment variable twd sets for runtime \"node\"; rename it (twd secret rm %s, then twd secret put <NEW_NAME>)", k, k)}
		}
	}
	cp := *app
	cp.Secrets = vals
	return &cp, nil
}

// nodeEnv is a node app's node.env (and deploy vars) with its secrets on
// top: a secret overrides a variable of the same name.
func nodeEnv(app *bundle.App) map[string]string {
	if len(app.Secrets) == 0 {
		return app.Node.Env
	}
	env := maps.Clone(app.Node.Env)
	if env == nil {
		env = map[string]string{}
	}
	maps.Copy(env, app.Secrets)
	return env
}
