// Package pyodide pins the Pyodide bundles the pinned workerd runs Python
// Workers with, and fetches them into a host's cache directory
// (ADR-055, docs/python.md).
//
// workerd downloads a Pyodide bundle (pyodide_<version>_<date>_<n>.capnp.bin)
// the first time it starts a Python Worker, unless the bundle is already in
// its --pyodide-bundle-disk-cache-dir. Sandboxed workerd has no network and
// may not write outside its instance, so hostd fetches every pinned bundle
// once per host, checks its SHA-256, and gives workerd the directory
// read-only. An offline host is seeded by copying the files there.
//
// Which bundle a Worker needs follows its compatibility date and flags
// (python_workers_20250116, python_workers_314, …), so every bundle the
// pinned workerd may ask for is pinned. Update Bundles with the workerd pin
// (TestBundlesMatchWorkerd checks each pin's version and date are ones the
// pinned binary knows; docs/python.md#updating-the-pins).
package pyodide

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// BaseURL is where workerd itself downloads bundles from.
const BaseURL = "https://pyodide-capnp-bin.edgeworker.net/"

// Bundle is one pinned Pyodide bundle.
type Bundle struct {
	File   string // file name, as workerd looks it up in its cache dir
	SHA256 string // hex
	Size   int64
}

// Bundles are the bundles workerd 1.20261121.1 asks for (one per line,
// oldest first).
var Bundles = []Bundle{
	{File: "pyodide_0.26.0a2_2024-03-01_91.capnp.bin", SHA256: "37273cc289599d7fc4cd2eb352e50d33c70fd5f02e391d02e05d2957ea4afc83", Size: 22473136},
	{File: "pyodide_0.28.2_2025-01-16_22.capnp.bin", SHA256: "ad0dafa91dd0e98f845ccd6298d92b969e6cc208b9648170b596fab7bbe9c8da", Size: 17501408},
	{File: "pyodide_314.0.6_2026-08-17_12.capnp.bin", SHA256: "b95469bc5c223ce884686e4fb0547a3a909a0513706f5c1dc659717550284a04", Size: 13717056},
}

// Missing returns the pinned bundles not in dir (by name and size; Ensure
// checks the digest when it writes one).
func Missing(dir string) []Bundle {
	var out []Bundle
	for _, b := range Bundles {
		if st, err := os.Stat(filepath.Join(dir, b.File)); err != nil || st.Size() != b.Size {
			out = append(out, b)
		}
	}
	return out
}

var mu sync.Mutex // one fetch at a time per process

// Ensure makes dir hold every pinned bundle, downloading the missing ones
// from baseURL ("" = BaseURL) and checking each SHA-256 before it is
// renamed into place. The directory and files end up read-only for
// everyone but their owner.
func Ensure(ctx context.Context, dir, baseURL string) error {
	mu.Lock()
	defer mu.Unlock()
	miss := Missing(dir)
	if len(miss) == 0 {
		return nil
	}
	if baseURL == "" {
		baseURL = BaseURL
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, b := range miss {
		if err := fetch(ctx, dir, baseURL, b); err != nil {
			return fmt.Errorf("pyodide: %s: %w (an offline host: copy the files listed in docs/python.md#offline-hosts into %s)", b.File, err, dir)
		}
	}
	return nil
}

func fetch(ctx context.Context, dir, baseURL string, b Bundle) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+b.File, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s: %s", req.URL, resp.Status)
	}
	f, err := os.CreateTemp(dir, ".fetch-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, b.Size+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != b.Size {
		return fmt.Errorf("got %d bytes, want %d", n, b.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != b.SHA256 {
		return errors.New("SHA-256 " + got + " does not match the pin " + b.SHA256)
	}
	if err := os.Chmod(f.Name(), 0o444); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, b.File))
}

// UserDir is the per-user cache tw dev (and tests) share, so a laptop
// fetches the bundles once: <user cache dir>/tailworks/pyodide.
func UserDir() (string, error) {
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "tailworks", "pyodide"), nil
}

// WorkerdArgs are the workerd flags that point it at dir.
func WorkerdArgs(dir string) []string {
	return []string{"--pyodide-bundle-disk-cache-dir", dir}
}
