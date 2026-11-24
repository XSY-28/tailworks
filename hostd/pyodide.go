package hostd

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/rosskukulinski/tailworks/hostd/bundle"
	"github.com/rosskukulinski/tailworks/internal/pyodide"
)

// Python Workers (ADR-055, docs/python.md): workerd reads the
// pinned Pyodide bundles from a host directory, read-only, and never
// downloads anything itself (its sandbox has no network).

// pyodideDir is the host's Pyodide bundle directory.
func (h *Host) pyodideDir() string {
	if h.cfg.PyodideDir != "" {
		return h.cfg.PyodideDir
	}
	return filepath.Join(h.cfg.DataDir, "pyodide")
}

// pyodideFor makes sure a Python app's Pyodide bundles are on this host
// (fetching the missing pins once) and returns their directory; "" for
// any other app.
func (h *Host) pyodideFor(app *bundle.App) (string, error) {
	if !app.Python {
		return "", nil
	}
	dir := h.pyodideDir()
	if len(pyodide.Missing(dir)) > 0 {
		h.log.Info("fetching the pinned Pyodide bundles for Python Workers (once per host)", "dir", dir)
	}
	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Minute)
	defer cancel()
	if err := pyodide.Ensure(ctx, dir, h.cfg.PyodideURL); err != nil {
		return "", &apiError{Code: "python_unavailable", Detail: err.Error()}
	}
	return dir, nil
}

// errPythonMicroVM refuses a Python Worker that resolves to the microVM
// tier: the guest image has no Pyodide bundles yet. It is not run in the
// process tier instead (ADR-032 D5b).
func errPythonMicroVM(app string) *apiError {
	return &apiError{Code: "tier_unavailable", Detail: fmt.Sprintf(
		"%q is a Python Worker, and Python Workers run on the process tier only for now (docs/python.md#limits); it is not run in the process tier instead. "+
			"Set \"tier\": \"process\" in tailworks.jsonc", app)}
}

// pyodideArgs are workerd's flags for the instance's Pyodide directory.
func (inst *instance) pyodideArgs() []string {
	if inst.pyodide == "" {
		return nil
	}
	return pyodide.WorkerdArgs(inst.pyodide)
}
