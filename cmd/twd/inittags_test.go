package main

import (
	"strings"
	"testing"
)

func TestAppTags(t *testing.T) {
	in := `{
  "name": "agent",
  "tailnet": {
    "tags": ["tag:twd-wl-agent"],
    "connects": ["tag:db"],
    "objects": {
      // a comment survives
      "SupportAgent": { "tags": ["tag:twd-agent-support"], "agent": true },
      "LookupAgent":  { "tags": ["tag:twd-agent-support-sub"] },
      "Mine":         { "tags": ["tag:custom"] }
    }
  }
}`
	out, ren, err := appTags([]byte(in), "billing")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		`"tags": ["tag:twd-wl-billing"]`,
		`"SupportAgent": { "tags": ["tag:twd-billing-support-agent"]`,
		`"LookupAgent":  { "tags": ["tag:twd-billing-lookup-agent"]`,
		`"tags": ["tag:custom"]`, // not a template tag: kept
		`"connects": ["tag:db"]`, // dependencies are never renamed
		"// a comment survives",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	if len(ren) != 3 {
		t.Errorf("renamed %v, want 3 tags", ren)
	}
	if strings.Contains(s, "tag:twd-agent-support") {
		t.Errorf("old tag left in\n%s", s)
	}
}

func TestClassLabel(t *testing.T) {
	for in, want := range map[string]string{"SupportAgent": "support-agent", "MCPServer": "mcpserver", "budget": "budget", "AiAgent": "ai-agent"} {
		if got := classLabel(in); got != want {
			t.Errorf("classLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
