package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rosskukulinski/twd/schema"
)

func TestSemver(t *testing.T) {
	for _, c := range []struct {
		a, b  string
		newer bool
	}{
		{"0.2.0", "0.1.0", true},
		{"v0.2.0", "0.2.0-dev", true},
		{"0.2.0-dev", "0.2.0", false},
		{"0.2.0", "0.2.0", false},
		{"0.10.0", "0.9.9", true},
		{"1.0.0", "0.99.0", true},
		{"0.1.1", "0.2.0", false},
	} {
		a, b := parseSemver(c.a), parseSemver(c.b)
		if !a.ok || !b.ok || a.newerThan(b) != c.newer {
			t.Errorf("%s newer than %s: got %v, want %v", c.a, c.b, a.newerThan(b), c.newer)
		}
	}
	if parseSemver("dev").ok || parseSemver("1.2").ok {
		t.Error("dev and 1.2 parsed as versions")
	}
}

// TestVersionCheck: `twd version --check` against a fake GitHub API: a
// newer release prints how to upgrade; --json matches $defs/version.
func TestVersionCheck(t *testing.T) {
	latest := "v0.3.0"
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/rosskukulinski/twd/releases/latest" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"tag_name": latest, "html_url": "https://github.com/rosskukulinski/twd/releases/tag/" + latest})
	}))
	defer gh.Close()
	t.Setenv("TWD_RELEASE_API", gh.URL)
	t.Setenv("TWD_REPO", "")
	old := version
	t.Cleanup(func() { version = old })
	state := t.TempDir()

	version = "0.2.0"
	out, err := captureStdout(t, func() error { return cmdVersion([]string{"--check", "--state-dir", state}) })
	if err != nil || !strings.Contains(out, "twd 0.3.0 is out (this is 0.2.0)") || !strings.Contains(out, "upgrade: "+upgradeCommand()) {
		t.Fatalf("version --check: %v %q", err, out)
	}
	out, err = captureStdout(t, func() error { return cmdVersion([]string{"--check", "--json", "--state-dir", state}) })
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	json.Unmarshal([]byte(out), &v)
	if probs, err := schema.CheckCLI("version", v); err != nil || len(probs) > 0 {
		t.Fatalf("version --check --json vs schema: %v %v\n%s", probs, err, out)
	}
	if v["latest"] != "0.3.0" || v["updateAvailable"] != true || v["daemons"] != nil {
		t.Fatalf("version --check --json: %s", out)
	}
	// The check refreshed the notice's cache.
	if c := loadUpdateCache(state); c.Latest != "0.3.0" || time.Since(c.CheckedAt) > time.Minute {
		t.Fatalf("cache: %+v", c)
	}

	version = "0.3.0"
	out, _ = captureStdout(t, func() error { return cmdVersion([]string{"--check", "--state-dir", state}) })
	if !strings.Contains(out, "is the latest release") {
		t.Fatalf("up to date: %q", out)
	}
	version = "0.3.0-dev"
	out, _ = captureStdout(t, func() error { return cmdVersion([]string{"--check", "--state-dir", state}) })
	if !strings.Contains(out, "development build") {
		t.Fatalf("dev build: %q", out)
	}

	latest = ""
	if err := cmdVersion([]string{"--check", "--state-dir", state}); err == nil || !strings.Contains(err.Error(), "update_check_failed") {
		t.Fatalf("no release: %v", err)
	}
}

// TestUpdateNotice: the notice is printed at most once a day, only when a
// newer release is out, and never for --json, CI or a non-terminal.
func TestUpdateNotice(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = "0.2.0"
	now := time.Now()
	c := updateCache{CheckedAt: now, Latest: "0.3.0"}
	if l := updateNoticeLine(c, now); !strings.Contains(l, "twd 0.3.0 is out") || !strings.Contains(l, "TWD_NO_UPDATE_CHECK=1") {
		t.Fatalf("notice: %q", l)
	}
	c.NotifiedAt = now.Add(-time.Hour)
	if l := updateNoticeLine(c, now); l != "" {
		t.Fatalf("notice twice in a day: %q", l)
	}
	c.NotifiedAt, c.Latest = time.Time{}, "0.2.0"
	if l := updateNoticeLine(c, now); l != "" {
		t.Fatalf("notice without a newer release: %q", l)
	}
	var buf strings.Builder
	if updateNoticeEnabled("ls", &buf) {
		t.Fatal("notice enabled for a non-terminal writer")
	}
	t.Setenv("CI", "1")
	if updateNoticeEnabled("ls", nil) {
		t.Fatal("notice enabled under CI")
	}
}
