// Package features names the host-side capabilities a deployment can need
// (ADR-upgrades-and-releases §7). hostd advertises Host() when it
// registers; mgmt places a deployment only on hosts that advertise every
// feature Required() finds in its bundle, so a feature that needs new
// hostd code can merge and roll out host by host.
//
// A feature string is added here when its hostd code lands, one per line
// in sorted position (AGENTS.md rule 3). Features that predate version
// reporting (twd 0.1.x: workerd, DOs, cron, public ingress, containers,
// secrets) are not listed: every host has them, and tiers and the
// "containers" capability already gate placement for those.
package features

import (
	"slices"

	"github.com/rosskukulinski/twd/hostd/bundle"
)

// Feature strings. A platform store kind (hostd/bundle RegisterStoreKind)
// is its own feature: "d1", "kv", "r2", …
const (
	// Telemetry is OTLP export, request records and /metrics (ADR observability).
	Telemetry = "telemetry"
)

// Host is what this build's hostd offers: every registered platform store
// kind plus the static features above, sorted.
func Host() []string {
	out := []string{
		Telemetry,
	}
	out = append(out, bundle.StoreKindNames()...)
	slices.Sort(out)
	return slices.Compact(out)
}

// Required is what a parsed app needs from its host, sorted.
func Required(app *bundle.App) []string {
	var out []string
	for _, s := range app.Stores {
		out = append(out, s.Kind)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// RequiredByBundle unpacks and parses a deploy bundle and returns
// Required. A bundle that doesn't parse needs nothing extra (deploy's own
// validation reports it).
func RequiredByBundle(b []byte) []string {
	files, err := bundle.Unpack(b)
	if err != nil {
		return nil
	}
	app, err := bundle.Parse(files)
	if err != nil || app == nil {
		return nil
	}
	return Required(app)
}

// Missing returns the features in need that have does not list.
func Missing(need, have []string) []string {
	var out []string
	for _, f := range need {
		if !slices.Contains(have, f) {
			out = append(out, f)
		}
	}
	return out
}
