package main

import (
	"strings"
	"testing"

	"github.com/rosskukulinski/tailworks/mgmt"
)

// A connects target that is another tailworks app's undefined tag: the
// snippet tw policy prints (and deploy's failure fix) defines it, so pasting
// it isn't rejected for a grant to an undefined tag.
func TestPolicySnippetDefinesConnectsTag(t *testing.T) {
	pf := mgmt.PreflightResult{
		MissingTagOwners:  []string{},
		UndefinedConnects: []string{"tag:tw-wl-mcp"},
		TagOwnersSnippet:  `"tagOwners": { "tag:tw-wl-mcp": ["autogroup:admin", "tag:tw"] }`,
		Missing:           []mgmt.PreflightMissing{{Src: "tag:tw-wl-app", Dst: "tag:tw-wl-mcp", Snippet: `{ "src": ["tag:tw-wl-app"], "dst": ["tag:tw-wl-mcp"], "ip": ["tcp:80"] }`}},
	}
	if s := policySnippet(pf, "", ""); !strings.Contains(s, `"tag:tw-wl-mcp": ["autogroup:admin", "tag:tw"]`) {
		t.Fatalf("snippet:\n%s", s)
	}
	if fix := strings.Join(preflightFailure(pf).Fix, "\n"); !strings.Contains(fix, `"tag:tw-wl-mcp": ["autogroup:admin", "tag:tw"]`) {
		t.Fatalf("fix:\n%s", fix)
	}
}
