package netd

import (
	"context"
	"strings"
	"sync"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/ipn"
)

// JoinSpans breaks one fresh join (no saved state) into stages. Each field is
// the offset in ms from Create (the object was queued); 0 means the stage was
// not seen. Rejoins from saved state record only Slot, Start, Running and
// Listening.
//
// Control-side stages come from two sources: the IPN bus (state transitions,
// the node's first SelfChange) and controlclient's own log lines
// (doLogin / RegisterReq / its response), matched as substrings. The log markers
// are diagnostics only: a tailscale upgrade that rewords them zeroes those
// fields, nothing else.
type JoinSpans struct {
	Slot         float64 `json:"slotMs,omitempty"`         // a join slot (JoinParallel) acquired
	Key          float64 `json:"keyMs,omitempty"`          // the KeySource returned the auth key
	Start        float64 `json:"startMs,omitempty"`        // tsnet backend started (netstack, magicsock, LocalAPI)
	Login        float64 `json:"loginMs,omitempty"`        // controlclient doLogin (the Noise connection to control is next)
	RegisterSent float64 `json:"registerSentMs,omitempty"` // RegisterRequest sent with the key
	Registered   float64 `json:"registeredMs,omitempty"`   // control's RegisterResponse
	Starting     float64 `json:"startingMs,omitempty"`     // IPN state Starting (logged in, waiting on the first netmap)
	Netmap       float64 `json:"netmapMs,omitempty"`       // the node's first netmap (SelfChange)
	Running      float64 `json:"runningMs,omitempty"`      // IPN state Running; tsnet.Up returns
	Listening    float64 `json:"listeningMs,omitempty"`    // ingress listener open, object ready
}

// spanRec collects JoinSpans for one join.
type spanRec struct {
	mu sync.Mutex
	t0 time.Time
	s  JoinSpans
}

func newSpanRec(t0 time.Time) *spanRec { return &spanRec{t0: t0} }

// at records the first occurrence of a stage.
func (r *spanRec) at(f func(*JoinSpans) *float64) {
	if r == nil {
		return
	}
	v := float64(time.Since(r.t0).Microseconds()) / 1000
	r.mu.Lock()
	if p := f(&r.s); *p == 0 {
		*p = v
	}
	r.mu.Unlock()
}

func (r *spanRec) get() JoinSpans {
	if r == nil {
		return JoinSpans{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.s
}

// logf wraps a tsnet logger, timestamping controlclient's login markers.
func (r *spanRec) logf(next func(string, ...any)) func(string, ...any) {
	return func(format string, args ...any) {
		// controlclient's logger is prefixed ("control: "), so match anywhere.
		switch {
		case strings.Contains(format, "doLogin("):
			r.at(func(s *JoinSpans) *float64 { return &s.Login })
		case strings.Contains(format, "RegisterReq: onode="):
			r.at(func(s *JoinSpans) *float64 { return &s.RegisterSent })
		case strings.Contains(format, "RegisterReq: got response"):
			r.at(func(s *JoinSpans) *float64 { return &s.Registered })
		}
		next(format, args...)
	}
}

// watch records IPN state transitions and the first SelfChange until ctx ends.
func (r *spanRec) watch(ctx context.Context, lc *local.Client) {
	w, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState)
	if err != nil {
		return
	}
	defer w.Close()
	for {
		n, err := w.Next()
		if err != nil {
			return
		}
		if n.State != nil {
			switch *n.State {
			case ipn.Starting:
				r.at(func(s *JoinSpans) *float64 { return &s.Starting })
			case ipn.Running:
				r.at(func(s *JoinSpans) *float64 { return &s.Running })
			}
		}
		if n.SelfChange != nil {
			r.at(func(s *JoinSpans) *float64 { return &s.Netmap })
		}
	}
}
