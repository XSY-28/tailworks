package main

import (
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/rosskukulinski/twd/hostd/mgmtapi"
	twdversion "github.com/rosskukulinski/twd/internal/version"
)

// The CLI tells mgmt its version (User-Agent: twd/<v>) and compares
// mgmt's Twd-Version answer with its own (ADR-041 §7):
// a CLI more than one release line behind, or newer than mgmt, gets one
// warning on stderr (not in --json mode, whose stderr carries only the
// error line).

var versionWarned sync.Once

func withVersionCheck(c *mgmtapi.Client) *mgmtapi.Client {
	if c == nil {
		return c
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{}
	}
	hc := *c.HTTP
	base := hc.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if _, ok := base.(versionRT); ok {
		return c
	}
	hc.Transport = versionRT{base}
	c.HTTP = &hc
	return c
}

type versionRT struct{ base http.RoundTripper }

func (v versionRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("User-Agent", "twd/"+version)
	resp, err := v.base.RoundTrip(r)
	if resp != nil {
		if mv := resp.Header.Get("Twd-Version"); mv != "" && !jsonOut {
			if msg, _ := twdversion.CLIAdvice(version, mv); msg != "" {
				versionWarned.Do(func() { fmt.Fprintln(os.Stderr, "warning [version_skew]: "+msg) })
			}
		}
	}
	return resp, err
}
