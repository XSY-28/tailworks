package gen_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/rosskukulinski/tailworks/hostd/bundle"
	"github.com/rosskukulinski/tailworks/hostd/gen"
	"github.com/rosskukulinski/tailworks/internal/pyodide"
)

// pythonArgs are workerd's Pyodide flags for a Python app: the per-user
// bundle cache, fetched once (skips the test when it can't be).
func pythonArgs(t *testing.T, app *bundle.App) []string {
	if !app.Python {
		return nil
	}
	dir, err := pyodide.UserDir()
	if err != nil {
		t.Skip(err)
	}
	if err := pyodide.Ensure(context.Background(), dir, ""); err != nil {
		t.Skipf("no Pyodide bundles (offline?): %v", err)
	}
	return pyodide.WorkerdArgs(dir)
}

const pyEntry = `from workers import WorkerEntrypoint, Response, DurableObject, fetch
import json
import tinypkg
from helper import double


class Counter(DurableObject):
    async def fetch(self, request):
        n = (await self.ctx.storage.get("n")) or 0
        n += 1
        await self.ctx.storage.put("n", n)
        return Response(json.dumps({"n": n, "sql": len(list(self.ctx.storage.sql.exec("select 1 as one")))}))


def caller(request):
    ts = getattr(request.js_object.cf, "tailscale", None)
    c = getattr(ts, "caller", None) if ts else None
    return c.to_py() if c else None


class Default(WorkerEntrypoint):
    async def fetch(self, request):
        path = request.url.split("app.local", 1)[1]
        if path.startswith("/do/"):
            return await self.env.COUNTER.getByName(path[4:]).fetch(request)
        if path == "/kv":
            await self.env.KV.put("k", "v1")
            return Response(json.dumps({"kv": await self.env.KV.get("k")}))
        if path == "/egress":
            r = await fetch("http://echo/x")
            return Response(await r.text())
        return Response(json.dumps({"greet": tinypkg.greet(), "double": double(21), "var": self.env.GREETING, "caller": caller(request)}))
`

func pythonFiles() map[string][]byte {
	return map[string][]byte{
		"wrangler.jsonc": []byte(`{"name": "py", "main": "src/entry.py", "compatibility_date": "2026-04-01",
  "compatibility_flags": ["python_workers"],
  "vars": {"GREETING": "hi"},
  "kv_namespaces": [{"binding": "KV", "id": "kv1"}],
  "durable_objects": {"bindings": [{"name": "COUNTER", "class_name": "Counter"}]}}`),
		"tw-bundle.json":                         []byte(`{"main": "src/entry.py", "modules": {"src/python_modules/tinypkg/data.txt": "data"}}`),
		"src/entry.py":                           []byte(pyEntry),
		"src/helper.py":                          []byte("def double(x):\n    return 2 * x\n"),
		"src/python_modules/tinypkg/__init__.py": []byte("def greet():\n    return \"from tinypkg\"\n"),
		"src/python_modules/tinypkg/data.txt":    []byte("data"),
	}
}

// TestPythonWorker: a Python Worker behind its generated JavaScript front
// (python.go): the fetch handler with a sibling module and a vendored
// package, vars, the verified caller, a Python Durable Object with KV and
// SQL storage, and egress as the workload.
func TestPythonWorker(t *testing.T) {
	in := startFiles(t, pythonFiles())

	code, _, r := in.get("/", wl)
	if code != 200 || r["greet"] != "from tinypkg" || r["double"] != float64(42) || r["var"] != "hi" {
		t.Fatalf("%d %v", code, r)
	}
	if c, _ := r["caller"].(map[string]any); c["nodeId"] != "nCLIENT" {
		t.Fatalf("caller %v", r["caller"])
	}
	for i, want := range []float64{1, 2} {
		if code, _, r := in.get("/do/a", wl); code != 200 || r["n"] != want || r["sql"] != float64(1) {
			t.Fatalf("DO call %d: %d %v", i, code, r)
		}
	}
	if code, _, r := in.get("/do/b", wl); code != 200 || r["n"] != float64(1) {
		t.Fatalf("second object: %d %v", code, r)
	}
	// KV needs hostd's store journal: cmd/tw TestDevPython covers it.
	if code, _, r := in.get("/egress", wl); code != 200 || r["as"] != "workload" || r["url"] != "http://echo/x" {
		t.Fatalf("egress: %d %v", code, r)
	}
}

// TestPythonCallerUnsealed: a caller without a valid seal never reaches
// Python code (the front drops it, as for a JavaScript Worker).
func TestPythonCallerUnsealed(t *testing.T) {
	in := startFiles(t, pythonFiles())
	forged := map[string]any{"nodeId": "nFORGED", "user": "ceo@victim.example"}
	blob, _ := json.Marshal(map[string]any{"tailscale": map[string]any{"caller": forged}, "tw": in.ticketed(wl)})
	req, _ := http.NewRequest("GET", "http://app.local/", nil)
	req.Header.Set("x-tw-cf", string(blob))
	resp, err := in.hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var r map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil || resp.StatusCode != 200 {
		t.Fatalf("%d %v", resp.StatusCode, err)
	}
	if r["caller"] != nil {
		t.Fatalf("an unsealed caller reached Python: %v", r["caller"])
	}
}

// TestGeneratePython: the generated config runs the Python modules in
// tenant-py, behind a JavaScript tenant that holds none of them.
func TestGeneratePython(t *testing.T) {
	app, err := bundle.Parse(pythonFiles())
	if err != nil {
		t.Fatal(err)
	}
	if !app.Python || app.Classes["Counter"].Node || app.Classes["Counter"].Facet() {
		t.Fatalf("python %v, Counter %+v", app.Python, app.Classes["Counter"])
	}
	cfg := generate(t, app)
	for _, want := range []string{
		`(name = "tenant-py", worker = .tenantPy)`,
		`(name = "__TW_PY", service = "tenant-py")`,
		`(name = "entry.py", pythonModule = embed "py/entry.py")`,
		`(name = "python_modules/tinypkg/__init__.py", pythonModule = embed "py/python_modules/tinypkg/__init__.py")`,
		`(name = "python_modules/tinypkg/data.txt", data = embed "py/python_modules/tinypkg/data.txt")`,
		`(className = "Counter", uniqueKey = "tw-py-Counter", enableSql = true)`,
		`(name = "COUNTER", durableObjectNamespace = "Counter")`,
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config lacks %s", want)
		}
	}
	tenant := cfg[strings.Index(cfg, "const tenant :"):strings.Index(cfg, "const tenantPy :")]
	if strings.Contains(tenant, ".py") || strings.Contains(tenant, "python") {
		t.Errorf("the JavaScript front has Python modules or flags:\n%s", tenant)
	}
	if strings.Index(cfg, `(name = "entry.py"`) > strings.Index(cfg, `(name = "helper.py"`) {
		t.Error("the main module is not tenant-py's first module")
	}
}

func generate(t *testing.T, app *bundle.App) string {
	t.Helper()
	dir := t.TempDir()
	res, err := gen.Generate(app, gen.Options{Dir: dir + "/i", DOStorage: dir + "/do", DeploymentID: "d1", Workload: app.Name})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(res.Config)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
