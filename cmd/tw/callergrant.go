package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rosskukulinski/tailworks/mgmt"
)

// callerGrant is the grant that lets people on the tailnet call a workload
// node with these tags (issue #48). Nothing reaches a new workload until a
// grant allows it, and the deploy preflight checks only the app's own tags
// and tailnet.connects, not who may call it. Workload nodes listen on port
// 80 (netd's ingress port), and with tailnet.https on 443 too
// (mgmt.CallerPorts, #360).
func callerGrant(workloadTags []string, https bool) string {
	if len(workloadTags) == 0 {
		return ""
	}
	q := make([]string, len(workloadTags))
	for i, t := range workloadTags {
		q[i] = strconv.Quote(t)
	}
	ports := mgmt.CallerPorts(https)
	for i, p := range ports {
		ports[i] = strconv.Quote(p)
	}
	return fmt.Sprintf(`{ "src": ["autogroup:member"], "dst": [%s], "ip": [%s] }`, strings.Join(q, ", "), strings.Join(ports, ", "))
}

// serviceCallerGrant is callerGrant for a workload behind a Tailscale
// Service: callers of the svc: name need a grant to it, and only the app
// capabilities on that grant reach caps() on calls through the Service
// (H22, #155; a grant to the workload tags covers only its node's own name).
func serviceCallerGrant(service string) string {
	if service == "" {
		return ""
	}
	return fmt.Sprintf(`{ "src": ["autogroup:member"], "dst": [%s], "ip": ["tcp:80"] }`, strconv.Quote(service))
}

// printCallerGrant prints callerGrant (and serviceCallerGrant for a
// Service) next to the tagOwners snippet that tw deploy prints for a new
// app's tags.
func printCallerGrant(workloadTags []string, https bool, service string) {
	g := callerGrant(workloadTags, https)
	if g == "" {
		return
	}
	fmt.Printf("  so people on your tailnet can call it, also add to \"grants\" (tailworks doesn't check this one; narrow src to a user or group if you like):\n    %s\n", g)
	if sg := serviceCallerGrant(service); sg != "" {
		fmt.Printf("  and for calls through %s (put callers' app capabilities on this grant):\n    %s\n", service, sg)
	}
}
