//go:build e2edataplane

package dataplane

import (
	"fmt"
	"math/rand"
	"strings"
	"syscall"
	"time"
)

// A fault takes the cluster, the current lease holder and a seeded rand,
// does its damage, heals, and returns what it did. Long faults outlast the
// lease (45 s) and force a takeover onto another host, as an operator would
// (tailworks has no automatic failover: docs/locks.md).
type fault struct {
	name string
	run  func(n *nemesis, host string) (detail string)
}

type nemesis struct {
	c      *cluster
	h      *history
	tg     *target
	rnd    *rand.Rand
	bundle string
}

func (n *nemesis) between(lo, hi time.Duration) time.Duration {
	return lo + time.Duration(n.rnd.Int63n(int64(hi-lo)+1))
}

func (n *nemesis) other(host string) string {
	var o []string
	for _, h := range n.c.hosts {
		if h != host {
			o = append(o, h)
		}
	}
	return o[n.rnd.Intn(len(o))]
}

// moveTo deploys dp onto host. live (a planned move of a running workload)
// is --replace --force-takeover: the lease moves at once and the old host
// stops at its next heartbeat. Otherwise it is --force-takeover alone, which
// mgmt refuses until the old host's lease has run out (45 s): the safe
// takeover of a host that may still be running (docs/locks.md "Failure
// modes"). Refusals are retried until within.
func (n *nemesis) moveTo(host string, live bool, within time.Duration) (deployment, string, error) {
	deadline := time.Now().Add(within)
	refused := map[int]int{}
	start := time.Now()
	for {
		d, st, b, err := n.c.deploy(n.bundle, host, live, true)
		if err == nil && st == 202 {
			n.tg.mu.Lock()
			n.tg.depID = d.DeploymentID
			n.tg.mu.Unlock()
			d, err = n.c.waitActive(d.DeploymentID, 120*time.Second)
			if err != nil {
				return d, "", err
			}
			n.tg.set(d)
			return d, fmt.Sprintf("accepted after %.1fs (refused %v)", time.Since(start).Seconds(), refused), nil
		}
		refused[st]++
		if time.Now().After(deadline) {
			return d, "", fmt.Errorf("move to %s: %d %s %v (refused %v)", host, st, b, err, refused)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

var faults = map[string]fault{
	// SIGKILL hostd and its workerd; restart a few seconds later. The lease
	// doesn't move.
	"kill-host": {"kill-host", func(n *nemesis, h string) string {
		down := n.between(1*time.Second, 5*time.Second)
		n.c.stop(h, true)
		time.Sleep(down)
		n.c.startHost(h)
		n.c.waitHostsLive(h)
		return fmt.Sprintf("down %v", down)
	}},
	// SIGKILL hostd alone, as an OOM kill or a panic would: its workerds
	// (each in a process group of its own) survive it. Restart hostd and
	// record which of the old workerds are still alive afterwards.
	"crash-hostd": {"crash-hostd", func(n *nemesis, h string) string {
		pid := n.c.pid(h)
		var old []int
		for _, k := range descendants(pid) {
			if strings.Contains(comm(k), "workerd") {
				old = append(old, k)
			}
		}
		n.c.crashOnly(h)
		time.Sleep(n.between(1*time.Second, 3*time.Second))
		n.c.startHost(h)
		n.c.waitHostsLive(h)
		time.Sleep(10 * time.Second)
		var alive []int
		for _, k := range old {
			if syscall.Kill(k, 0) == nil {
				alive = append(alive, k)
				n.c.orphan(h, k)
			}
		}
		return fmt.Sprintf("old workerds %v; still alive 10 s after the restart: %v", old, alive)
	}},
	// SIGKILL workerd only; hostd restarts it.
	"kill-workerd": {"kill-workerd", func(n *nemesis, h string) string {
		k := n.c.killWorkerd(h)
		time.Sleep(3 * time.Second)
		return fmt.Sprintf("%d workerd killed", k)
	}},
	// SIGSTOP the host (hostd + workerd) for less than FenceAfter.
	"pause-short": {"pause-short", func(n *nemesis, h string) string {
		d := n.between(3*time.Second, 20*time.Second)
		n.c.pause(h, false)
		time.Sleep(d)
		n.c.resume(h)
		return fmt.Sprintf("paused %v", d)
	}},
	// SIGSTOP the host past the lease, take over elsewhere, then SIGCONT:
	// the zombie-writer case. The old copy must not act again (D1).
	"pause-takeover": {"pause-takeover", func(n *nemesis, h string) string {
		return n.takeover(h, func() { n.c.pause(h, false) }, func() { n.c.resume(h) })
	}},
	// SIGSTOP only hostd (workerd keeps running) past the lease.
	"pause-hostd-takeover": {"pause-hostd-takeover", func(n *nemesis, h string) string {
		return n.takeover(h, func() { n.c.pause(h, true) }, func() { n.c.resume(h) })
	}},
	// Cut the host off from mgmt (it stays on the tailnet) briefly.
	"cut-short": {"cut-short", func(n *nemesis, h string) string {
		d := n.between(3*time.Second, 20*time.Second)
		n.c.proxies[h].cut(true)
		time.Sleep(d)
		n.c.proxies[h].cut(false)
		return fmt.Sprintf("cut %v", d)
	}},
	// Cut the host off past the lease and take over elsewhere (TestLocksFailover's case, seeded).
	"cut-takeover": {"cut-takeover", func(n *nemesis, h string) string {
		return n.takeover(h, func() { n.c.proxies[h].cut(true) }, func() { n.c.proxies[h].cut(false) })
	}},
	// R2's S3 backend (r2_test.go): cut off from one host, or slow.
	"cut-s3":  {"cut-s3", r2CutS3},
	"slow-s3": {"slow-s3", r2SlowS3},
	// SIGKILL a host in the middle of a large R2 put (r2_test.go).
	"kill-mid-put": {"kill-mid-put", r2KillMidPut},
	// A move of the live workload under load (deploy --replace
	// --force-takeover: a stateful workload can't move without it). The old
	// host stops at its next heartbeat; the new copy starts empty
	// (docs/locks.md "Failure modes").
	"move": {"move", func(n *nemesis, h string) string {
		to := n.other(h)
		d, how, err := n.moveTo(to, true, 60*time.Second)
		if err != nil {
			return "FAILED: " + err.Error()
		}
		return fmt.Sprintf("%s → %s epoch %d, %s", h, d.Host, d.Epoch, how)
	}},
	// mgmt crashes (SIGKILL) or restarts (SIGTERM) and comes back.
	"restart-mgmt": {"restart-mgmt", func(n *nemesis, h string) string {
		crash := n.rnd.Intn(2) == 0
		down := n.between(500*time.Millisecond, 5*time.Second)
		n.c.stop("mgmt", crash)
		time.Sleep(down)
		n.c.startMgmt(false)
		return fmt.Sprintf("crash=%v down %v", crash, down)
	}},
}

// takeover breaks h with fail, moves the workload to another host with
// --force-takeover as soon as mgmt allows, waits a little, and heals h.
func (n *nemesis) takeover(h string, fail, heal func()) string {
	t0 := time.Now()
	fail()
	to := n.other(h)
	d, how, err := n.moveTo(to, false, 120*time.Second)
	if err != nil {
		heal()
		return "FAILED: " + err.Error()
	}
	time.Sleep(n.between(2*time.Second, 8*time.Second))
	heal()
	return fmt.Sprintf("%s → %s epoch %d, takeover %s, healed at t0+%.1fs", h, d.Host, d.Epoch, how, time.Since(t0).Seconds())
}

var scenarios = map[string][]string{
	// R2 (TestR2, r2_test.go): fs and s3 backends, and two replicas on s3.
	"r2-fs":       {"kill-host", "crash-hostd", "kill-mid-put", "cut-short", "cut-takeover", "pause-takeover", "move", "restart-mgmt"},
	"r2-replicas": {"kill-host", "crash-hostd", "kill-workerd", "kill-mid-put", "cut-short", "cut-s3", "slow-s3", "restart-mgmt"},
	"r2-s3":       {"kill-host", "crash-hostd", "kill-mid-put", "cut-short", "cut-takeover", "cut-s3", "slow-s3", "pause-takeover", "move", "restart-mgmt"},

	"queues":  {"kill-host", "crash-hostd", "kill-workerd", "pause-short", "cut-short", "move", "restart-mgmt"},
	"fencing": {"pause-takeover", "pause-hostd-takeover", "cut-takeover", "move"},
	"d1":      {"move", "kill-host", "pause-takeover", "restart-mgmt"},
	"mixed":   {"kill-host", "crash-hostd", "kill-workerd", "pause-short", "pause-takeover", "pause-hostd-takeover", "cut-short", "cut-takeover", "move", "restart-mgmt"},
}

// runNemesis applies faults from the scenario until the deadline, with a
// quiet gap between them.
func (n *nemesis) run(names []string, until time.Time, logf func(string, ...any)) {
	for time.Now().Before(until) {
		time.Sleep(n.between(3*time.Second, 8*time.Second))
		if !time.Now().Before(until) {
			return
		}
		f := faults[names[n.rnd.Intn(len(names))]]
		host := n.tg.get().Host
		start := time.Now()
		detail := f.run(n, host)
		ev := faultEv{Kind: f.name, Host: host, Start: start, End: time.Now(), Detail: detail}
		n.h.fault(ev)
		logf("nemesis: %s on %s (%.1fs): %s", f.name, host, ev.End.Sub(start).Seconds(), detail)
	}
}
