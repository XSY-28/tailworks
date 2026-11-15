package main_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rosskukulinski/twd/e2e/agenteval"
	"github.com/rosskukulinski/twd/e2e/cliflow"
	"github.com/rosskukulinski/twd/hostd/hostdtest"
	"github.com/rosskukulinski/twd/internal/policy"
	"github.com/rosskukulinski/twd/netd/nettest"
	"tailscale.com/tailcfg"
)

// TestAgentEval is issue #25's eval: a scripted coding agent (no LLM)
// replays the commands twd's skills tell it to run, in order (scaffold →
// dev → deploy → inspect → logs → teardown), against a real twd (twd up
// on testcontrol + a fake Tailscale API, single process). The first deploy
// fails the way a real first deploy does (acl_denied: the app's new tag
// has no tagOwners); the "user" adds the line from the error's hint and
// the agent re-runs the deploy step, as twd-deploy says. Every command in
// between also runs once with --json, and each output is checked against
// schema/cli.schema.json; every error against docs/errors.md.
//
// TWD_EVAL_DUMP=<dir> writes every run (args, stdout, stderr) there.
func TestAgentEval(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	wd, err := nettest.WorkerdBinary()
	if err != nil || !hostdtest.HasNode() {
		t.Skip("needs workerd and node (make deps)")
	}
	t.Setenv("TWD_WORKERD", wd)
	ctl, err := nettest.StartControl()
	if err != nil {
		t.Fatal(err)
	}
	defer ctl.Close()
	ctl.Server.SetGlobalAppCaps(tailcfg.PeerCapMap{nettest.TestCap: []tailcfg.RawMessage{nettest.TestCapValue}})
	fts := newFakeTS(t)
	api := httptest.NewServer(fts)
	defer api.Close()
	bin := cliflow.BuildBinaries(t)
	state, _ := hostdtest.ShortDir("twd-ev-")
	work := t.TempDir()
	keyFile := filepath.Join(work, "api.env")
	os.WriteFile(keyFile, []byte("TS_API_KEY=fake-not-a-key\n"), 0o600)
	e := &cliflow.Env{T: t, BinDir: bin, StateDir: state, WorkDir: work, Tailnet: "ttest.ts.net", APIKeyFile: keyFile,
		ControlURL: ctl.URL, TSAPIURL: api.URL, Prefix: "twd-v-", SingleProcess: true}
	apps := filepath.Join(work, "projects")
	os.MkdirAll(apps, 0o755)
	ag := &agenteval.Agent{T: t, Bin: filepath.Join(bin, "twd"), StateDir: state, Dir: apps}
	t.Cleanup(func() {
		if dir := os.Getenv("TWD_EVAL_DUMP"); dir != "" {
			os.MkdirAll(dir, 0o755)
			b, _ := json.MarshalIndent(ag.Log, "", "  ")
			os.WriteFile(filepath.Join(dir, "runs.json"), b, 0o644)
		}
		if t.Failed() {
			for _, c := range []string{"supervisor", "mgmt", "hostd"} {
				b, _ := os.ReadFile(filepath.Join(state, "logs", c+".log"))
				t.Logf("%s log (tail):\n%s", c, hostdtest.Trim(string(b[max(0, len(b)-4000):]), 4000))
			}
		}
		ag.Exec("down", "--purge")
		os.RemoveAll(state)
	})
	steps, err := agenteval.Steps()
	if err != nil {
		t.Fatal(err)
	}
	ok := func(def string, r agenteval.Run) {
		t.Helper()
		if err := agenteval.CheckResult(def, r); err != nil {
			t.Fatalf("%s %s: %v", r.Ref, strings.Join(r.Args, " "), err)
		}
	}
	doc := func(r agenteval.Run) map[string]any {
		t.Helper()
		v, err := agenteval.Doc(r)
		if err != nil {
			t.Fatal(err)
		}
		m, _ := v.(map[string]any)
		return m
	}
	// okOrError: commands whose success needs what this harness doesn't
	// have; either outcome must follow the contract.
	okOrError := func(def string, r agenteval.Run) {
		t.Helper()
		if r.OK() {
			ok(def, r)
			return
		}
		if _, err := agenteval.CheckError(r); err != nil {
			t.Fatal(err)
		}
	}

	// The operator starts twd (the skills tell the agent to ask for this).
	up := ag.Exec(append(e.UpCommand(), "--json")...)
	ok("up", up)

	// scaffold: twd init → cd.
	for _, r := range ag.Replay(steps["scaffold"]) {
		ok("init", r)
	}
	if filepath.Base(ag.Dir) != "hello-mcp" {
		t.Fatalf("after the scaffold step the agent is in %s", ag.Dir)
	}
	// dev: the --once smoke test answers the MCP initialize.
	for _, r := range ag.Replay(steps["dev"]) {
		ok("dev", r)
		if s, _ := doc(r)["smoke"].(map[string]any); s == nil || s["status"] != float64(200) {
			t.Fatalf("twd dev --once smoke: %s", r.Stdout)
		}
	}
	// deploy, first try: the policy has no tagOwners for the app's new tag.
	first := ag.Replay(steps["deploy"])
	de, err := agenteval.CheckError(first[0])
	if err != nil {
		t.Fatal(err)
	}
	hint, _ := de["hint"].(string)
	if de["code"] != "acl_denied" || !strings.Contains(hint, `"tag:twd-wl-hello-mcp"`) || !strings.Contains(hint, "tagOwners") {
		t.Fatalf("first deploy: want acl_denied with the tagOwners line in hint, got %v", de)
	}
	if _, err := agenteval.CheckStream("deploy.event", first[0].Stderr[:strings.LastIndex(strings.TrimRight(first[0].Stderr, "\n"), "\n")+1]); err != nil {
		t.Fatalf("deploy events: %v", err)
	}
	// The user adds what the hint says; the agent runs the step again.
	fts.mu.Lock()
	fts.policy, err = policy.MergeWith(fts.policy, policy.Snippet{TagOwners: map[string][]string{"tag:twd-wl-hello-mcp": {"autogroup:admin", "tag:twd"}}}, policy.Options{})
	fts.etag++
	fts.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	// twd-deploy: mgmt re-reads the policy every few seconds, so retry
	// acl_denied for a little while before going back to the user.
	var dep agenteval.Run
	for i := 0; ; i++ {
		dep = ag.Replay(steps["deploy"])[0]
		if dep.OK() || i == 10 || !strings.Contains(dep.Stderr, `"code":"acl_denied"`) {
			break
		}
		time.Sleep(2 * time.Second)
	}
	ok("deploy", dep)
	evs, err := agenteval.CheckStream("deploy.event", dep.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, ev := range evs {
		kinds[ev["event"].(string)] = true
	}
	for _, k := range []string{"start", "preflight", "build", "placed", "status", "ready"} {
		if !kinds[k] {
			t.Fatalf("deploy --json events: no %q in\n%s", k, dep.Stderr)
		}
	}
	d := doc(dep)
	if d["status"] != "active" || d["workload"] != "hello-mcp" {
		t.Fatalf("deploy result: %s", dep.Stdout)
	}
	// inspect: ls and status.
	for _, r := range ag.Replay(steps["inspect"]) {
		ok(r.Args[0], r)
	}
	// logs: NDJSON lines (a fresh MCP server may have logged nothing yet).
	for _, r := range ag.Replay(steps["logs"]) {
		if !r.OK() {
			t.Fatalf("%s: %s", r.Ref, r.Stderr)
		}
		if _, err := agenteval.CheckLogs(r.Stdout); err != nil {
			t.Fatal(err)
		}
	}

	// Every other command once, with --json, against this twd.
	if _, err := agenteval.CheckLogs(ag.Exec("logs", "hello-mcp", "--console", "-n", "20", "--json").Stdout); err != nil {
		t.Fatal(err)
	}
	ok("version", ag.Exec("version", "--json"))
	if r := ag.Exec("doctor", "--json"); true {
		if err := agenteval.CheckDoc("doctor", doc(r)); err != nil {
			t.Fatal(err)
		}
		if !r.OK() {
			if _, err := agenteval.CheckError(agenteval.Run{Args: r.Args, Stderr: r.Stderr, Exit: r.Exit}); err != nil {
				t.Fatal(err)
			}
		}
	}
	ok("tenant ls", ag.Exec("tenant", "ls", "--json"))
	tenantID, _ := doc(up)["tenantID"].(string)
	ok("tenant set", ag.Exec("tenant", "set", tenantID, "--max-objects", "500", "--json"))
	ok("host ls", ag.Exec("host", "ls", "--json"))
	ok("host add", ag.Exec("host", "add", "twd-v-extra", "--out", filepath.Join(work, "extra.join"), "--json"))
	ok("host rm", ag.Exec("host", "rm", "twd-v-extra", "--json")) // 204 from mgmt: nothing to clean up
	ag.Stdin = "not-a-real-secret"
	ok("secret put", ag.Exec("secret", "put", "API_TOKEN", "--json"))
	ok("secret ls", ag.Exec("secret", "ls", "--json"))
	ok("secret rm", ag.Exec("secret", "rm", "API_TOKEN", "--json"))
	okOrError("expose", ag.Exec("expose", "off", "hello-mcp", "--json"))
	ok("audit", ag.Exec("audit", "--all", "--json"))
	ok("image public ls", ag.Exec("image", "public", "ls", "--json"))
	ok("docs ls", ag.Exec("docs", "ls", "--json"))
	ok("docs search", ag.Exec("docs", "search", "acl_denied", "--json"))
	ok("docs show", ag.Exec("docs", "show", "errors.md#acl_denied", "--json"))
	ok("agent ls", ag.Exec("agent", "ls", "--json"))
	ok("agent show", ag.Exec("agent", "show", "twd-deploy", "--json"))
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	ag.Env = []string{"HOME=" + home}
	ok("agent setup", ag.Exec("agent", "setup", "--dry-run", "--json"))
	ok("agent remove", ag.Exec("agent", "remove", "--dry-run", "--json"))
	ag.Env = nil
	// kill: the workload's own object (twd ls lists it), then undeploy.
	var objID string
	for _, o := range doc(ag.Exec("ls", "--json"))["objects"].([]any) {
		if m := o.(map[string]any); m["workload"] == "hello-mcp" && m["parent"] == nil {
			objID, _ = m["objectID"].(string)
		}
	}
	if objID == "" {
		t.Fatal("twd ls: no object for hello-mcp")
	}
	ok("kill", ag.Exec("kill", objID, "--reason", "eval", "--json"))
	ok("audit", ag.Exec("audit", objID, "--json"))
	if _, err := agenteval.CheckError(ag.Exec("kill", objID, "--json")); err != nil {
		t.Fatal(err) // already killed: a typed error
	}

	// teardown: undeploy, as twd-teardown says.
	for _, r := range ag.Replay(steps["teardown"]) {
		ok("undeploy", r)
	}
	ok("down", ag.Exec("down", "--json"))

}
