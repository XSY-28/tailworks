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

func TestRequiredWorkerInternet(t *testing.T) {
	// The boolean form of tailnet.egress.internet needs a host that parses
	// it; the list form (which older hosts read) and absence need nothing.
	for cfg, want := range map[string]bool{
		`{"tailnet":{"tags":["tag:app"]}}`:                                     false,
		`{"tailnet":{"tags":["tag:app"],"egress":{"internet":["a.example"]}}}`: false,
		`{"tailnet":{"tags":["tag:app"],"egress":{"internet":[]}}}`:            false,
		`{"tailnet":{"tags":["tag:app"],"egress":{"internet":false}}}`:         true,
		`{"tailnet":{"tags":["tag:app"],"egress":{"internet":true}}}`:          true,
	} {
		b, err := bundle.Pack(map[string][]byte{
			"wrangler.jsonc":  []byte(`{"name":"app","main":"index.js","compatibility_date":"2026-04-01"}`),
			"index.js":        []byte(`export default { fetch() { return new Response("ok") } }`),
			"tailworks.jsonc": []byte(cfg),
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := slices.Contains(RequiredByBundle(b), WorkerInternet); got != want {
			t.Errorf("%s: requires worker-internet = %v, want %v", cfg, got, want)
		}
	}
	if !slices.Contains(Host(), WorkerInternet) {
		t.Error("hosts must advertise worker-internet")
	}
}
