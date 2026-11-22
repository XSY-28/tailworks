package tsnetlc

import (
	"time"

	"tailscale.com/tsnet"
)

// SetDialDelay slows every LocalAPI dial (set it before the nodes start).
func SetDialDelay(d time.Duration) { dialDelay = d }

// CloseIdle closes ts's pooled LocalAPI connections, as Close does first.
func CloseIdle(ts *tsnet.Server) {
	if v, ok := transports.Load(ts); ok {
		v.(interface{ CloseIdleConnections() }).CloseIdleConnections()
	}
}

// Forget drops ts's transport (after a test closed ts by hand).
func Forget(ts *tsnet.Server) { transports.Delete(ts) }
