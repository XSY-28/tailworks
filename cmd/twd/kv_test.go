package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rosskukulinski/twd/schema"
)

// TestCLIKV: twd kv namespace create|list|delete and key put|get|list|
// delete against a real mgmt: --binding resolved from the app's
// wrangler.jsonc, values from an argument, a file and stdin, metadata and
// TTL, list with --prefix, and Cloudflare's rules.
func TestCLIKV(t *testing.T) {
	e := newPubEnv(t)
	e.setPolicy(`{}`)
	app := writePlainApp(t)
	os.WriteFile(filepath.Join(app, "wrangler.jsonc"), []byte(`{"name": "support", "main": "src/index.js", "compatibility_date": "2026-04-01",
		"kv_namespaces": [{ "binding": "OAUTH_KV", "id": "oauth" }, { "binding": "CACHE" }]}`), 0o644)
	if out, err := e.call(t, cmdDeploy, app, "--allow-missing-grants", "--no-wait"); err != nil {
		t.Fatalf("deploy: %v\n%s", err, out)
	}
	t.Chdir(app)
	oldIn, oldOut := kvInput, kvOutput
	t.Cleanup(func() { kvInput, kvOutput = oldIn, oldOut })
	var got bytes.Buffer
	kvOutput = func() io.Writer { return &got }
	run := func(args ...string) string {
		t.Helper()
		out, err := e.call(t, cmdKV, args...)
		if err != nil {
			t.Fatalf("twd kv %v: %v\n%s", args, err, out)
		}
		return out
	}
	failing := func(want string, args ...string) {
		t.Helper()
		out, err := e.call(t, cmdKV, args...)
		if err == nil || !strings.Contains(err.Error()+cliText(err)+out, want) {
			t.Fatalf("twd kv %v: %v %s, want %q", args, err, out, want)
		}
	}
	get := func(args ...string) string {
		t.Helper()
		got.Reset()
		run(append([]string{"key", "get"}, args...)...)
		return got.String()
	}

	if out := run("namespace", "create", "oauth"); !strings.Contains(out, "KV namespace oauth created for workload support") {
		t.Fatal(out)
	}
	if out := run("namespace", "create", "oauth"); !strings.Contains(out, "already exists") {
		t.Fatal(out)
	}
	failing("unknown_store", "key", "put", "k", "v", "--binding", "CACHE") // CACHE was never created

	// Values from an argument, a file and stdin.
	run("key", "put", "client:1", `{"name":"app"}`, "--binding", "OAUTH_KV", "--metadata", `{ "kind": "client" }`)
	file := filepath.Join(t.TempDir(), "v")
	os.WriteFile(file, []byte("from a file\n"), 0o644)
	run("key", "put", "grant:1", "--path", file, "--namespace", "oauth", "--ttl", "3600")
	kvInput = func() io.Reader { return strings.NewReader("from stdin") }
	run("key", "put", "token:1", "--binding", "OAUTH_KV")
	if v := get("client:1", "--binding", "OAUTH_KV"); v != `{"name":"app"}` {
		t.Fatalf("get client:1: %q", v)
	}
	if v := get("grant:1", "--namespace", "oauth"); v != "from a file\n" {
		t.Fatalf("get grant:1: %q", v)
	}
	if v := get("token:1", "--binding", "OAUTH_KV"); v != "from stdin" {
		t.Fatalf("get token:1: %q", v)
	}

	// list, --prefix, --json.
	out := run("key", "list", "--binding", "OAUTH_KV", "--json")
	var res kvResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Keys == nil || len(*res.Keys) != 3 {
		t.Fatalf("list --json: %v %s", err, out)
	}
	var meta map[string]string
	keys := *res.Keys
	json.Unmarshal(keys[0].Metadata, &meta)
	if keys[0].Name != "client:1" || meta["kind"] != "client" {
		t.Fatalf("client:1: %+v %s", keys[0], keys[0].Metadata)
	}
	if x := keys[1].Expiration; x < time.Now().Unix()+3500 {
		t.Fatalf("grant:1 expiration %d", x)
	}
	if out := run("key", "list", "--binding", "OAUTH_KV", "--prefix", "tok"); !strings.Contains(out, `"token:1"`) || strings.Contains(out, "client") {
		t.Fatalf("list --prefix: %s", out)
	}
	if out := run("namespace", "list"); !strings.Contains(out, "oauth") {
		t.Fatalf("namespace list: %s", out)
	}

	// Rules, before anything reaches mgmt.
	failing("at least 60", "key", "put", "k", "v", "--namespace", "oauth", "--ttl", "5")
	failing("--metadata must be JSON", "key", "put", "k", "v", "--namespace", "oauth", "--metadata", "{")
	failing("no kv_namespaces binding", "key", "get", "k", "--binding", "NOPE")
	failing("need the namespace", "key", "get", "k")
	failing("not both", "key", "get", "k", "--binding", "OAUTH_KV", "--namespace", "oauth")
	failing("illegal key", "key", "get", "..", "--namespace", "oauth")
	failing("not_found", "key", "get", "nope", "--namespace", "oauth")

	// Every subcommand's --json output matches schema/cli.schema.json.
	checkJSON := func(def string, args ...string) {
		t.Helper()
		var v any
		if err := json.Unmarshal([]byte(run(append(args, "--json")...)), &v); err != nil {
			t.Fatalf("twd kv %v --json: %v", args, err)
		}
		if ps, err := schema.CheckCLI(def, v); err != nil || len(ps) > 0 {
			t.Fatalf("twd kv %v --json vs $defs/%q: %v %v", args, def, ps, err)
		}
	}
	checkJSON("kv namespace create", "namespace", "create", "scratch")
	checkJSON("kv namespace list", "namespace", "list")
	checkJSON("kv key put", "key", "put", "j", "v", "--namespace", "scratch", "--metadata", `{"a":1}`, "--ttl", "120")
	checkJSON("kv key get", "key", "get", "j", "--namespace", "scratch")
	checkJSON("kv key list", "key", "list", "--namespace", "scratch")
	checkJSON("kv key delete", "key", "delete", "j", "--namespace", "scratch")
	checkJSON("kv key list", "key", "list", "--namespace", "scratch") // empty: keys is []
	checkJSON("kv namespace delete", "namespace", "delete", "scratch", "--yes")

	// delete a key, then the namespace.
	run("key", "delete", "token:1", "--binding", "OAUTH_KV")
	failing("not_found", "key", "get", "token:1", "--binding", "OAUTH_KV")
	if out := run("namespace", "delete", "oauth", "--yes"); !strings.Contains(out, "deleted, with its data") {
		t.Fatal(out)
	}
	failing("unknown_store", "key", "list", "--binding", "OAUTH_KV")
}
