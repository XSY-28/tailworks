package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rosskukulinski/tailworks/hostd/bundle"
)

func assetsApp(t *testing.T, assets string, files map[string]string) *app {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"wrangler.jsonc": `{"name":"site","main":"src/index.js",` + assets + `}`,
		"src/index.js":   "export default {}",
	})
	writeFiles(t, dir, files)
	a, err := loadApp(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestAddAssets: assets.directory's files go in the bundle under assets/,
// except .assetsignore matches, VCS/OS clutter and the control files
// tailworks doesn't read yet (warned at load).
func TestAddAssets(t *testing.T) {
	a := assetsApp(t, `"assets":{"directory":"./dist","binding":"ASSETS"}`, map[string]string{
		"dist/index.html":               "<h1>hi</h1>",
		"dist/js/app.js":                "1",
		"dist/.well-known/security.txt": "x",
		"dist/_headers":                 "/*\n  X-A: b",
		"dist/_redirects":               "/a /b",
		"dist/.assetsignore":            "*.map\nprivate/\n/top-only.txt\n# comment",
		"dist/js/app.js.map":            "{}",
		"dist/private/secret.txt":       "s",
		"dist/top-only.txt":             "t",
		"dist/sub/top-only.txt":         "kept",
		"dist/.DS_Store":                "junk",
	})
	files := map[string][]byte{}
	if err := a.addAssets(files); err != nil {
		t.Fatal(err)
	}
	var got []string
	for p := range files {
		got = append(got, p)
	}
	sort.Strings(got)
	want := "assets/.well-known/security.txt,assets/index.html,assets/js/app.js,assets/sub/top-only.txt"
	if strings.Join(got, ",") != want {
		t.Errorf("bundle assets:\n %s\nwant\n %s", strings.Join(got, ","), want)
	}
	w := strings.Join(a.Warnings, "\n")
	for _, n := range []string{"_headers", "_redirects"} {
		if !strings.Contains(w, "assets: "+n+" is not supported yet") {
			t.Errorf("no warning for %s: %s", n, w)
		}
	}
	if strings.Contains(w, "assets: not supported") {
		t.Errorf("assets still warned as unsupported: %s", w)
	}
}

func TestAddAssetsErrors(t *testing.T) {
	for _, c := range []struct{ assets, want string }{
		{`"assets":{"directory":"./nope"}`, "is not a directory"},
		{`"assets":{"binding":"ASSETS"}`, "assets has no directory"},
		{`"assets":{"directory":"./public","html_handling":"sometimes"}`, "html_handling"},
		{`"assets":"./public"`, "want an object"},
	} {
		a := assetsApp(t, c.assets, map[string]string{"public/index.html": "x"})
		err := a.addAssets(map[string][]byte{})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.assets, err, c.want)
		}
	}
	// A build output under assets/ would be taken for an asset.
	a := assetsApp(t, `"assets":{"directory":"./public"}`, map[string]string{"public/index.html": "x"})
	if err := a.addAssets(map[string][]byte{bundle.AssetsPrefix + "x.js": nil}); err == nil || !strings.Contains(err.Error(), "where the bundle carries assets") {
		t.Errorf("collision: %v", err)
	}
	// No "assets": nothing added, no error.
	a = assetsApp(t, "", nil)
	files := map[string][]byte{}
	if err := a.addAssets(files); err != nil || len(files) != 0 {
		t.Errorf("no assets: %v %v", files, err)
	}
}

// TestSnapshotAssetsDir: tw dev reloads on an edit to an asset, even in
// dist/ (which the source snapshot skips).
func TestSnapshotAssetsDir(t *testing.T) {
	a := assetsApp(t, `"assets":{"directory":"./dist"}`, map[string]string{"dist/index.html": "v1"})
	before := snapshot(a.Dir, a.assetsDir())
	if snapshot(a.Dir, "") == before {
		t.Fatal("the assets dir did not count")
	}
	p := filepath.Join(a.Dir, "dist", "index.html")
	future := time.Now().Add(time.Minute)
	os.WriteFile(p, []byte("v2"), 0o644)
	os.Chtimes(p, future, future)
	if snapshot(a.Dir, a.assetsDir()) == before {
		t.Error("an asset edit did not change the snapshot")
	}
}
