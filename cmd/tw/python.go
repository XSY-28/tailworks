package main

import (
	"slices"
	"strings"
)

// pythonWorker refuses a Python Worker (main: *.py, or the python_workers
// compatibility flag) by name, before a bundler reports a missing .py
// loader (COMPAT.md#python).
func pythonWorker(file string, w map[string]any) error {
	main := wranglerMain(w)
	flags, _ := w["compatibility_flags"].([]any)
	if !strings.HasSuffix(main, ".py") && !slices.Contains(flags, any("python_workers")) {
		return nil
	}
	return fail("not_supported", "Python Workers are not supported: "+file+" has main "+main+" (python_workers); tailworks bundles JavaScript and TypeScript only").
		withFix("run the Python code in a container (docs/containers.md)", "see docs/COMPAT.md#python")
}
