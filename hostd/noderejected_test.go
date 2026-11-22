package hostd_test

import (
	"testing"
	"time"

	"github.com/rosskukulinski/tailworks/hostd/fakemgmt"
	"github.com/rosskukulinski/tailworks/hostd/hostdtest"
)

// TestReadyNodeRejectedRemints (issue #200): control rejects a workload node
// after it is ready (here its node key expires; a rejoin from the netmap
// cache sees the same NeedsLogin after Ready). hostd re-mints it under the
// same hostname, mgmt gets a ready report with the new node ID, and the
// workload answers on the new node.
func TestReadyNodeRejectedRemints(t *testing.T) {
	s := startSuite(t, fakemgmt.Config{NoPush: true})
	e := s.e
	d := e.Deploy(hostdtest.Fixture("probe"), "probe", nil)
	probe := e.WaitActive("probe", d, 60*time.Second)
	before, _ := e.FM.Object(probe.ID)
	expired := false
	for _, n := range s.ctl.Server.AllNodes() {
		if n.Hostinfo.Valid() && n.Hostinfo.Hostname() == probe.Hostname {
			n.KeyExpiry = time.Now().Add(-time.Minute)
			s.ctl.Server.UpdateNode(n)
			expired = true
		}
	}
	if !expired {
		t.Fatalf("no control node for %s", probe.Hostname)
	}
	st := time.Now()
	deadline := st.Add(30 * time.Second)
	for {
		o, ok := e.FM.Object(probe.ID)
		if ok && o.Status == "ready" && o.NodeID != "" && o.NodeID != before.NodeID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a ready node that control rejected was never re-minted: mgmt has %+v (before node %s)", o, before.NodeID)
		}
		time.Sleep(50 * time.Millisecond)
	}
	after := e.WaitActive("probe", d, 30*time.Second)
	t.Logf("re-minted and reported ready %v after the key expired", time.Since(st).Round(time.Millisecond))
	if after.Hostname != probe.Hostname || after.NodeID == probe.NodeID {
		t.Errorf("after: %+v (before %+v)", after, probe)
	}
	if n := e.FM.ReadyCalls(probe.ID); n != 2 {
		t.Errorf("accepted ready reports %d, want 2 (the first node, then the re-minted one)", n)
	}
	s.waitPeer(after.IP)
	if code, _ := s.getJSON("http://" + after.IP + "/probe/k/"); code != 200 {
		t.Fatalf("probe via the re-minted node: %d", code)
	}
}
