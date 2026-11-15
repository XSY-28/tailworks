package gen

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/rosskukulinski/twd/hostd/bundle"
)

// Platform stores (ADR-platform-stores): every storage binding of the app
// (bundle.App.Stores) is backed by the platform's `Store` Durable Object
// class (js/store.js) in the platform worker, one object per (kind, name).
//
// Each kind registers a StoreKindGen: the workerd binding type its tenant
// binding uses (workerd's own `kvNamespace`, `queue`, `r2Bucket`, … so
// tenant code gets upstream's API, or a plain `service`), the backend
// entrypoint that binding targets (with props {kind, name}), and its
// platform module. Facets (Dynamic Workers) can't be handed workerd's
// native bindings, so a kind with a native binding also names a facet
// proxy entrypoint: the facet's env.<Binding> is an RPC stub to it, and it
// forwards each call to a native binding of the platform worker
// (`__TWD_STORE_<Binding>`), so facets get the same semantics.

//go:embed js/store.js
var storeJS string

// StoreKindGen is how a store kind is generated.
type StoreKindGen struct {
	Kind string
	// Binding is the workerd binding type of env.<Binding> in the tenant
	// worker: "kvNamespace", "queue", "r2Bucket" or "service".
	Binding string
	// Backend is the platform entrypoint class the binding targets.
	Backend string
	// FacetProxy is the platform entrypoint class a facet's env gets
	// (props {"binding": name}); it calls env["__TWD_STORE_" + binding].
	// Empty: facets get Backend itself (only for Binding "service").
	FacetProxy string
	// JS is the kind's platform module (twd-store-<kind>.js). It imports
	// from "twd-store.js" and exports Backend (and FacetProxy).
	JS string
}

var (
	storeGenMu sync.RWMutex
	storeGens  = map[string]StoreKindGen{}
	classRE    = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
)

// RegisterStoreKind registers how a store kind is generated. It panics on
// a duplicate or malformed kind (a programming error).
func RegisterStoreKind(k StoreKindGen) {
	storeGenMu.Lock()
	defer storeGenMu.Unlock()
	switch k.Binding {
	case "kvNamespace", "queue", "r2Bucket":
		if !classRE.MatchString(k.FacetProxy) {
			panic("gen: store kind " + k.Kind + ": a native binding needs a FacetProxy")
		}
	case "service":
		if k.FacetProxy != "" && !classRE.MatchString(k.FacetProxy) {
			panic("gen: store kind " + k.Kind + ": bad FacetProxy")
		}
	default:
		panic("gen: store kind " + k.Kind + ": bad binding type " + k.Binding)
	}
	if !classRE.MatchString(k.Backend) || k.JS == "" {
		panic("gen: store kind " + k.Kind + ": bad backend or no JS")
	}
	if _, dup := storeGens[k.Kind]; dup {
		panic("gen: store kind registered twice: " + k.Kind)
	}
	storeGens[k.Kind] = k
}

func storeGen(kind string) (StoreKindGen, bool) {
	storeGenMu.RLock()
	defer storeGenMu.RUnlock()
	k, ok := storeGens[kind]
	return k, ok
}

// storeCfg is one entry of twd-config.json "stores": what a facet's env
// gets for a binding.
type storeCfg struct {
	Binding string         `json:"binding"`
	Kind    string         `json:"kind"`
	Name    string         `json:"name"`
	Facet   string         `json:"facet"` // platform entrypoint class
	Props   map[string]any `json:"props"`
}

// checkStores refuses a binding of an unknown kind, and a secret with a
// storage binding's name.
func checkStores(app *bundle.App) error {
	for _, b := range app.Stores {
		if _, ok := storeGen(b.Kind); !ok {
			return fmt.Errorf("gen: storage binding %s: kind %q is not supported by this hostd", b.Binding, b.Kind)
		}
		if _, ok := app.Secrets[b.Binding]; ok {
			return fmt.Errorf("gen: secret %q has the name of a storage binding; rename one (twd secret rm %s)", b.Binding, b.Binding)
		}
	}
	return nil
}

// storeKinds are the kinds app uses, sorted.
func storeKinds(app *bundle.App) []StoreKindGen {
	seen := map[string]bool{}
	var out []StoreKindGen
	for _, b := range app.Stores {
		if !seen[b.Kind] {
			seen[b.Kind] = true
			k, _ := storeGen(b.Kind)
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

func storeProps(b bundle.StoreBinding) string {
	p, _ := json.Marshal(map[string]string{"kind": b.Kind, "name": b.Name})
	return string(p)
}

// storeConfig is twd-config.json "stores".
func storeConfig(app *bundle.App) []storeCfg {
	out := []storeCfg{}
	for _, b := range app.Stores {
		k, _ := storeGen(b.Kind)
		c := storeCfg{Binding: b.Binding, Kind: b.Kind, Name: b.Name, Facet: k.Backend, Props: map[string]any{"kind": b.Kind, "name": b.Name}}
		if k.FacetProxy != "" {
			c.Facet, c.Props = k.FacetProxy, map[string]any{"binding": b.Binding}
		}
		out = append(out, c)
	}
	return out
}

// storeFiles are the platform modules for app's stores (none without).
func storeFiles(app *bundle.App) map[string]string {
	if len(app.Stores) == 0 {
		return nil
	}
	files := map[string]string{"twd-store.js": storeJS}
	var idx strings.Builder
	idx.WriteString("export { Store } from \"twd-store.js\";\n")
	for _, k := range storeKinds(app) {
		mod := "twd-store-" + k.Kind + ".js"
		files[mod] = k.JS
		cls := []string{k.Backend}
		if k.FacetProxy != "" && k.FacetProxy != k.Backend {
			cls = append(cls, k.FacetProxy)
		}
		fmt.Fprintf(&idx, "export { %s } from %s;\n", strings.Join(cls, ", "), jsq(mod))
	}
	files["twd-stores.js"] = idx.String()
	return files
}

// storePlatformExports replaces platform.js's //@@STORES@@ marker.
func storePlatformExports(app *bundle.App) string {
	if len(app.Stores) == 0 {
		return ""
	}
	return "export * from \"twd-stores.js\";\n"
}

// storeModules are the platform worker's module entries.
func storeModules(app *bundle.App) string {
	files := storeFiles(app)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "    (name = %s, esModule = embed %s),\n", cq(n), cq(n))
	}
	return b.String()
}

// storeNamespace is the platform's Store DO namespace entry.
func storeNamespace(app *bundle.App, workload string) string {
	if len(app.Stores) == 0 {
		return ""
	}
	return fmt.Sprintf("    (className = \"Store\", uniqueKey = %s, enableSql = true),\n", cq("twd-"+workload+"-__twd_store"))
}

func storeBinding(name string, k StoreKindGen, b bundle.StoreBinding) string {
	return fmt.Sprintf("    (name = %s, %s = (name = \"platform\", entrypoint = %s, props = (json = %s))),\n",
		cq(name), k.Binding, cq(k.Backend), cq(storeProps(b)))
}

// storePlatformBindings are the platform worker's native bindings that
// facet proxies forward to.
func storePlatformBindings(app *bundle.App) string {
	var s strings.Builder
	for _, b := range app.Stores {
		if k, _ := storeGen(b.Kind); k.FacetProxy != "" {
			s.WriteString(storeBinding("__TWD_STORE_"+b.Binding, k, b))
		}
	}
	return s.String()
}

// storeTenantBindings are the tenant worker's env.<Binding> entries.
func storeTenantBindings(app *bundle.App) string {
	var s strings.Builder
	for _, b := range app.Stores {
		k, _ := storeGen(b.Kind)
		s.WriteString(storeBinding(b.Binding, k, b))
	}
	return s.String()
}
