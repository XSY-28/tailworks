package bundle

import (
	"slices"
	"testing"
)

func TestInternetEgress(t *testing.T) {
	for _, c := range []struct {
		name, jsonc string
		workerOff   bool
		workerAllow []string
		containers  []string
	}{
		{"absent", `{"tailnet": {}}`, false, nil, nil},
		{"true", `{"tailnet": {"egress": {"internet": true}}}`, false, nil, []string{"*", "0.0.0.0/0", "::/0"}},
		{"false", `{"tailnet": {"egress": {"internet": false}}}`, true, nil, nil},
		{"empty", `{"tailnet": {"egress": {"internet": []}}}`, true, []string{}, []string{}},
		{"list", `{"tailnet": {"egress": {"internet": ["api.example.com", "*.github.com"]}}}`, false,
			[]string{"api.example.com", "*.github.com"}, []string{"api.example.com", "*.github.com"}},
	} {
		var tc twConfig
		if err := UnmarshalJSONC([]byte(c.jsonc), &tc); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		ie := tc.Tailnet.Egress.Internet
		if ie.WorkerOff() != c.workerOff || !slices.Equal(ie.WorkerAllow(), c.workerAllow) || !slices.Equal(ie.Containers(), c.containers) {
			t.Errorf("%s: off=%v allow=%v containers=%v", c.name, ie.WorkerOff(), ie.WorkerAllow(), ie.Containers())
		}
	}
	var tc twConfig
	if err := UnmarshalJSONC([]byte(`{"tailnet": {"egress": {"internet": "x"}}}`), &tc); err == nil {
		t.Error("a string must be refused")
	}
}
