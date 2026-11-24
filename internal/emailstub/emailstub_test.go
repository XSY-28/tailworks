package emailstub

import (
	"os"
	"strings"
	"testing"
)

// TestBuildMJSMatches: create-tailworks' build.mjs stubs cloudflare:email
// with the same text as the Go bundler (it can't import this package).
func TestBuildMJSMatches(t *testing.T) {
	b, err := os.ReadFile("../../js/packages/create-tailworks/lib/build.mjs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `contents: "`+ClassJS+`",`) {
		t.Fatalf("build.mjs's cloudflare:email stub differs from emailstub.ClassJS:\n%s", ClassJS)
	}
}
