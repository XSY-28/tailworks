package hostd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range files {
		mode := int64(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	zw.Close()
	return buf.Bytes()
}

func TestExtractTarGzRefusesEscapes(t *testing.T) {
	dir := t.TempDir()
	for name, files := range map[string]map[string]string{
		"dotdot":   {"../evil": "x"},
		"absolute": {"/tmp/evil": "x"},
	} {
		p := filepath.Join(dir, name+".tgz")
		os.WriteFile(p, tarGz(t, files), 0o600)
		if err := extractTarGz(p, filepath.Join(dir, name)); err == nil {
			t.Errorf("%s: extracted", name)
		}
	}
	p := filepath.Join(dir, "ok.tgz")
	os.WriteFile(p, tarGz(t, map[string]string{"twd-0.2.0-linux-amd64/install.sh": "#!/bin/sh\n"}), 0o600)
	if err := extractTarGz(p, filepath.Join(dir, "ok")); err != nil {
		t.Fatal(err)
	}
}

// installArtifact runs the tarball's install.sh into the prefix.
func TestSelfUpgradeInstalls(t *testing.T) {
	prefix := t.TempDir()
	pub, priv := testReleaseKey(t)
	tgz := tarGz(t, signRelease(t, priv, "twd-0.2.0-linux-amd64", map[string]string{
		"twd-0.2.0-linux-amd64/install.sh": "#!/bin/sh\n[ \"$1\" = --prefix ] || exit 3\necho 0.2.0 > \"$2/installed\"\n",
	}))
	h := &Host{cfg: Config{DataDir: t.TempDir(), SelfUpgrade: true, InstallPrefix: prefix, Restart: func() {}}}
	h.upg.signers = []ed25519.PublicKey{pub}
	p := filepath.Join(t.TempDir(), "twd-0.2.0-linux-amd64.tar.gz")
	os.WriteFile(p, tgz, 0o600)
	if err := h.installArtifact(context.Background(), p, filepath.Join(t.TempDir(), "x")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(prefix, "installed")); strings.TrimSpace(string(b)) != "0.2.0" {
		t.Fatalf("install.sh did not run into the prefix: %q", b)
	}
	// A failing install.sh is an error (the old release stays).
	bad := tarGz(t, signRelease(t, priv, "twd-0.2.0-linux-amd64", map[string]string{"twd-0.2.0-linux-amd64/install.sh": "#!/bin/sh\necho nope; exit 1\n"}))
	os.WriteFile(p, bad, 0o600)
	if err := h.installArtifact(context.Background(), p, filepath.Join(t.TempDir(), "y")); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("failing install.sh: %v", err)
	}
	if !h.selfUpgradeEnabled() || !slices.Contains(h.features(), FeatureSelfUpgrade) {
		t.Fatalf("self-upgrade not offered: %v", h.features())
	}
	if slices.Contains((&Host{}).features(), FeatureSelfUpgrade) {
		t.Fatal("self-upgrade offered without -self-upgrade")
	}
}

// approvals.json: records from before versioning load; a newer version is
// not loaded; saves stamp "v":1.
func TestApprovalsFormat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "approvals.json")
	os.WriteFile(p, []byte(`[{"tenantID":"t","workload":"w","workflow":"f","instance":"i","step":"s","type":"x","status":"pending","requestedAt":"2026-11-15T00:00:00Z"}]`), 0o600)
	h := &Host{cfg: Config{DataDir: dir}, log: slog.New(slog.DiscardHandler)}
	s := h.approvals()
	if len(s.m) != 1 {
		t.Fatalf("v0 record not loaded: %v", s.m)
	}
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), `"v": 1`) {
		t.Fatalf("save without v: %s", b)
	}
	os.WriteFile(p, []byte(`[{"v":2,"tenantID":"t","workload":"w","workflow":"f","instance":"i","step":"s"}]`), 0o600)
	h2 := &Host{cfg: Config{DataDir: dir}, log: slog.New(slog.DiscardHandler)}
	if n := len(h2.approvals().m); n != 0 {
		t.Fatalf("a newer record loaded (%d)", n)
	}
}
