package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestTypesEnv: twd types declares every supported binding of the wrangler
// config, the secrets it can see, and nothing twd doesn't support; --env
// types that environment; --check fails on a stale file and passes after a
// rewrite.
func TestTypesEnv(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"twd.jsonc": `{ "name": "shop", "tailnet": { "tags": ["tag:twd-wl-shop"] } }`,
		"wrangler.jsonc": `{
  "name": "shop",
  "main": "src/worker.ts",
  "compatibility_date": "2026-04-01",
  "vars": { "GREETING": "hi", "RETRIES": 3, "DEBUG": false, "LIMITS": { "max-items": 5, "tags": ["a"] } },
  "durable_objects": { "bindings": [
    { "name": "CART", "class_name": "Cart" },
    { "name": "REMOTE", "class_name": "Other", "script_name": "other-worker" }
  ] },
  "kv_namespaces": [{ "binding": "CACHE", "id": "cache" }],
  "r2_buckets": [{ "binding": "FILES", "bucket_name": "files" }],
  "d1_databases": [{ "binding": "DB", "database_name": "db" }],
  "queues": { "producers": [{ "binding": "JOBS", "queue": "jobs" }], "consumers": [{ "queue": "jobs" }] },
  "workflows": [{ "name": "checkout", "binding": "CHECKOUT", "class_name": "Checkout" }],
  "services": [{ "binding": "AUTH", "service": "auth" }],
  "secrets": { "required": ["STRIPE_KEY"] },
  "env": { "staging": { "vars": { "STAGE_ONLY": "1" } } }
}`,
		".dev.vars":         "API_TOKEN=s3cret\n",
		".dev.vars.example": "WEBHOOK_SECRET=\n",
	})
	if err := cmdTypes([]string{dir}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, typesFile))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		"interface Env {\n",
		"  GREETING: string;\n",
		"  RETRIES: number;\n",
		"  DEBUG: boolean;\n",
		`    "max-items": number;`,
		"    tags: string[];",
		`  CART: DurableObjectNamespace<import("./src/worker").Cart>;`,
		"  REMOTE: DurableObjectNamespace;\n",
		"  CACHE: KVNamespace;\n",
		"  FILES: R2Bucket;\n",
		"  DB: D1Database;\n",
		"  JOBS: Queue;\n",
		"  CHECKOUT: Workflow;\n",
		"  STRIPE_KEY: string;\n",
		"  API_TOKEN: string;\n",
		"  WEBHOOK_SECRET: string;\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%s lacks %q:\n%s", typesFile, want, got)
		}
	}
	for _, bad := range []string{"AUTH", "STAGE_ONLY", "s3cret", "shop"} {
		if strings.Contains(got, bad) {
			t.Errorf("%s has %q:\n%s", typesFile, bad, got)
		}
	}

	if err := cmdTypes([]string{dir, "--check"}); err != nil {
		t.Errorf("--check right after writing: %v", err)
	}
	// A new var makes it stale; --check names the fix and writes nothing.
	writeFiles(t, dir, map[string]string{".dev.vars": "API_TOKEN=x\nNEW_ONE=y\n"})
	err = cmdTypes([]string{dir, "--check"})
	if ce := asCLIErr(err); ce == nil || ce.Code != "types_stale" {
		t.Fatalf("--check on a stale file: %v, want types_stale", err)
	}
	if b2, _ := os.ReadFile(filepath.Join(dir, typesFile)); string(b2) != got {
		t.Error("--check rewrote the file")
	}

	// --env staging: the env's vars only (vars and bindings aren't inherited).
	out := filepath.Join(dir, "types", "env.d.ts")
	if err := cmdTypes([]string{dir, "--env", "staging", "--out", "types/env.d.ts"}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(out)
	if s := string(b); !strings.Contains(s, "STAGE_ONLY: string;") || strings.Contains(s, "GREETING") || strings.Contains(s, "CART") || !strings.Contains(s, "--env staging") {
		t.Errorf("--env staging:\n%s", s)
	}
}

// TestTypesNode: a runtime "node" app gets process.env.
func TestTypesNode(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"twd.jsonc": `{ "name": "svc", "runtime": "node", "main": "src/server.ts",
  "node": { "listen": "unix", "env": { "LOG_LEVEL": "info" } },
  "tailnet": { "tags": ["tag:twd-wl-svc"] } }`,
		".dev.vars": "DB_PASSWORD=x\n",
	})
	if err := cmdTypes([]string{dir}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, typesFile))
	s := string(b)
	for _, want := range []string{"declare namespace NodeJS {", "interface ProcessEnv {", "TWD_SOCKET: string;", "TWD_WORKLOAD: string;", "LOG_LEVEL: string;", "DB_PASSWORD: string;"} {
		if !strings.Contains(s, want) {
			t.Errorf("lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "PORT") || strings.Contains(s, "interface Env") {
		t.Errorf("listen unix: no PORT, and no Env:\n%s", s)
	}
}

// TestTypesTemplates: every template ships the worker-configuration.d.ts
// that `twd types` writes for it, and a tsconfig.json that includes it.
// (make templates-typecheck runs tsc over them.)
func TestTypesTemplates(t *testing.T) {
	ct, err := createTwdDir()
	if err != nil {
		t.Skip("create-twd not found:", err)
	}
	ents, _ := os.ReadDir(filepath.Join(ct, "templates"))
	for _, e := range ents {
		dir := filepath.Join(ct, "templates", e.Name())
		if err := cmdTypes([]string{dir, "--check"}); err != nil {
			t.Errorf("template %s: %v (run: twd types %s)", e.Name(), err, dir)
		}
		if !inTsconfig(dir, filepath.Join(dir, typesFile)) {
			t.Errorf("template %s: tsconfig.json doesn't include %s", e.Name(), typesFile)
		}
		if _, err := os.Stat(filepath.Join(dir, "tsconfig.json")); err != nil {
			t.Errorf("template %s: no tsconfig.json", e.Name())
		}
	}
}

// TestNPMVersions: the npm packages and the templates' dependencies on
// them are at VERSION (scripts/npm-versions.mjs; the release publishes
// them at the tag's version).
func TestNPMVersions(t *testing.T) {
	ct, err := createTwdDir()
	if err != nil {
		t.Skip("create-twd not found:", err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	root := filepath.Join(ct, "..", "..", "..")
	out, err := exec.Command(node, filepath.Join(root, "scripts", "npm-versions.mjs")).CombinedOutput()
	if err != nil {
		t.Errorf("scripts/npm-versions.mjs: %v\n%s", err, out)
	}
}
