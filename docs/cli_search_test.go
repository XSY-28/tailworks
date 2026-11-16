package docs

import "testing"

// TestSearchFindsFlags: a flag name finds the generated CLI reference
// (cli.md, `make cli-docs`) in the top hits.
func TestSearchFindsFlags(t *testing.T) {
	for _, q := range []string{
		"wait-public",
		"allow-missing-grants",
	} {
		found := false
		var got []string
		for _, h := range Search(q, 5) {
			found = found || h.Doc == "cli.md"
			got = append(got, h.Ref())
		}
		if !found {
			t.Errorf("search %q: want cli.md in the top 5, got %v", q, got)
		}
	}
}
