package main

import (
	"fmt"

	"github.com/rosskukulinski/twd/schema"
)

// checkTwdSchema refuses a twd.jsonc with keys or values twd doesn't know,
// so a typo ("objcts") fails here instead of deploying silently without it.
func checkTwdSchema(raw []byte) error {
	probs, err := schema.CheckTwd(raw)
	if err != nil {
		return fail("bad_request", "twd.jsonc: "+err.Error())
	}
	if len(probs) == 0 {
		return nil
	}
	lines := make([]string, len(probs))
	for i, p := range probs {
		lines[i] = p.String()
	}
	msg := "twd.jsonc has 1 problem"
	if len(probs) > 1 {
		msg = fmt.Sprintf("twd.jsonc has %d problems", len(probs))
	}
	return fail("bad_request", msg, lines...).
		withFix("every key is listed in docs/twd-jsonc.md; add \"$schema\" for editor completion (see that page)")
}
