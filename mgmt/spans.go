package mgmt

import "time"

// mintSpans times one mintOne attempt, stage by stage, for the "mint spans"
// log line (cold-start breakdown). Durations use the replica's monotonic
// clock: they are diagnostics, never ownership decisions (those use m.now).
// queued is create → this attempt on the store clock.
type mintSpans struct {
	start, last                                                        time.Time
	queued                                                             time.Duration
	load, claim, throttle, reclaim, broker, createKey, custody, commit time.Duration
}

// mark sets *d to the time since the previous mark.
func (s *mintSpans) mark(d *time.Duration) {
	now := time.Now()
	if s.last.IsZero() {
		s.last = s.start
	}
	*d = now.Sub(s.last)
	s.last = now
}

func msf(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func (s *mintSpans) attrs() []any {
	return []any{"queued_ms", msf(s.queued), "load_ms", msf(s.load), "claim_ms", msf(s.claim), "throttle_ms", msf(s.throttle),
		"reclaim_ms", msf(s.reclaim), "broker_ms", msf(s.broker), "createkey_ms", msf(s.createKey), "custody_ms", msf(s.custody),
		"commit_ms", msf(s.commit), "attempt_ms", msf(s.last.Sub(s.start)), "create_to_minted_ms", msf(s.queued + s.last.Sub(s.start))}
}
