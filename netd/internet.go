package netd

import (
	"strings"

	"tailscale.com/ipn/ipnstate"
)

// RecipeInternetAPI is where an internet egress refusal points: a gateway on
// the tailnet is how a Worker calls an internet API.
const RecipeInternetAPI = "https://github.com/rosskukulinski/tailworks/blob/master/docs/recipes.md#call-an-internet-api"

// internetEgressDetail is the one-line explanation for a name that is not on
// the tailnet. Workers (and Node apps) have no internet egress, and there is
// no switch that turns it on; only containers have an egress.internet
// allowlist.
func internetEgressDetail(host string) string {
	return host + " is not a tailnet name, and Workers have no internet egress (names are never resolved via host DNS). " +
		"Put a gateway for it on your tailnet and call that: " + RecipeInternetAPI
}

// tailnetShaped reports whether want (lowercase, no trailing dot) looks like
// a name on a tailnet, so a miss means a missing peer or grant rather than an
// internet destination: a bare MagicDNS short name, a name under this node's
// MagicDNS suffix or any visible peer's, or any *.ts.net name.
func tailnetShaped(want string, st *ipnstate.Status) bool {
	if !strings.Contains(want, ".") {
		return true
	}
	under := func(suffix string) bool {
		suffix = strings.Trim(strings.ToLower(suffix), ".")
		return suffix != "" && strings.HasSuffix(want, "."+suffix)
	}
	if under("ts.net") || under("beta.tailscale.net") {
		return true
	}
	if st == nil {
		return false
	}
	if st.CurrentTailnet != nil && under(st.CurrentTailnet.MagicDNSSuffix) {
		return true
	}
	if under(st.MagicDNSSuffix) {
		return true
	}
	if st.Self != nil {
		if _, s, ok := strings.Cut(strings.TrimSuffix(st.Self.DNSName, "."), "."); ok && under(s) {
			return true
		}
	}
	for _, p := range st.Peer {
		if _, s, ok := strings.Cut(strings.TrimSuffix(p.DNSName, "."), "."); ok && under(s) {
			return true
		}
	}
	return false
}
