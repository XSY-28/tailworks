package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestHelpGroups: `twd help` lists every command exactly once, in a named
// group (none falls through to "Other"), with a short line; no line is
// long; `twd help --all` still has every full usage line.
func TestHelpGroups(t *testing.T) {
	titles, groups := groupedCommands()
	seen := map[string]int{}
	for i, title := range titles {
		if title == "Other" {
			var names []string
			for _, c := range groups[i] {
				names = append(names, c.name)
			}
			t.Errorf("commands in no help group (add them to helpGroups and shortHelp): %s", strings.Join(names, ", "))
		}
		for _, c := range groups[i] {
			seen[c.name]++
		}
	}
	for _, c := range commands {
		if c.hidden {
			if seen[c.name] != 0 {
				t.Errorf("twd help lists the hidden command %s", c.name)
			}
			continue
		}
		if seen[c.name] != 1 {
			t.Errorf("twd help lists %s %d times", c.name, seen[c.name])
		}
		if _, ok := shortHelp[c.name]; !ok {
			t.Errorf("no shortHelp line for %s", c.name)
		}
		if l := helpLine(c); len([]rune(l)) > 60 {
			t.Errorf("twd help line for %s is %d characters (≤ 60): %q", c.name, len([]rune(l)), l)
		}
	}

	var so, se bytes.Buffer
	if code := run([]string{"help"}, &so, &se); code != 0 {
		t.Fatalf("twd help: exit %d", code)
	}
	for _, l := range strings.Split(so.String(), "\n") {
		if len([]rune(l)) > 130 {
			t.Errorf("twd help has a %d-character line: %q", len([]rune(l)), l)
		}
	}
	for _, g := range []string{"Set up:", "Develop:", "Deploy:", "Data:", "Observe:", "Operate:"} {
		if !strings.Contains(so.String(), g) {
			t.Errorf("twd help lacks the %q group", g)
		}
	}
	so.Reset()
	if code := run([]string{"help", "--all"}, &so, &se); code != 0 {
		t.Fatalf("twd help --all: exit %d", code)
	}
	for _, c := range commands {
		if !c.hidden && !strings.Contains(so.String(), "twd "+c.usage) {
			t.Errorf("twd help --all lacks %s's usage", c.name)
		}
	}
}
