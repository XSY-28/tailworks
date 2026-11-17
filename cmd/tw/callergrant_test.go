package main

import "testing"

func TestCallerGrant(t *testing.T) {
	for _, c := range []struct {
		tags []string
		want string
	}{
		{nil, ""},
		{[]string{"tag:tw-wl-support"}, `{ "src": ["autogroup:member"], "dst": ["tag:tw-wl-support"], "ip": ["tcp:80"] }`},
		{[]string{"tag:a", "tag:b"}, `{ "src": ["autogroup:member"], "dst": ["tag:a", "tag:b"], "ip": ["tcp:80"] }`},
	} {
		if got := callerGrant(c.tags); got != c.want {
			t.Errorf("callerGrant(%q)\n got %s\nwant %s", c.tags, got, c.want)
		}
	}
}
