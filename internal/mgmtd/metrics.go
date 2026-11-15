package mgmtd

import (
	"net/http"

	"github.com/rosskukulinski/twd/internal/obs"
	"github.com/rosskukulinski/twd/mgmt"
)

// MetricsHandler is GET /metrics: mgmt's registry and every registry added
// with AddMetrics (ADR-observability).
func (rt *Runtime) MetricsHandler() http.Handler {
	return mgmt.MetricsHandler(func() obs.Gatherers {
		rt.metricsMu.Lock()
		defer rt.metricsMu.Unlock()
		return append(obs.Gatherers(nil), rt.extraMetrics...)
	})
}

// AddMetrics serves r with mgmt's metrics (the embedded host's registry in
// single-process twd up, whose host has no node of its own).
func (rt *Runtime) AddMetrics(r *obs.Registry) {
	rt.metricsMu.Lock()
	defer rt.metricsMu.Unlock()
	rt.extraMetrics = append(rt.extraMetrics, r)
}
