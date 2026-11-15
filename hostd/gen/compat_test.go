package gen_test

import (
	"strings"
	"testing"

	"github.com/rosskukulinski/twd/hostd/hostdtest"
)

// TestRuntimeCompat measures the Workers runtime surfaces listed in
// docs/COMPAT.md ("Runtime" and "Durable Objects") on the generated
// platform + stock workerd: in the Worker, in a facet-mode object and in a
// plain-DO object. When a result changes, update docs/COMPAT.md.
func TestRuntimeCompat(t *testing.T) {
	in := start(t, hostdtest.Fixture("compat"), nil)
	where := map[string]map[string]any{
		"Worker": wl,
		"Facet":  obj("Facet", "f1"),
		"Plain":  obj("Plain", "p1"),
	}

	t.Run("NodejsCompat", func(t *testing.T) {
		for w, twd := range where {
			code, _, r := in.get("/node", twd)
			if code != 200 || r["base64"] != "dHdk" || r["uuid"] != true || r["als"] != "ctx-1" || r["emitted"] != float64(42) ||
				r["sha256"] != "684d49d53963cec93bc0c0056995be45d6f99c8ec9976bb80ce03515bb921668" {
				t.Fatalf("%s: %d %v", w, code, r)
			}
		}
	})

	t.Run("DOStorageKVAndSQL", func(t *testing.T) {
		for _, w := range []string{"Facet", "Plain"} {
			code, _, r := in.get("/storage", where[w])
			if code != 200 || jsonOf(r["got"]) != `{"n":3}` || jsonOf(r["listed"]) != `["a","b","c","d"]` ||
				r["afterDelete"] != nil || r["sqlSum"] != float64(3) || r["sqlBytes"] != true {
				t.Fatalf("%s: %d %v", w, code, r)
			}
		}
	})

	// connect() is not built: the egress entrypoints (the Worker's
	// globalOutbound and the facets' Egress) carry HTTP only, so workerd
	// refuses the socket. Raw TCP egress would need netd support
	// (ctx.tailnet.connect() is a typed plan_feature_missing stub).
	t.Run("ConnectTCPRefused", func(t *testing.T) {
		for w, twd := range where {
			code, _, r := in.get("/connect?host=echo&port=80", twd)
			if code != 200 || r["ok"] != false || !strings.Contains(fmtAny(r["error"]), "cannot connect") {
				t.Fatalf("%s: connect() changed (update docs/COMPAT.md): %d %v", w, code, r)
			}
		}
	})
}

// addWasm is a minimal module exporting add(i32, i32) i32.
var addWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, // magic, version
	0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f, // type: (i32 i32) -> i32
	0x03, 0x02, 0x01, 0x00, // func 0 has type 0
	0x07, 0x07, 0x01, 0x03, 'a', 'd', 'd', 0x00, 0x00, // export "add" = func 0
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b, // local.get 0, local.get 1, i32.add
}

// TestRuntimeCompatWasm: a bundle that ships a .wasm module next to its JS
// (wrangler's CompiledWasm rule) runs in the Worker, a facet object and a
// plain-DO object. twd's own bundler (twd deploy) has no .wasm loader, so
// the bundle is assembled by hand here; see docs/COMPAT.md "wasm".
func TestRuntimeCompatWasm(t *testing.T) {
	const js = `import { DurableObject } from "cloudflare:workers";
import add from "./add.wasm";
const sum = () => Response.json({ sum: new WebAssembly.Instance(add).exports.add(2, 3) });
export class Facet extends DurableObject { fetch() { return sum(); } }
export class Plain extends DurableObject { fetch() { return sum(); } }
export default { fetch() { return sum(); } };
`
	files := map[string][]byte{
		"wrangler.jsonc": []byte(`{"name": "wasm", "main": "index.js", "compatibility_date": "2026-04-01",
  "durable_objects": {"bindings": [{"name": "FACET", "class_name": "Facet"}, {"name": "PLAIN", "class_name": "Plain"}]}}`),
		"twd.jsonc": []byte(`{"tailnet": {"objects": {"Plain": {"egress": "workload"}}}}`),
		"index.js":  []byte(js),
		"add.wasm":  addWasm,
	}
	in := startFiles(t, files)
	for w, twd := range map[string]map[string]any{"Worker": wl, "Facet": obj("Facet", "w1"), "Plain": obj("Plain", "w2")} {
		code, _, r := in.get("/", twd)
		if code != 200 || r["sum"] != float64(5) {
			t.Fatalf("%s: %d %v", w, code, r)
		}
	}
}
