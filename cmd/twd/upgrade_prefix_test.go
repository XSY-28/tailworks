package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkPrefix makes an install.sh-shaped directory (VERSION, share/twd, bin/twd).
func mkPrefix(t *testing.T, dir, ver string) string {
	t.Helper()
	for _, d := range []string{filepath.Join(dir, "share", "twd"), filepath.Join(dir, "bin")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte(ver+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "twd"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestServiceBinary(t *testing.T) {
	for _, tc := range []struct{ name, def, want string }{
		{"plist", plistFor(&state{Dir: "/Users/u/.twd"}, "/Users/u/.twd/bin/twd"), "/Users/u/.twd/bin/twd"},
		{"plist escaped", plistFor(&state{Dir: "/Users/u/.twd"}, "/opt/a & b/bin/twd"), "/opt/a & b/bin/twd"},
		{"unit", unitFor(&state{Dir: "/home/u/.twd"}, "/home/u/.twd/bin/twd"), "/home/u/.twd/bin/twd"},
		{"unit quoted", unitFor(&state{Dir: "/home/u/my twd"}, `/opt/t "w"/bin/twd`), `/opt/t "w"/bin/twd`},
		{"neither", "garbage", ""},
	} {
		if got := serviceBinary(tc.def); got != tc.want {
			t.Errorf("%s: serviceBinary = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The issue #63 case: the new CLI runs from the extracted release (which
// looks like a prefix) while ~/.twd is the real install.
func TestResolvePrefixExtractedRelease(t *testing.T) {
	root := t.TempDir()
	home := mkPrefix(t, filepath.Join(root, "home", ".twd"), "0.1.0")
	extracted := mkPrefix(t, filepath.Join(root, "tmp", "twd-0.2.0-darwin-arm64"), "0.2.0")
	s := &state{Dir: home}

	cands := statePrefixCandidates(s) // no service or supervisor for a temp dir: the state dir itself
	if len(cands) != 1 || cands[0].Prefix != home {
		t.Fatalf("candidates = %+v, want the state dir", cands)
	}
	pc, err := resolvePrefix("", extracted, cands)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(pc.Prefix, home) {
		t.Fatalf("prefix = %q, want the state dir's install %q", pc.Prefix, home)
	}
	if pc.Mismatch == "" || !strings.Contains(pc.Mismatch, extracted) || !strings.Contains(pc.Mismatch, home) {
		t.Fatalf("mismatch = %q, want it to name both prefixes", pc.Mismatch)
	}
	if v := installedVersion(pc.Prefix); v != "0.1.0" {
		t.Fatalf("installed version read from %s = %q", pc.Prefix, v)
	}

	// --prefix overrides, and is not a mismatch.
	pc, err = resolvePrefix(home, extracted, cands)
	if err != nil || pc.Mismatch != "" || !samePath(pc.Prefix, home) || pc.From != "--prefix" {
		t.Fatalf("--prefix: %+v, %v", pc, err)
	}
	// --prefix must be an install.
	if _, err := resolvePrefix(filepath.Join(root, "nope"), extracted, cands); err == nil {
		t.Fatal("--prefix of a non-install was accepted")
	}
	// A service binary in the prefix wins over the state dir, and names it.
	pc, _ = resolvePrefix("", extracted, append([]prefixCandidate{{Prefix: prefixOfBinary(filepath.Join(home, "bin", "twd")), From: "the LaunchAgent runs it"}}, cands...))
	if !samePath(pc.Prefix, home) || !strings.Contains(pc.Mismatch, "LaunchAgent") {
		t.Fatalf("service candidate: %+v", pc)
	}
}

func TestResolvePrefixSame(t *testing.T) {
	home := mkPrefix(t, filepath.Join(t.TempDir(), ".twd"), "0.2.0")
	pc, err := resolvePrefix("", home, []prefixCandidate{{}, {Prefix: home, From: "the state dir is the install prefix"}})
	if err != nil || pc.Mismatch != "" || pc.Prefix != home {
		t.Fatalf("running the installed twd: %+v, %v", pc, err)
	}
	// Nothing owns the state dir (an install elsewhere, twd not running):
	// the binary's own prefix, as before.
	pc, err = resolvePrefix("", home, nil)
	if err != nil || pc.Mismatch != "" || pc.Prefix != home {
		t.Fatalf("no candidates: %+v, %v", pc, err)
	}
	// A dev build (no prefix) against an installed state dir: a mismatch.
	pc, err = resolvePrefix("", "", []prefixCandidate{{Prefix: home, From: "x"}})
	if err != nil || pc.Mismatch == "" || pc.Prefix != home {
		t.Fatalf("dev build: %+v, %v", pc, err)
	}
	// Nothing at all.
	if pc, err := resolvePrefix("", "", nil); err != nil || pc.Prefix != "" {
		t.Fatalf("nothing: %+v, %v", pc, err)
	}
}

func TestPrefixOfBinary(t *testing.T) {
	home := mkPrefix(t, filepath.Join(t.TempDir(), ".twd"), "0.2.0")
	if got := prefixOfBinary(filepath.Join(home, "bin", "twd")); !samePath(got, home) {
		t.Fatalf("prefixOfBinary = %q, want %q", got, home)
	}
	// After install.sh swaps releases, an old process may run from bin.prev.
	if got := prefixOfBinary(filepath.Join(home, "bin.prev", "twd")); !samePath(got, home) {
		t.Fatalf("bin.prev: prefixOfBinary = %q, want %q", got, home)
	}
	if got := prefixOfBinary(filepath.Join(t.TempDir(), "bin", "twd")); got != "" {
		t.Fatalf("not a prefix: %q", got)
	}
	if got := prefixOfBinary(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
}
