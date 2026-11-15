package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rosskukulinski/twd/internal/devreap"
)

// devTmpGlob matches twd dev's private dirs (dev.go: os.MkdirTemp("/tmp",
// "twd-dev-")).
const devTmpGlob = "/tmp/twd-dev-*"

// guardDevDir makes twd dev clean up even when it is SIGKILLed (issue #44):
// first it reaps the dirs of earlier twd dev runs whose process is gone
// (their workerd and node children, then the dir), then it guards tmp, so
// a watchdog kills this run's children and removes tmp however twd dev
// exits. Another live twd dev (another worktree's) is left alone. Failure
// is only a warning: twd dev still runs, it just can't promise the
// cleanup.
func guardDevDir(tmp, workerd string) {
	for _, d := range devreap.ReapStale(devTmpGlob) {
		fmt.Fprintf(os.Stderr, "twd dev: cleaned up %s (left by a twd dev that was killed)\n", filepath.Base(d))
	}
	if err := devreap.Guard(tmp, devreap.Options{Programs: []string{workerd}}); err != nil {
		fmt.Fprintf(os.Stderr, "twd dev: warning: no cleanup watchdog (%v); if twd dev is killed with SIGKILL, workerd may keep running\n", err)
	}
}
