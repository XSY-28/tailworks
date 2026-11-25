//go:build e2edataplane

package dataplane

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Wall-clock skew on a host (H3, ADR-028). The hosts are a test build
// (-tags twtestclock: internal/wallclock): each follows the offset in
// <work>/<host>/clock, and with libfaketime its workerds follow it too, so
// the whole host reads a wrong wall clock. Monotonic clocks are untouched.
// Without libfaketime only hostd could be skewed, which no real host does
// (its workerds would disagree with it), so the skew fault is left out.

// faketimeLib finds libfaketime: TW_DP_FAKETIME_LIB, or where apt and
// Homebrew put it. "" if there is none.
func faketimeLib() string {
	if v := os.Getenv("TW_DP_FAKETIME_LIB"); v != "" {
		return v
	}
	for _, pat := range []string{
		"/usr/lib/*/faketime/libfaketime.so.1",
		"/usr/lib/faketime/libfaketime.so.1",
		"/opt/homebrew/lib/faketime/libfaketime.1.dylib",
		"/usr/local/lib/faketime/libfaketime.1.dylib",
	} {
		if m, _ := filepath.Glob(pat); len(m) > 0 {
			return m[0]
		}
	}
	return ""
}

// skewFaults are the scenarios that get the skew fault when libfaketime is
// there (init, so the scenario lists in nemesis_test.go stay as they are).
var skewFaults = []string{"fencing", "mixed"}

func init() {
	faults["skew"] = fault{"skew", skewHost}
	if faketimeLib() == "" {
		return
	}
	for _, s := range skewFaults {
		scenarios[s] = append(scenarios[s], "skew")
	}
}

// hostClocks is each host's current offset, as the harness last set it.
type hostClocks struct {
	mu  sync.Mutex
	off map[string]time.Duration
	cli *http.Client // reads the workload's /clock
}

func (c *cluster) clockFile(host string) string { return filepath.Join(c.work, host, "clock") }

// clockEnv is the test hook's environment for a host process (nil for mgmt).
func (c *cluster) clockEnv(name string) []string {
	if name == "mgmt" {
		return nil
	}
	os.MkdirAll(filepath.Join(c.work, name), 0o700)
	env := []string{"TW_TEST_WALL_CLOCK_FILE=" + c.clockFile(name)}
	if lib := faketimeLib(); lib != "" {
		env = append(env, "TW_TEST_FAKETIME_LIB="+lib)
	}
	return env
}

// offset is host's wall-clock offset now.
func (c *cluster) offset(host string) time.Duration {
	c.clocks.mu.Lock()
	defer c.clocks.mu.Unlock()
	return c.clocks.off[host]
}

// setClock steps host's wall clock (hostd at once, its workerds within
// about a second).
func (c *cluster) setClock(host string, d time.Duration) {
	c.clocks.mu.Lock()
	c.clocks.off[host] = d
	c.clocks.mu.Unlock()
	p := c.clockFile(host)
	os.WriteFile(p+".w", []byte(d.String()), 0o600)
	os.Rename(p+".w", p)
}

// workerdClock is how far the workload's Date.now() on dns is from ours.
func (c *cluster) workerdClock(dns string) (time.Duration, error) {
	var r struct{ Now int64 }
	t0 := time.Now()
	if _, err := getJSON(c.clocks.cli, "http://"+dns+"/clock", &r); err != nil {
		return 0, err
	}
	mid := t0.Add(time.Since(t0) / 2)
	return time.UnixMilli(r.Now).Sub(mid), nil
}

// waitClock waits up to 10 s for the workload on dns to read our clock
// plus want (±1.5 s), and returns what it read.
func (c *cluster) waitClock(dns string, want time.Duration) (time.Duration, error) {
	var got time.Duration
	var err error
	for i := 0; i < 20; i++ {
		if got, err = c.workerdClock(dns); err == nil && (got-want).Abs() < 1500*time.Millisecond {
			return got, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err == nil {
		err = fmt.Errorf("workerd's clock is %v off, want %v", got.Round(time.Millisecond), want)
	}
	return got, err
}

// checkSkewHook proves the hook works before the nemesis relies on it:
// the workload on h (the first holder) reads a +90 s clock, then the true
// one again. Without libfaketime it does nothing.
func checkSkewHook(t *testing.T, c *cluster, cli *http.Client, h, dns string) {
	c.clocks.cli = cli
	if faketimeLib() == "" {
		t.Logf("skew: no libfaketime (set TW_DP_FAKETIME_LIB), so no skew fault")
		return
	}
	c.setClock(h, 90*time.Second)
	got, err := c.waitClock(dns, 90*time.Second)
	c.setClock(h, 0)
	if err != nil {
		c.dumpLogs()
		t.Fatalf("skew hook: %v", err)
	}
	if _, err := c.waitClock(dns, 0); err != nil {
		t.Fatalf("skew hook: back to 0: %v", err)
	}
	t.Logf("skew hook works: dp on %s read +%v, then the true clock", h, got.Round(time.Millisecond))
}

// skewHost steps the lease holder's wall clock forward or back by 1–6 min
// (from wherever it was) and leaves it there: every later fault on this
// host, takeovers included, happens with a wrong clock, and a later skew
// may step it again. ADR-028 says ownership doesn't read the wall clock,
// so nothing should notice.
func skewHost(n *nemesis, h string) string {
	step := n.between(time.Minute, 6*time.Minute).Round(time.Second)
	if n.rnd.Intn(2) == 0 {
		step = -step
	}
	prev := n.c.offset(h)
	to := prev + step
	if to.Abs() > 8*time.Minute { // keep it within ±8 min: step back toward 0
		to = prev - step
	}
	n.c.setClock(h, to)
	d := n.tg.get()
	if d.Host != h {
		return fmt.Sprintf("%v → %v (the workload moved off %s, not checked)", prev, to, h)
	}
	got, err := n.c.waitClock(d.Endpoint.MagicDNS, to)
	if err != nil {
		return fmt.Sprintf("%v → %v; workerd: %v", prev, to, err)
	}
	return fmt.Sprintf("%v → %v; workerd reads %+.1fs", prev, to, got.Seconds())
}
