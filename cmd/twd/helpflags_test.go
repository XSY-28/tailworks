package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `twd <cmd> --help` lists the command's flags with their descriptions,
// and help mode stops at flag parsing: nothing the command would do runs.
func TestHelpListsFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"deploy", "--help"}, []string{"flags:", "--allow-missing-grants", "deploy even if tailnet.connects grants are missing", "--wait <duration>", "(default 3m0s)"}},
		{[]string{"init", "x", "--template", "agent", "--help"}, []string{"--keep-tags", "--template <string>", "(default hello)"}},
		{[]string{"host", "setup", "--help"}, []string{"--undo  ", "--hugepages <int>"}},
		{[]string{"logs", "-h"}, []string{"-n <int>", "(default 50)"}},
	} {
		var so, se bytes.Buffer
		if code := run(tc.args, &so, &se); code != 0 {
			t.Fatalf("twd %s: exit %d: %s", strings.Join(tc.args, " "), code, se.String())
		}
		out := so.String()
		for _, w := range tc.want {
			if !strings.Contains(out, w) {
				t.Errorf("twd %s: help lacks %q:\n%s", strings.Join(tc.args, " "), w, out)
			}
		}
		if strings.Contains(out, "--state-dir <") || strings.Contains(out, "--json ") {
			t.Errorf("twd %s: lists the global flags:\n%s", strings.Join(tc.args, " "), out)
		}
		if strings.Contains(out, "<twd ") {
			t.Errorf("twd %s: a backticked word became a type name:\n%s", strings.Join(tc.args, " "), out)
		}
	}
	// init in help mode must not create the directory.
	dir := filepath.Join(t.TempDir(), "app")
	var so, se bytes.Buffer
	run([]string{"init", dir, "--template", "hello", "--help"}, &so, &se)
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("twd init --help created %s", dir)
	}
	if helpMode {
		t.Fatal("help mode left on")
	}
}
