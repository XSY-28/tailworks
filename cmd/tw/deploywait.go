package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rosskukulinski/tailworks/hostd/mgmtapi"
	"github.com/rosskukulinski/tailworks/mgmt"
)

// waitReady waits for the primary deployment to reach its ready workload node.
func waitReady(ctx context.Context, mc *mgmtapi.Client, deploymentID, workload, label string, since time.Time, wait time.Duration, ev *events) (mgmt.DeploymentView, time.Duration, error) {
	var v mgmt.DeploymentView
	var activeAt time.Duration
	lastStatus := ""
	for {
		if err := mc.Do(ctx, "GET", "/v1/deployments/"+deploymentID, nil, &v); err != nil {
			return v, activeAt, err
		}
		if st := v.Status + "/" + v.Endpoint.Status; st != lastStatus {
			ev.emit("status", map[string]any{"deploymentID": deploymentID, "status": v.Status, "node": v.Endpoint.Status})
			lastStatus = st
		}
		switch v.Status {
		case "failed":
			return v, activeAt, fail("deploy_failed", "the host could not start "+label, strings.Split(v.Error, "\n")...).withFix("tw logs " + workload)
		case "crashloop":
			return v, activeAt, fail("deploy_crashloop", label+" started, then its workerd kept exiting; the host keeps restarting it with backoff", strings.Split(v.Error, "\n")...).withFix("tw logs " + workload)
		case "active":
			if activeAt == 0 {
				activeAt = time.Since(since)
			}
			if v.Endpoint.Status == "ready" && v.Endpoint.MagicDNS != "" {
				return v, activeAt, nil
			}
		}
		if v.Endpoint.Status == "failed" || v.Endpoint.Status == "killed" {
			return v, activeAt, workloadNodeFailed(workload, v.Endpoint.Status)
		}
		if time.Since(since) > wait {
			return v, activeAt, fail("identity_pending", fmt.Sprintf("%s not ready after %v (status %s, node %s)", workload, wait, v.Status, v.Endpoint.Status)).withFix("tw status", "tw logs "+workload)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
