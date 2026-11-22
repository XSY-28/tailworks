//go:build e2etwupgrade

package twupgrade

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rosskukulinski/tailworks/e2e/cliflow"
	"github.com/rosskukulinski/tailworks/hostd/hostdtest"
	"github.com/rosskukulinski/tailworks/netd"
	"github.com/rosskukulinski/tailworks/netd/nettest"
)

// Time budgets for `tw up` (single process, to ready) and `tw down` on
// testcontrol (#245). Measured after the fix: up 0.3–0.7 s, down 0.1–0.3 s
// (macOS and a Linux VM). Before it: down 5.0 s every time (mgmt's node
// waited out its LocalAPI's 5 s shutdown grace for the HTTPS certificate
// prefetch), up 3.4–6.5 s on Linux (tw up's join and mgmt's start waited
// for netcheck and DERP). The budgets leave room for a loaded machine.
const (
	upBudget   = 4 * time.Second
	downBudget = 2500 * time.Millisecond
)

// TestUpDownBudget (#245): `tw up`, `tw down`, and `tw up` again on the
// saved state (what a service restart or a reboot runs), three times, each
// within its budget; the last down purges. Run it with `make e2e-tw-upgrade`.
func TestUpDownBudget(t *testing.T) {
	wd, err := nettest.WorkerdBinary()
	if err != nil {
		t.Skip(err)
	}
	root, err := nettest.FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	bin := buildBins(t, root, "0.0.9")
	ctl, err := nettest.StartControl()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctl.Close)
	api := newFakeAPI(t, "v0.0.9")
	state, err := hostdtest.ShortDir("tw-ud-")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	env := append(os.Environ(), "TW_WORKERD="+wd, "TW_NO_UPDATE_CHECK=1", "TW_RELEASE_API="+api.URL)
	tw := func(args ...string) (string, time.Duration) {
		t.Helper()
		cmd := exec.Command(filepath.Join(bin, "tw"), append([]string{"--state-dir", state}, args...)...)
		cmd.Dir, cmd.Env = work, env
		var so, se bytes.Buffer
		cmd.Stdout, cmd.Stderr = &so, &se
		st := time.Now()
		if err := cmd.Run(); err != nil {
			t.Fatalf("tw %s: %v\n%s\n%s", strings.Join(args, " "), err, so.String(), netd.Redact(se.String()))
		}
		return so.String(), time.Since(st)
	}
	t.Cleanup(func() {
		if t.Failed() {
			for _, c := range []string{"supervisor", "mgmt", "hostd"} {
				b, _ := os.ReadFile(filepath.Join(state, "logs", c+".log"))
				t.Logf("---- %s.log (tail)\n%s", c, netd.Redact(string(b[max(0, len(b)-6000):])))
			}
		}
		exec.Command(filepath.Join(bin, "tw"), "--state-dir", state, "down", "--purge").Run()
		os.RemoveAll(state)
	})
	keyFile := cliflow.WriteFakeOAuthFile(t, filepath.Join(work, "oauth.env"))
	up := []string{"up", "--oauth-client-file", keyFile, "--tailnet", "ttest.ts.net", "--yes", "--control-url", ctl.URL, "--tsapi-url", api.URL,
		"--hostname-prefix", "tw-ud-", "--mgmt-hostname", "tw-ud-mgmt", "--host-id", "tw-ud", "--json"}
	for i := range 3 {
		down := []string{"down", "--json"}
		if i == 2 {
			down = append(down, "--purge")
		}
		for _, c := range [][]string{up, down} {
			so, took := tw(c...)
			var r struct {
				Timings map[string]float64 `json:"timingsMs"`
			}
			json.Unmarshal([]byte(so), &r)
			budget := upBudget
			if c[0] == "down" {
				budget = downBudget
			}
			t.Logf("tw %s (%d): %v, phases (ms from start) %v", c[0], i+1, took.Round(time.Millisecond), r.Timings)
			if took > budget {
				t.Errorf("tw %s (%d) took %v, budget %v", c[0], i+1, took.Round(time.Millisecond), budget)
			}
		}
	}
}
