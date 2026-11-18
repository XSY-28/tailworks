package hostd

import (
	"fmt"
	"strings"
)

// seatbeltExecDirs are the system dirs a sandboxed workload may exec from
// (a Node app's child_process: /bin/sh, /usr/bin/…), besides its runtime
// binary, a Node install prefix and its own dirs. The Linux sandbox allows
// the same OS dirs (procsb's systemRO).
var seatbeltExecDirs = []string{
	"/System",
	"/bin",
	"/opt",
	"/sbin",
	"/usr",
}

// seatbeltDevWrite are the device files a workload may open for writing
// (dyld registers with /dev/dtracehelper at startup).
var seatbeltDevWrite = []string{
	"/dev/dtracehelper",
	"/dev/null",
	"/dev/zero",
}

// seatbeltProfile is the Seatbelt profile of a process-tier workload
// (issue #121). On top of (allow default), which keeps the Mach services
// and sysctls workerd and node need:
//
//   - reads and writes are denied under each of deny (home dir, hostd's
//     data and run dirs, credential files), then allowed again under each
//     of own; each of readOnly (the runtime binary) and everything below
//     readOnlyTrees (a Node install prefix) is readable;
//   - writes are denied everywhere else too (/tmp, /Users/Shared, …),
//     except a few devices;
//   - all network is denied (no DNS: netd resolves names), except unix
//     sockets in own, TCP connects to localhost:<connectTCP> (a Node app's
//     egress proxy and its own port) and TCP binds on localhost:<bindTCP>
//     (its own port, workerd's inspector);
//   - signals reach only processes in the same sandbox instance (the
//     workload itself and its children): not tw-hostd, tw-mgmt or another
//     workload, even of the same tenant;
//   - exec is allowed only of the runtime binary, the Node prefix, own and
//     the OS dirs (seatbeltExecDirs).
//
// Seatbelt applies the last matching rule, so allows follow denies.
func seatbeltProfile(own, readOnly, readOnlyTrees, deny []string, bindTCP, connectTCP []uint16) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n")
	b.WriteString("(deny file-write*)\n")
	for _, d := range seatbeltDevWrite {
		fmt.Fprintf(&b, "(allow file-write* (literal %s))\n", sbQuote(d))
	}
	b.WriteString("(deny network*)\n")
	for _, p := range connectTCP {
		fmt.Fprintf(&b, "(allow network-outbound (remote tcp \"localhost:%d\"))\n", p)
	}
	for _, p := range bindTCP {
		fmt.Fprintf(&b, "(allow network-bind network-inbound (local tcp \"localhost:%d\"))\n", p)
	}
	b.WriteString("(deny signal)\n(allow signal (target same-sandbox))\n")
	b.WriteString("(deny process-exec*)\n")
	for _, d := range seatbeltExecDirs {
		fmt.Fprintf(&b, "(allow process-exec (subpath %s))\n", sbQuote(d))
	}
	for _, o := range readOnly {
		for _, p := range []string{o, realPath(o)} {
			fmt.Fprintf(&b, "(allow process-exec (literal %s))\n", sbQuote(p))
		}
	}
	for _, o := range append(append([]string{}, readOnlyTrees...), own...) {
		fmt.Fprintf(&b, "(allow process-exec (subpath %s))\n", sbQuote(realPath(o)))
	}
	for _, r := range realPaths(deny) {
		fmt.Fprintf(&b, "(deny file-read* file-write* (subpath %s))\n", sbQuote(r))
	}
	for _, m := range ancestors(append(append([]string{}, own...), readOnlyTrees...)) {
		fmt.Fprintf(&b, "(allow file-read-metadata (literal %s))\n", sbQuote(m))
	}
	for _, o := range readOnly {
		for _, p := range []string{o, realPath(o)} {
			fmt.Fprintf(&b, "(allow file-read* (literal %s))\n", sbQuote(p))
		}
	}
	for _, o := range readOnlyTrees {
		fmt.Fprintf(&b, "(allow file-read* (subpath %s))\n", sbQuote(realPath(o)))
	}
	for _, o := range own {
		r := realPath(o)
		fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", sbQuote(r))
		fmt.Fprintf(&b, "(allow network-outbound (remote unix-socket (subpath %s)))\n", sbQuote(r))
		fmt.Fprintf(&b, "(allow network-bind network-inbound (local unix-socket (subpath %s)))\n", sbQuote(r))
	}
	return b.String()
}
