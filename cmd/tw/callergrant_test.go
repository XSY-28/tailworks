package main

import (
	"strings"
	"testing"

	"github.com/rosskukulinski/tailworks/mgmt"
)

func TestCallerGrant(t *testing.T) {
	for _, c := range []struct {
		tags []string
		want string
	}{
		{nil, ""},
		{[]string{"tag:tw-wl-support"}, `{ "src": ["autogroup:member"], "dst": ["tag:tw-wl-support"], "ip": ["tcp:80"] }`},
		{[]string{"tag:a", "tag:b"}, `{ "src": ["autogroup:member"], "dst": ["tag:a", "tag:b"], "ip": ["tcp:80"] }`},
	} {
		if got := callerGrant(c.tags); got != c.want {
			t.Errorf("callerGrant(%q)\n got %s\nwant %s", c.tags, got, c.want)
		}
	}
}

// TestServiceCallerGrant (#155): a workload behind a Service also gets the
// grant to its svc: name, where callers' app caps must go.
func TestServiceCallerGrant(t *testing.T) {
	if got := serviceCallerGrant(""); got != "" {
		t.Errorf("no Service: %q", got)
	}
	want := `{ "src": ["autogroup:member"], "dst": ["svc:support"], "ip": ["tcp:80"] }`
	if got := serviceCallerGrant("svc:support"); got != want {
		t.Errorf("serviceCallerGrant\n got %s\nwant %s", got, want)
	}
	pf := mgmt.PreflightResult{Service: "svc:support"}
	if s := policySnippet(pf, callerGrant([]string{"tag:tw-wl-support"}), want); !strings.Contains(s, want+", // calls through svc:support") {
		t.Errorf("policy snippet lacks the Service grant:\n%s", s)
	}
}
