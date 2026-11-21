package hostd_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rosskukulinski/tailworks/hostd/fakemgmt"
	"github.com/rosskukulinski/tailworks/hostd/hostdtest"
)

// TestRT3_H17_CronOverlap (security/LEDGER.md H17, issue #152): a
// scheduled() that leaves work running past the cron timeout (here in
// ctx.waitUntil(), after the handler itself has returned) must not overlap
// with the trigger's next run, and must not outlive the timeout. The run
// lasts until its waitUntil work settles, the next tick is skipped while it
// is in flight, and at CronTimeout hostd drops the run, which cancels it in
// workerd. The tick is driven with chosen wall-clock times (CronTickAt);
// everything below it is the production path, with a short CronTimeout.
func TestRT3_H17_CronOverlap(t *testing.T) {
	const timeout = 2 * time.Second
	frozen := time.Date(2000, 1, 1, 0, 1, 0, 0, time.UTC) // see TestCronTriggers
	s := startSuiteCapsOpts(t, fakemgmt.Config{}, nil, func(o *hostdtest.Options) {
		o.CronTimeout = timeout
		o.CronClock = func() time.Time { return frozen }
	})
	e := s.e
	// "1 0 4 1 *" records its start, then sleeps SLEEP_MS in waitUntil and
	// records its end.
	dep := e.Deploy(hostdtest.Fixture("cron"), "cron", map[string]string{"SLEEP_MS": "5000"})
	wl := e.WaitActive("cron", dep, 60*time.Second)
	s.waitPeer(wl.IP)

	phases := func() map[string]int {
		t.Helper()
		code, _, b := s.do("GET", "http://"+wl.IP+"/runs", nil)
		var out []map[string]any
		if code != 200 || json.Unmarshal(b, &out) != nil {
			t.Fatalf("/runs: %d %s", code, b)
		}
		n := map[string]int{}
		for _, r := range out {
			if r["cron"] == "1 0 4 1 *" {
				p, _ := r["phase"].(string)
				n[p]++
			}
		}
		return n
	}

	at := time.Date(2027, 1, 4, 0, 1, 0, 0, time.UTC)
	st := time.Now()
	first := make(chan int, 1)
	go func() { first <- e.Host.CronTickAt(at) }()
	deadline := time.Now().Add(10 * time.Second)
	for phases()["start"] == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the first run never started")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The trigger comes due again while the first run's work is in flight.
	if n := e.Host.CronTickAt(at.AddDate(1, 0, 0)); n != 0 {
		t.Fatalf("next tick started %d runs while the previous run was still in flight (overlap)", n)
	}
	if n := <-first; n != 1 {
		t.Fatalf("first tick started %d runs, want 1", n)
	}
	if d := time.Since(st); d < timeout {
		t.Fatalf("the run ended after %v, before its waitUntil work (the run must last until that settles or the %v timeout)", d, timeout)
	}

	// Past SLEEP_MS: the timed-out run was cancelled, not left running.
	time.Sleep(time.Until(st.Add(6500 * time.Millisecond)))
	if p := phases(); p["start"] != 1 || p["end"] != 0 {
		t.Fatalf("phases %v: want one start and no end (the run outlived its timeout)", p)
	}

	// A cancelled run frees the trigger: the next due minute runs it.
	if n := e.Host.CronTickAt(at.AddDate(2, 0, 0)); n != 1 {
		t.Fatalf("trigger stuck after a timed-out run: %d", n)
	}
}
