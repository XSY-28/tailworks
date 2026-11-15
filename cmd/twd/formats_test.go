package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// formatFixture returns testdata/formats/<name>; TWD_WRITE_FIXTURES=1
// writes it from gen only if it is missing (ADR-upgrades-and-releases §4).
func formatFixture(t *testing.T, name string, gen func() []byte) []byte {
	t.Helper()
	p := filepath.Join("testdata", "formats", name)
	if os.Getenv("TWD_WRITE_FIXTURES") == "1" {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			if err := os.WriteFile(p, gen(), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%s: %v (new fixture? run with TWD_WRITE_FIXTURES=1)", p, err)
	}
	return b
}

// TestConfigFormatFixtures: every committed twd.json still reads.
func TestConfigFormatFixtures(t *testing.T) {
	gens := map[string]func() []byte{
		// twd.json as `twd up` wrote it before versioning.
		"twd-v0.json": func() []byte {
			return []byte(`{
  "tailnet": "example.ts.net",
  "tenantID": "t_1",
  "mintMode": "oauth",
  "workerd": "/home/me/.twd/bin/workerd"
}`)
		},
		"twd-v1.json": func() []byte {
			b, _ := json.MarshalIndent(Config{V: configVersion, Tailnet: "example.ts.net", TenantID: "t_1", MintMode: "oauth",
				Workerd: "/home/me/.twd/bin/workerd"}, "", "  ")
			return b
		},
	}
	for _, name := range []string{"twd-v0.json", "twd-v1.json"} {
		var c Config
		if err := decodeConfig(formatFixture(t, name, gens[name]), &c); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if c.Tailnet != "example.ts.net" || c.TenantID != "t_1" || c.MintMode != "oauth" || c.Workerd != "/home/me/.twd/bin/workerd" {
			t.Errorf("%s: %+v", name, c)
		}
	}
	var c Config
	if err := decodeConfig([]byte(`{"v":2,"tailnet":"x"}`), &c); err == nil || !strings.Contains(err.Error(), "newer twd") {
		t.Fatalf("v2: %v", err)
	}
}

// TestSaveWritesConfigVersion: save stamps twd.json.
func TestSaveWritesConfigVersion(t *testing.T) {
	s := &state{Dir: t.TempDir(), Cfg: Config{Tailnet: "example.ts.net"}}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(s.configPath())
	if !strings.Contains(string(b), `"v": 1`) {
		t.Fatalf("twd.json: %s", b)
	}
}
