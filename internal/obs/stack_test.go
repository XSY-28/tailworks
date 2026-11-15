package obs

import (
	"strings"
	"testing"
	"time"
)

func TestFoldRuntimeFrames(t *testing.T) {
	stack := strings.Join([]string{
		"Error: boom at /boom",
		"    at explode (src/boom.ts:3:9)",
		"    at Object.fetch (src/index.ts:8:7)",
		"    at Object.fetch (__twd_rt.js:1712:28)",
		"    at async __twd_rt.js:1650:12",
		"    at async runWithCaller (__twd_rt.js:88:3)",
		"    at async Object.fetch (__twd_main.js:4:10)",
		"    at handler (src/index.ts:20:1)",
		"    at __twd_facet.js:9:9",
	}, "\n")
	got := FoldRuntimeFrames(stack)
	want := strings.Join([]string{
		"Error: boom at /boom",
		"    at explode (src/boom.ts:3:9)",
		"    at Object.fetch (src/index.ts:8:7)",
		"    … 4 twd runtime frames (__twd_main.js, __twd_rt.js)",
		"    at handler (src/index.ts:20:1)",
		"    … 1 twd runtime frame (__twd_facet.js)",
	}, "\n")
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestFoldRuntimeFramesLeavesAppFrames(t *testing.T) {
	for _, s := range []string{
		"",
		"Error: x\n    at f (src/index.ts:1:1)",
		// The app's own file that merely mentions __twd_ isn't a runtime frame.
		"Error: x\n    at __twd_helper (src/index.ts:1:1)",
		// A message that names the runtime isn't a frame.
		"Error: thrown in __twd_rt.js:1:1",
	} {
		if got := FoldRuntimeFrames(s); got != s {
			t.Errorf("changed %q to %q", s, got)
		}
	}
}

// Text folds the runtime frames; the JSON record keeps the whole stack.
func TestTextFoldsRuntimeFrames(t *testing.T) {
	r := &Record{Time: time.Unix(0, 0), Kind: KindRequest, Method: "GET", Path: "/boom", Status: 500, Exceptions: []Exception{{
		Name: "Error", Message: "boom", Stack: "Error: boom\n    at f (src/index.ts:2:3)\n    at __twd_rt.js:1:2\n    at __twd_rt.js:3:4",
	}}}
	txt := r.Text()
	if !strings.Contains(txt, "at f (src/index.ts:2:3)") || !strings.Contains(txt, "… 2 twd runtime frames (__twd_rt.js)") || strings.Contains(txt, "__twd_rt.js:1:2") {
		t.Fatalf("text:\n%s", txt)
	}
	if !strings.Contains(r.Exceptions[0].Stack, "__twd_rt.js:1:2") {
		t.Fatal("Text changed the record's stack")
	}
}
