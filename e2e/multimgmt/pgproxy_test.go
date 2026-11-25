//go:build e2emultimgmt

package multimgmt

import (
	"os"
	"testing"
	"time"

	"github.com/rosskukulinski/tailworks/e2e/internal/mgmtpg"
)

// pgProxy sits between one replica and Postgres and injects faults
// (mgmtpg.Proxy: pass, delay, blackhole, reset). e2e/dataplane's
// TW_DP_MGMT=pg3 cluster uses the same proxy.
type pgProxy struct{ p *mgmtpg.Proxy }

func newPGProxy(t *testing.T, target string) *pgProxy {
	return &pgProxy{mgmtpg.NewProxy(t, target)}
}

func (p *pgProxy) addr() string { return p.p.Addr() }

// set changes the mode; "reset" also drops every open connection.
func (p *pgProxy) set(mode string, d time.Duration) { p.p.Set(mode, d) }

// viaProxy rewrites a postgres:// DSN to go through addr.
func viaProxy(t *testing.T, dsn, addr string) string {
	out := mgmtpg.ViaProxy(t, dsn, addr)
	if extra := os.Getenv("TW_MM_DSN_SUFFIX"); extra != "" { // e.g. &tw_max_conns=2
		out += extra
	}
	return out
}
