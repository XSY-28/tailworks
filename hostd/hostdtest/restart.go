package hostdtest

import (
	"context"

	"github.com/rosskukulinski/twd/hostd"
)

// Restart stops twd-hostd and starts a new one on the same data and run
// directories, as a host process restart does: workerd instances stop,
// then the new hostd reloads its state and redeploys its assignments.
func (e *Env) Restart() {
	e.T.Helper()
	e.Host.Close()
	h, err := hostd.New(e.cfg)
	if err != nil {
		e.T.Fatal(err)
	}
	if err := h.Start(context.Background()); err != nil {
		e.T.Fatal(err)
	}
	e.T.Cleanup(func() { h.Close() })
	e.Host = h
}
