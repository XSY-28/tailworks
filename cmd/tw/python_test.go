package main

import "testing"

func TestPythonWorkerRefused(t *testing.T) {
	for _, c := range []struct {
		w    map[string]any
		want string
	}{
		{map[string]any{"main": "src/entry.py"}, "not_supported"},
		{map[string]any{"main": "src/index.ts", "compatibility_flags": []any{"python_workers"}}, "not_supported"},
		{map[string]any{"main": "src/index.ts", "compatibility_flags": []any{"nodejs_compat"}}, ""},
		{map[string]any{}, ""},
	} {
		if got := cliCode(pythonWorker("wrangler.jsonc", c.w)); got != c.want {
			t.Errorf("%v: code %q, want %q", c.w, got, c.want)
		}
	}
}
