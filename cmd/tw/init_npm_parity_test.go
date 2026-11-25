package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCreateTailworksFallbackMatchesInit(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	packageDir, err := createTailworksDir()
	if err != nil {
		t.Skip("create-tailworks not found:", err)
	}
	packageDir, err = filepath.Abs(packageDir)
	if err != nil {
		t.Fatal(err)
	}
	templates, err := os.ReadDir(filepath.Join(packageDir, "templates"))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(packageDir, "bin", "create-tailworks.js")
	noTW := t.TempDir()
	for _, template := range templates {
		if !template.IsDir() {
			continue
		}
		t.Run(template.Name(), func(t *testing.T) {
			// Exercise case folding, separators, and the 40-character label limit.
			const directory = "İDemo__Project---1234567890123456789012345678901234567890"
			goDir := filepath.Join(t.TempDir(), directory)
			if err := cmdInit([]string{goDir, "--template", template.Name()}); err != nil {
				t.Fatal(err)
			}

			jsRoot := t.TempDir()
			args := []string{bin, directory}
			if template.Name() != "hello" {
				args = append(args, "--template", template.Name())
			}
			cmd := exec.CommandContext(t.Context(), node, args...)
			cmd.Dir = jsRoot
			cmd.Env = append(cmd.Environ(), "PATH="+noTW)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("create-tailworks: %v\n%s", err, out)
			}
			for _, name := range []string{"package.json", "tailworks.jsonc", "wrangler.jsonc"} {
				want, goErr := os.ReadFile(filepath.Join(goDir, name))
				got, jsErr := os.ReadFile(filepath.Join(jsRoot, directory, name))
				if errors.Is(goErr, os.ErrNotExist) && errors.Is(jsErr, os.ErrNotExist) {
					continue
				}
				if goErr != nil || jsErr != nil || !bytes.Equal(got, want) {
					t.Errorf("%s differs from tw init (Go: %v, Node: %v)\nGot:\n%s\nWant:\n%s", name, goErr, jsErr, got, want)
				}
			}
		})
	}
}
