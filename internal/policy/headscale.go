package policy

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The Headscale profile (ADR-015, RFC §7.2). Headscale's policy parser
// rejects fields and values it doesn't know (measured on v0.29.7 with
// POST /api/v1/policy/check), so a snippet for a Headscale server carries:
//   - no postures and no srcPosture ("unknown field");
//   - no svc: destinations ("invalid alias format") and no
//     autoApprovers.services ("unknown field");
//   - no nodeAttrs: tailworks' only one is funnel, which Headscale doesn't
//     support, and Headscale's nodeAttrs take no app caps;
//   - no autogroup:admin, as an owner ("invalid owner format") or a source:
//     tagOwners are users, groups or tags.
//
// Caps travel in grants' app field, which Headscale accepts.

// Headscale adapts s to the Headscale profile: autogroup:admin is dropped
// from every tagOwners list, so tailworks' tags are owned by tag:tw and tag:tw
// itself has no owners (only the API key mints it; nobody can tag their
// own device with it). Grants from autogroup:admin and all nodeAttrs are
// dropped. The result passes Check with Options{Headscale: true}.
func Headscale(s Snippet) Snippet {
	out := Snippet{TagOwners: map[string][]string{}}
	for t, owners := range s.TagOwners {
		var keep []string
		for _, o := range owners {
			if !strings.HasPrefix(o, "autogroup:") && o != t {
				keep = append(keep, o)
			}
		}
		if len(keep) == 0 && t != TwTagPrefix {
			keep = []string{TwTagPrefix}
		}
		if keep == nil {
			keep = []string{}
		}
		out.TagOwners[t] = keep
	}
	for _, g := range s.Grants {
		if slices.ContainsFunc(strs(g["src"]), func(x string) bool { return strings.HasPrefix(x, "autogroup:") }) {
			continue
		}
		out.Grants = append(out.Grants, maps.Clone(g))
	}
	return out
}

// checkHeadscale rejects what Headscale's parser would.
func checkHeadscale(s Snippet) error {
	if len(s.NodeAttrs) > 0 {
		return fmt.Errorf("policy snippet: the Headscale profile sets no nodeAttrs (Headscale has no funnel; caps go in grants)")
	}
	for _, t := range slices.Sorted(maps.Keys(s.TagOwners)) {
		for _, o := range s.TagOwners[t] {
			if strings.HasPrefix(o, "autogroup:") {
				return fmt.Errorf("policy snippet: Headscale rejects owner %q for %q (owners are users, groups or tags)", o, t)
			}
		}
	}
	for i, g := range s.Grants {
		for _, x := range strs(g["src"]) {
			if strings.HasPrefix(x, "autogroup:") {
				return fmt.Errorf("policy snippet: grant %d: the Headscale profile has no %s source", i, x)
			}
		}
		for _, x := range strs(g["dst"]) {
			if strings.HasPrefix(x, "svc:") {
				return fmt.Errorf("policy snippet: grant %d: Headscale has no Tailscale Services (dst %q)", i, x)
			}
		}
	}
	return nil
}
