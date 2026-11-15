package main_test

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rosskukulinski/twd/e2e/cliflow"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/*.golden")

// TestAgentSetupSnapshot snapshots `twd agent setup` for a fake home with
// five agents' config dirs (one with an AGENTS.md of its own, one Claude
// skill the user wrote): the dry run (user and project scope), that it
// writes nothing, that setup is idempotent, and that remove restores the
// user's files byte for byte. Run with -update to rewrite the golden file.
func TestAgentSetupSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	bin := cliflow.BuildBinaries(t)
	// Resolved: macOS's temp dir is behind a symlink (/var → /private/var),
	// and the snapshot must not depend on it.
	home, _ := filepath.EvalSymlinks(t.TempDir())
	proj, _ := filepath.EvalSymlinks(t.TempDir())
	for _, d := range []string{".claude/skills/twd-mine", ".codex", ".cursor", ".gemini", ".config/opencode"} {
		os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	userAgents := "# my global rules\n\nAlways run the tests.\n"
	mine := "---\nname: twd-mine\ndescription: the user's own skill\n---\n\nnot twd's\n"
	os.WriteFile(filepath.Join(home, ".codex", "AGENTS.md"), []byte(userAgents), 0o644)
	os.WriteFile(filepath.Join(home, ".claude/skills/twd-mine/SKILL.md"), []byte(mine), 0o644)
	os.WriteFile(filepath.Join(proj, "AGENTS.md"), []byte("# project\n"), 0o644)

	var snap strings.Builder
	twd := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(filepath.Join(bin, "twd"), args...)
		cmd.Env = []string{"HOME=" + home, "PATH=" + t.TempDir()} // no agent binaries on PATH
		cmd.Dir = proj
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("twd %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		s := strings.ReplaceAll(string(out), proj, "$PROJECT")
		snap.WriteString("$ twd " + strings.Join(args, " ") + "\n" + s + "\n")
		return s
	}
	files := func() string {
		var out []string
		filepath.Walk(home, func(p string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() {
				out = append(out, strings.TrimPrefix(p, home))
			}
			return nil
		})
		return strings.Join(out, "\n")
	}
	before := files()
	twd("agent", "setup", "--dry-run")
	twd("agent", "setup", "--dry-run", "--scope", "project", "--agent", "claude,codex,cursor,opencode")
	if files() != before {
		t.Fatalf("--dry-run wrote files:\n%s", files())
	}
	twd("agent", "setup", "--yes")
	if out := twd("agent", "setup", "--dry-run"); !strings.Contains(out, "already up to date") || strings.Contains(out, "create ") || strings.Contains(out, "update ") {
		t.Fatalf("setup is not idempotent:\n%s", out)
	}
	twd("agent", "remove", "--dry-run")
	twd("agent", "remove", "--yes")
	if b, _ := os.ReadFile(filepath.Join(home, ".codex", "AGENTS.md")); string(b) != userAgents {
		t.Fatalf("remove changed the user's AGENTS.md:\n%q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".claude/skills/twd-mine/SKILL.md")); string(b) != mine {
		t.Fatal("remove touched a skill twd didn't write")
	}
	if files() != before {
		t.Fatalf("after remove, files differ from before setup:\n%s\nwant:\n%s", files(), before)
	}

	golden := filepath.Join("testdata", "agent-setup.golden")
	if *updateGolden {
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(golden, []byte(snap.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if snap.String() != string(want) {
		t.Fatalf("twd agent setup output changed (run go test ./cmd/twd -run TestAgentSetupSnapshot -update, and review the diff):\n%s", snap.String())
	}
}
