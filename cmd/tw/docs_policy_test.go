package main

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

// The policy snippets in README.md and docs/INSTALL.md are what admins copy
// into their tailnet policy, so they must say exactly what tw up checks:
// operatorSnippet. They drifted once (tag:tw-issuer in the OAuth one).
// The README also has the one line to paste before tw up --apply-policy
// writes the rest (issue #320): tag:tw alone, as operatorSnippet has it.
func TestDocPolicySnippetsMatchCode(t *testing.T) {
	t.Parallel()
	block := regexp.MustCompile("(?s)```jsonc\n(.*?)```")
	n, first := 0, 0
	for _, f := range []string{"../../README.md", "../../docs/INSTALL.md"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range block.FindAllSubmatch(b, -1) {
			body := string(m[1])
			if !strings.Contains(body, `"tagOwners"`) {
				continue
			}
			n++
			std, err := hujson.Standardize([]byte("{" + body + "}"))
			if err != nil {
				t.Errorf("%s: snippet doesn't parse: %v\n%s", f, err, body)
				continue
			}
			var got struct {
				TagOwners map[string][]string `json:"tagOwners"`
				Grants    []any               `json:"grants"`
				NodeAttrs []any               `json:"nodeAttrs"`
			}
			if err := json.Unmarshal(std, &got); err != nil {
				t.Fatal(err)
			}
			want, which := operatorSnippet(), "operatorSnippet"
			if len(got.TagOwners) == 1 && len(got.Grants) == 0 && reflect.DeepEqual(got.TagOwners["tag:tw"], want.TagOwners["tag:tw"]) {
				n, first = n-1, first+1
				continue
			}
			if len(got.NodeAttrs) > 0 {
				t.Errorf("%s: snippet has nodeAttrs %v; the operator snippet has none (mgmt never uses Funnel)", f, got.NodeAttrs)
			}
			if !reflect.DeepEqual(got.TagOwners, want.TagOwners) {
				t.Errorf("%s: tagOwners = %v, want %s's %v", f, got.TagOwners, which, want.TagOwners)
			}
			wb, _ := json.Marshal(want.Grants)
			var wg []any
			json.Unmarshal(wb, &wg)
			if !reflect.DeepEqual(got.Grants, wg) {
				t.Errorf("%s: grants = %v, want %s's %v", f, got.Grants, which, wg)
			}
		}
	}
	if n != 2 || first != 1 {
		t.Errorf("found %d policy snippets and %d tag:tw-only ones in README.md + INSTALL.md, want 2 and 1 (update this test if you add one)", n, first)
	}
}
