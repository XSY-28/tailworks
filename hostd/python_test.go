package hostd_test

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rosskukulinski/tailworks/hostd"
	"github.com/rosskukulinski/tailworks/hostd/fakemgmt"
	"github.com/rosskukulinski/tailworks/hostd/hostdtest"
	"github.com/rosskukulinski/tailworks/hostd/mgmtapi"
	"github.com/rosskukulinski/tailworks/internal/pyodide"
	"github.com/rosskukulinski/tailworks/netd/nettest"
)

func pythonBundle() map[string][]byte {
	return map[string][]byte{
		"wrangler.jsonc": []byte(`{"name": "py", "main": "src/entry.py", "compatibility_date": "2026-04-01",
  "compatibility_flags": ["python_workers"],
  "durable_objects": {"bindings": [{"name": "COUNTER", "class_name": "Counter"}]}}`),
		"tailworks.jsonc": []byte(`{"tailnet": {"tags": ["tag:tw-wl-py"]}}`),
		"tw-bundle.json":  []byte(`{"main": "src/entry.py"}`),
		"src/entry.py": []byte(`from workers import WorkerEntrypoint, Response, DurableObject
import json


class Counter(DurableObject):
    async def fetch(self, request):
        n = (await self.ctx.storage.get("n")) or 0
        await self.ctx.storage.put("n", n + 1)
        return Response(str(n + 1))


class Default(WorkerEntrypoint):
    async def fetch(self, request):
        ts = getattr(request.js_object.cf, "tailscale", None)
        c = getattr(ts, "caller", None) if ts else None
        r = await self.env.COUNTER.getByName("a").fetch("http://do/")
        return Response(json.dumps({"caller": c.to_py() if c else None, "do": await r.text()}))
`),
	}
}

// TestPythonWorkerSandboxed (issue #292): a Python Worker deployed to a
// host runs under the host's process sandbox (Seatbelt on macOS, Landlock
// on Linux), with Pyodide read-only from the host's bundle dir and no
// network: a tailnet caller reaches it, its identity is the verified one,
// and its Durable Object keeps state.
func TestPythonWorkerSandboxed(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("short")
	}
	sb, _ := hostd.ResolveSandbox(hostd.SandboxAuto)
	if sb == hostd.SandboxNone {
		t.Skip("no process sandbox on this host")
	}
	pd, err := pyodide.UserDir()
	if err != nil {
		t.Skip(err)
	}
	if err := pyodide.Ensure(context.Background(), pd, ""); err != nil {
		t.Skipf("no Pyodide bundles (offline?): %v", err)
	}
	ctl, err := nettest.StartControl()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctl.Close)
	e := hostdtest.Start(t, hostdtest.Options{Fake: fakemgmt.Config{StaticKey: nettest.DummyKey}, Ephemeral: true,
		Sandbox: hostd.SandboxAuto, InsecureSharedHost: true, PyodideDir: pd,
		Tenant: mgmtapi.Tenant{Tailnet: "a.ts.net", ControlURL: ctl.URL, MintMode: "static"}})
	wl := e.WaitActive("py", e.DeployFiles(pythonBundle(), "py"), 90*time.Second)
	for _, w := range e.Host.Status().Workloads {
		if w.Workload == "py" && w.SandboxProfile == "" {
			t.Fatalf("no sandbox profile: %+v", w)
		}
	}
	client, err := nettest.NewNode(context.Background(), ctl.URL, nettest.DummyKey, "ttest-client", filepath.Join(e.Dir, "client"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	waitPeerOf(t, client, wl.IP)
	hc := client.HTTPClient(60 * time.Second)
	for i, want := range []string{"1", "2"} {
		resp, err := hc.Get("http://" + wl.IP + "/")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var r struct {
			Caller map[string]any `json:"caller"`
			DO     string         `json:"do"`
		}
		if err := json.Unmarshal(b, &r); err != nil || resp.StatusCode != 200 {
			t.Fatalf("GET %d: %d %s", i, resp.StatusCode, b)
		}
		if r.DO != want || r.Caller == nil || !strings.HasPrefix(r.Caller["hostname"].(string), "ttest-client") {
			t.Fatalf("GET %d: %s", i, b)
		}
	}
}

// TestPythonMicroVMRefused: a Python Worker that resolves to the microVM
// tier (the tenant's default) is refused, not run in the process tier.
func TestPythonMicroVMRefused(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("short")
	}
	ctl, err := nettest.StartControl()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctl.Close)
	e := hostdtest.Start(t, hostdtest.Options{Fake: fakemgmt.Config{StaticKey: nettest.DummyKey}, Ephemeral: true,
		PyodideDir: t.TempDir(),
		Tenant:     mgmtapi.Tenant{Tailnet: "a.ts.net", ControlURL: ctl.URL, MintMode: "static", Tier: "microvm"}})
	e.DeployFiles(pythonBundle(), "py")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		for _, w := range e.Host.Status().Workloads {
			if w.Workload != "py" {
				continue
			}
			if w.Status == "active" {
				t.Fatal("a Python Worker ran although its tier is microvm")
			}
			if w.Status == "failed" {
				if !strings.HasPrefix(w.Error, "tier_unavailable:") || !strings.Contains(w.Error, "Python Workers run on the process tier") {
					t.Fatalf("failed for another reason: %s", w.Error)
				}
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the deployment neither failed nor ran")
}
