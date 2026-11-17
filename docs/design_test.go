package docs

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestADRIndex: every row of docs/design/adr/README.md links an ADR file
// that exists, every numbered ADR file has a row, rows are in number order,
// and, in CI, no ADR file still carries a slug instead of a number
// (AGENTS.md rule 5: a slug is only for a branch, so the PR that merges it
// numbers it).
func TestADRIndex(t *testing.T) {
	const dir = "design/adr/"
	b, err := os.ReadFile(dir + "README.md")
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile(`(?m)^\| \[([^\]]+)\]\(([^)]+)\) \|`)
	numbered := regexp.MustCompile(`^(\d{3})-[a-z0-9-]+\.md$`)
	rows := map[string]bool{}
	last := ""
	for _, m := range row.FindAllStringSubmatch(string(b), -1) {
		num, file := m[1], m[2]
		fm := numbered.FindStringSubmatch(file)
		if fm == nil || fm[1] != num {
			t.Errorf("index row %q links %q: want a file named %s-<slug>.md", num, file, num)
		}
		if num <= last {
			t.Errorf("index row %s comes after %s: keep rows in number order", num, last)
		}
		last = num
		if _, err := os.Stat(dir + file); err != nil {
			t.Errorf("index row %s: %v", num, err)
		}
		rows[file] = true
	}
	if len(rows) < 40 {
		t.Fatalf("found only %d index rows", len(rows))
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		n := e.Name()
		if n == "README.md" || !strings.HasSuffix(n, ".md") {
			continue
		}
		if !numbered.MatchString(n) {
			// A slug draft is fine on a branch; the PR that merges it
			// to master numbers it, and CI (CI=true) enforces that.
			report := t.Logf
			if os.Getenv("CI") != "" {
				report = t.Errorf
			}
			report("%s%s has no number: number it in the PR that merges it to master (AGENTS.md rule 5)", dir, n)
			continue
		}
		if !rows[n] {
			t.Errorf("%s%s has no row in %sREADME.md", dir, n, dir)
		}
		h, err := os.ReadFile(dir + n)
		if err != nil {
			t.Fatal(err)
		}
		if want := "# ADR-" + n[:3] + ":"; !strings.HasPrefix(string(h), want) {
			t.Errorf("%s%s: first line should start %q", dir, n, want)
		}
	}
}
