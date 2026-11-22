// Package publicip is the SSRF guard shared by both internet egress paths:
// Workers' fetch() (netd, ADR-worker-internet-egress) and containers'
// enableInternet (netd/sbxnet, ADR-032 D4). It decides which addresses an
// internet flow may dial (ADR-019) and matches tailnet.egress.internet
// allowlists, so the two paths can never disagree about either.
//
// The check is always applied to the address that is actually dialled,
// after DNS, never to a name: a name that resolves to a private address is
// refused like the address itself (no DNS rebinding past the check).
package publicip

import (
	"net/netip"
	"strings"
)

// blockedNets are reserved, tailnet and translated ranges that the
// stdlib predicates in Blocked don't cover.
var blockedNets = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT: the tailnet range, incl. 100.100.100.100 (ADR-019)
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking; sbxnet's synthetic pool
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),               // IPv4-compatible (deprecated) embeds IPv4
	netip.MustParsePrefix("100::/64"),            // discard
	netip.MustParsePrefix("fec0::/10"),           // site-local (deprecated)
	netip.MustParsePrefix("fd7a:115c:a1e0::/48"), // the tailnet's IPv6 range
	netip.MustParsePrefix("64:ff9b::/96"),        // NAT64 could reach any IPv4
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"), // 6to4 embeds IPv4
	netip.MustParsePrefix("2001::/32"), // Teredo
}

// Blocked returns why a may not be dialled by internet egress, or "" for
// a public address. It does not know the host's own addresses; callers
// refuse those too.
func Blocked(a netip.Addr) string {
	a = a.Unmap()
	switch {
	case !a.IsValid():
		return "invalid address"
	case a.IsLoopback(), a.IsUnspecified():
		return "loopback/unspecified"
	case a.IsLinkLocalUnicast(), a.IsLinkLocalMulticast(), a.IsMulticast(), a.IsInterfaceLocalMulticast():
		return "link-local/multicast (metadata services)"
	case a.IsPrivate():
		return "a private network"
	case !a.IsGlobalUnicast():
		return "not a global unicast address"
	case a == netip.AddrFrom4([4]byte{255, 255, 255, 255}):
		return "broadcast"
	}
	for _, n := range blockedNets {
		if n.Contains(a) {
			return "a reserved, tailnet or translated range (" + n.String() + ")"
		}
	}
	return ""
}

// specialTLDs are suffixes that never name a public internet host: the
// special-use names (RFC 6761, 6762, 8375, 9476) and common private ones.
// A name under one is refused before any DNS lookup, so it never reaches
// the host's resolver (which may well answer it with a LAN address).
var specialTLDs = []string{"alt", "arpa", "corp", "home", "internal", "intranet", "invalid", "lan", "local", "localdomain", "localhost", "onion", "test"}

// SpecialName reports whether name (lowercase, no trailing dot) cannot be
// a public DNS name: a single label, a special-use or private suffix, or a
// numeric last label (a resolver may read "127.1" or "0x7f.1" as an
// address).
func SpecialName(name string) bool {
	dot := strings.LastIndexByte(name, '.')
	if dot < 0 {
		return true
	}
	tld := name[dot+1:]
	if tld == "" || strings.Trim(tld, "0123456789") == "" || strings.HasPrefix(tld, "0x") {
		return true
	}
	for _, s := range specialTLDs {
		if tld == s {
			return true
		}
	}
	return false
}

// Allowlist is a parsed tailnet.egress.internet list: host names,
// "*.suffix" wildcards, "*" (any name), IPs and CIDRs.
type Allowlist struct {
	hosts []string
	nets  []netip.Prefix
}

// ParseAllowlist parses entries (case and a trailing dot are ignored;
// blanks are skipped).
func ParseAllowlist(entries []string) Allowlist {
	var l Allowlist
	for _, a := range entries {
		a = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(a)), ".")
		if a == "" {
			continue
		}
		if pf, err := netip.ParsePrefix(a); err == nil {
			l.nets = append(l.nets, pf.Masked())
		} else if ip, err := netip.ParseAddr(a); err == nil {
			l.nets = append(l.nets, netip.PrefixFrom(ip.Unmap(), ip.Unmap().BitLen()))
		} else {
			l.hosts = append(l.hosts, a)
		}
	}
	return l
}

// Allows reports whether name (lowercase, no trailing dot; a host name or
// an IP literal) is on the list. An IP literal matches only IP and CIDR
// entries; a name matches only host entries.
func (l Allowlist) Allows(name string) bool {
	if ip, err := netip.ParseAddr(name); err == nil {
		for _, n := range l.nets {
			if n.Contains(ip.Unmap()) {
				return true
			}
		}
		return false
	}
	for _, h := range l.hosts {
		if HostMatch(h, name) {
			return true
		}
	}
	return false
}

// HostMatch matches a lowercase host pattern: "*" (any), "*.suffix" (any
// name strictly under suffix) or an exact name.
func HostMatch(pattern, name string) bool {
	switch {
	case pattern == "*":
		return true
	case strings.HasPrefix(pattern, "*."):
		return strings.HasSuffix(name, pattern[1:]) && len(name) > len(pattern)-1
	}
	return pattern == name
}
