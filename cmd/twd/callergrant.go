package main

import (
	"fmt"
	"strconv"
	"strings"
)

// callerGrant is the grant that lets people on the tailnet call a workload
// node with these tags (issue #48). Nothing reaches a new workload until a
// grant allows it, and the deploy preflight checks only the app's own tags
// and tailnet.connects, not who may call it. Workload nodes listen on port
// 80 (netd's ingress port).
func callerGrant(workloadTags []string) string {
	if len(workloadTags) == 0 {
		return ""
	}
	q := make([]string, len(workloadTags))
	for i, t := range workloadTags {
		q[i] = strconv.Quote(t)
	}
	return fmt.Sprintf(`{ "src": ["autogroup:member"], "dst": [%s], "ip": ["tcp:80"] }`, strings.Join(q, ", "))
}

// printCallerGrant prints callerGrant next to the tagOwners snippet that
// twd deploy prints for a new app's tags.
func printCallerGrant(workloadTags []string) {
	g := callerGrant(workloadTags)
	if g == "" {
		return
	}
	fmt.Printf("  so people on your tailnet can call it, also add to \"grants\" (twd doesn't check this one; narrow src to a user or group if you like):\n    %s\n", g)
}
