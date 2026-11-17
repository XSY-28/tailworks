package features

import (
	"slices"
	"testing"

	"github.com/rosskukulinski/tailworks/hostd/bundle"
)

func TestHostListsStoreKinds(t *testing.T) {
	h := Host()
	for _, want := range []string{"d1", "kv", "r2", Queues, Telemetry} {
		if !slices.Contains(h, want) {
			t.Errorf("Host() = %v, lacks %q", h, want)
		}
	}
	if !slices.IsSorted(h) {
		t.Errorf("Host() not sorted: %v", h)
	}
}

func TestRequired(t *testing.T) {
	files := map[string][]byte{
		"wrangler.jsonc": []byte(`{"name":"app","main":"index.js","compatibility_date":"2026-04-01",
			"kv_namespaces":[{"binding":"CACHE","id":"cache"}],
			"r2_buckets":[{"binding":"B","bucket_name":"b"}]}`),
		"index.js":        []byte(`export default { fetch() { return new Response("ok") } }`),
		"tailworks.jsonc": []byte(`{"tailnet":{"tags":["tag:app"]}}`),
	}
	b, err := bundle.Pack(files)
	if err != nil {
		t.Fatal(err)
	}
	got := RequiredByBundle(b)
	if !slices.Equal(got, []string{"kv", "r2"}) {
		t.Fatalf("RequiredByBundle = %v", got)
	}
	if m := Missing(got, []string{"kv"}); !slices.Equal(m, []string{"r2"}) {
		t.Fatalf("Missing = %v", m)
	}
}
