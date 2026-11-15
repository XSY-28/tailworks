package gen_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rosskukulinski/twd/hostd/bundle"
	"github.com/rosskukulinski/twd/hostd/gen"
)

// TestGenerateD1: a d1_databases binding is workerd's own D1 API (a
// wrapped binding of cloudflare-internal:d1-api) whose fetcher is the
// platform's D1Backend with props naming the database; facets get a stub
// to D1Backend that the facet runtime wraps ("d1").
func TestGenerateD1(t *testing.T) {
	app := &bundle.App{
		Name: "w", Main: "index.js", CompatDate: "2026-04-01",
		Modules: map[string][]byte{"index.js": []byte("export default { fetch() { return new Response('ok'); } };")},
		Classes: map[string]*bundle.Class{},
		Stores:  []bundle.StoreBinding{{Kind: "d1", Binding: "DB", Name: "prod-db"}},
	}
	dir := filepath.Join(t.TempDir(), "i")
	if _, err := gen.Generate(app, gen.Options{Dir: dir, DOStorage: filepath.Join(dir, "do"), DeploymentID: "dp_1", Workload: "w"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, "config.capnp"))
	c := string(cfg)
	for _, want := range []string{
		`(name = "DB", wrapped = (moduleName = "cloudflare-internal:d1-api", innerBindings = [(name = "fetcher", service = (name = "platform", entrypoint = "D1Backend", props = (json = "{\"kind\":\"d1\",\"name\":\"prod-db\"}")))]))`,
		`(name = "twd-store-d1.js", esModule = embed "twd-store-d1.js")`,
		`(className = "Store", uniqueKey = "twd-w-__twd_store", enableSql = true)`,
	} {
		if !strings.Contains(c, want) {
			t.Errorf("config.capnp lacks %s", want)
		}
	}
	if strings.Contains(c, "__TWD_STORE_") {
		t.Error("D1 needs no platform-side native binding")
	}
	idx, _ := os.ReadFile(filepath.Join(dir, "twd-stores.js"))
	if !strings.Contains(string(idx), `export { D1Backend } from "twd-store-d1.js";`) {
		t.Errorf("twd-stores.js: %s", idx)
	}
	var tc struct {
		Stores []struct {
			Binding, Facet, Wrap string
			Props                map[string]string
		} `json:"stores"`
	}
	b, _ := os.ReadFile(filepath.Join(dir, "twd-config.json"))
	if err := json.Unmarshal(b, &tc); err != nil || len(tc.Stores) != 1 || tc.Stores[0].Facet != "D1Backend" || tc.Stores[0].Wrap != "d1" || tc.Stores[0].Props["name"] != "prod-db" {
		t.Fatalf("twd-config.json stores: %+v %v", tc.Stores, err)
	}
}
