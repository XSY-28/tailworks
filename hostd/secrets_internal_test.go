package hostd

import (
	"strings"
	"testing"

	"github.com/rosskukulinski/twd/hostd/bundle"
)

func TestApplySecrets(t *testing.T) {
	app := &bundle.App{Name: "w"}
	if got, err := applySecrets(app, bundle.TierProcess, "w", nil); err != nil || got != app {
		t.Fatalf("no secrets: %v %v", got, err)
	}
	vals := map[string]string{"API_KEY": "v1"}
	got, err := applySecrets(app, bundle.TierProcess, "w", vals)
	if err != nil || got == app || got.Secrets["API_KEY"] != "v1" || app.Secrets != nil {
		t.Fatalf("process tier: %+v %v (the shared app must stay untouched)", got, err)
	}
	// microVM: refused with a typed error naming the secrets, never the values.
	_, err = applySecrets(app, bundle.TierMicroVM, "w", vals)
	if ae := asAPIError(err); ae == nil || ae.Code != "secrets_unsupported_tier" || !strings.Contains(ae.Detail, "API_KEY") || strings.Contains(ae.Detail, "v1") {
		t.Fatalf("microvm: %v", err)
	}
	node := &bundle.App{Runtime: bundle.RuntimeNode, Node: &bundle.NodeApp{Env: map[string]string{"A": "env", "B": "env"}}}
	if _, err := applySecrets(node, bundle.TierProcess, "w", map[string]string{"PORT": "1"}); asAPIError(err).Code != "bad_secret_name" {
		t.Fatalf("node reserved: %v", err)
	}
	if _, err := applySecrets(app, bundle.TierProcess, "w", map[string]string{"TWD_X": "1"}); asAPIError(err).Code != "bad_secret_name" {
		t.Fatalf("TWD_ prefix: %v", err)
	}
	ns, err := applySecrets(node, bundle.TierProcess, "w", map[string]string{"B": "secret", "C": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	env := nodeEnv(ns)
	if env["A"] != "env" || env["B"] != "secret" || env["C"] != "secret" || node.Node.Env["B"] != "env" {
		t.Fatalf("node env %v (node.env %v)", env, node.Node.Env)
	}
}
