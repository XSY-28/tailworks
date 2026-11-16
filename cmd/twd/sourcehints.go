package main

import (
	"os"
	"path/filepath"
	"strings"
)

// Hints for running twd from a source checkout (issue #46): there the fix
// for missing JS packages is `make deps`, not "reinstall twd". A plain
// `make build` builds the Go binaries but doesn't vendor or build the JS
// packages, so bundling an app fails with `Could not resolve "@twd/…"`.

// twdModule is the first line of a twd checkout's go.mod.
const twdModule = "module github.com/rosskukulinski/twd"

// sourceCheckout is the root of the twd checkout this twd runs from (above
// the executable or the working directory), or "".
func sourceCheckout() string {
	for _, d := range repoRoots() { // state.go
		if !isFile(filepath.Join(d, "Makefile")) || !dirExists(filepath.Join(d, "js", "packages", "create-twd")) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil && strings.HasPrefix(string(b), twdModule+"\n") {
			return d
		}
	}
	return ""
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// reinstallFix is the fix for missing or incomplete bundled files: `make
// deps` in a source checkout, else fallback (the installed release's fix).
func reinstallFix(fallback string) string {
	if root := sourceCheckout(); root != "" {
		return "cd " + root + " && make deps   (a source checkout: make build doesn't install or build the JS packages)"
	}
	return fallback
}

// noNodeModules is the doctor detail suffix when the bundler found no
// node_modules above create-twd (nodePaths returned none).
func noNodeModules(np []string, createTwd string) string {
	if len(np) > 0 {
		return ""
	}
	return " (no node_modules found above " + createTwd + ")"
}

// unresolvedTwdImport reports whether bundler output says an @twd/*
// package (twd's own SDK) could not be resolved.
func unresolvedTwdImport(out string) bool {
	return strings.Contains(out, `Could not resolve "@twd/`)
}

// withBundleFix adds the fix for an unresolved @twd/* import to a
// build_failed error: twd's JS packages are missing next to the bundler.
func withBundleFix(e *cliErr, out string) *cliErr {
	if !unresolvedTwdImport(out) {
		return e
	}
	return e.withFix(reinstallFix("twd's JS packages (@twd/tailnet, @twd/mcp, …) are missing: reinstall twd (install.sh), then `twd doctor`"))
}
