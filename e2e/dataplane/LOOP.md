# Loop: the data plane under faults (chaos + invariants)

Issue #415. The second loop in the `e2e/multimgmt` style, for the data
plane: Queues, Durable Object and lock fencing with a real `tw-hostd`, and
D1 across moves. State lives in `e2e/dataplane/LEDGER.md`. Read it first,
every iteration.

Run one scenario by hand:

```sh
make deps
TW_DP_SCENARIO=fencing TW_DP_SEED=7 TW_DP_DURATION=4m \
  go test -tags e2edataplane -count=1 -v -timeout 25m -run TestDataplane ./e2e/dataplane/
```

`make e2e-dataplane` runs the `mixed` scenario for 4 minutes with
today's date as the seed. `nightly.yml` runs it every night. To replay a
night's run, take the seed from its log (`scenario mixed, seed 20261124`).
`TW_DP_FAULTS=pause-takeover,move` narrows a run to some faults.
`TW_DP_REPLAY=<results dir> go test -tags e2edataplane -run TestReplay
./e2e/dataplane/` re-runs the checkers on a recorded history, so a checker
change can be tried without a new run.

## Hard rules

1. **Hermetic only.** testcontrol, a local DERP, the fake Tailscale API.
   No real tailnet, no real keys.
2. **Only kill what you started.** Every process the harness starts or
   signals (hostd, its workerds, mgmt) is recorded in
   `results/<run>/pids`. Faults signal those PIDs and their descendants,
   never a name pattern.
3. **Budget:** one experiment per iteration, ≤ 15 min wall clock, ≤ 3
   reruns of a seed.
4. **Findings become issues** labelled `dataplane-chaos`, each with the
   seed, the scenario, the violated invariant and the evidence from
   `report.json`. A finding's regression test goes in with its fix.

## The cluster

- `tw-mgmt` (SQLite) on a unix socket. Each of `h1`, `h2` and `h3` reaches
  it through its own proxy, which the nemesis can cut. A cut host stays on
  the tailnet and keeps answering clients: the split-brain case of
  `TestLocksFailover`.
- Three `tw-hostd` processes with the default timings (heartbeat 3 s,
  `FenceAfter` 30 s, lease 45 s), one host per process, so SIGSTOP stalls a
  real host. `-queue-visibility 5s`.
- One workload, `testdata/dp`, holds every primitive, so one lease and one
  fault cover them all: a queue producer and consumer (with a dead-letter
  queue), the lock template, `Reg` (a DO counter), `Pulse` (a DO whose alarm
  makes an egress call every 250 ms), a D1 database and a KV namespace.
- Tailnet nodes: `dp-cli` (the client and the prober), `dp-lka` and
  `dp-lkb` (two lock clients), `dp-store` (the fenced store of
  docs/locks.md), `dp-rec` (the recorder). The consumer and Pulse report
  to the recorder, so the record survives whatever happens to the hosts.

## The nemesis (seeded: `TW_DP_SEED`)

| Fault | What it does |
|---|---|
| `kill-host` | SIGKILL hostd and its workerds; restart it after 1–5 s |
| `crash-hostd` | SIGKILL hostd alone, as an OOM kill or a panic would (its workerds, in process groups of their own, survive it); restart it. The new hostd must reap them (D7, #432) |
| `kill-workerd` | SIGKILL the host's workerds; hostd restarts them |
| `pause-short` | SIGSTOP hostd and its workerds for 3–20 s (under `FenceAfter`), then SIGCONT |
| `pause-takeover` | SIGSTOP the host, `--force-takeover` to another host once mgmt allows it, SIGCONT 2–8 s later |
| `pause-hostd-takeover` | the same, stopping only hostd (workerd keeps running) |
| `cut-short` | cut the host off from mgmt for 3–20 s |
| `cut-takeover` | cut it off, take over elsewhere, reconnect it |
| `skew` | step the host's wall clock (hostd and its workerds) 1–6 min forward or back, within ±8 min, and leave it there for the faults that follow (H3). Linux only: `fencing` and `mixed` get it where the preload library builds |
| `move` | `deploy --replace --force-takeover` of the live workload to another host |
| `restart-mgmt` | SIGKILL or SIGTERM mgmt, restart it after 0.5–5 s |

Scenarios are fault mixes: `queues`, `fencing`, `d1`, `mixed`
(`nemesis_test.go`). The nemesis always targets the current lease holder.

Clock skew: the hosts are a test build (`-tags twtestclock`,
`internal/wallclock`). Each hostd follows the offset in `<work>/<host>/clock`
for its wall-clock reads (tickets, caller seals, MAC timestamps, cron), and
its workerds run with `internal/wallclock/testdata/skewclock.c` preloaded,
which shifts `CLOCK_REALTIME`, `gettimeofday` and `time` by the same
offset. Monotonic clocks aren't shifted. Before the nemesis starts, the
harness checks that the workload's `Date.now()` follows a +90 s step
(`/clock`). The library is Linux only, so on macOS the skew fault is left
out and a seed runs a different fault sequence than on Linux. mgmt (SQLite)
keeps the true clock.

Not yet: multiple mgmt replicas on Postgres are covered by
`e2e/multimgmt`. Joining that harness here is the next step.

## Workloads and the history

Every client op is tagged with a unique ID and timed on the client
(`Sent` before the request leaves, `Recv` when the answer is in). Events go
to `history.jsonl` as they happen. The ops are queue sends (every 7th one
is retried once, every 23rd is poison), Reg increments and reads, probes of
every endpoint ever seen (to catch a copy that should be fenced), D1
inserts, KV puts and gets (`kv_test.go`), and lock acquire/renew/release
with a fenced-store write after each grant and renew.

## Invariants (`check_test.go`)

| ID | Invariant |
|---|---|
| D1 | One live serving copy: once a copy at a newer epoch has answered, no older epoch answers a request sent after that. No older epoch's Pulse egress arrives more than 1 s after the newer copy's first |
| D1-reg | The DO counter, per epoch: increments are unique and increase in real-time order, and a read sees every increment acknowledged before it was sent. Writes survive a restart on the same host. Across a move the copy starts empty (documented) |
| D2 | Fencing: two lock holders never overlap within an epoch, or across a takeover of a stalled or cut-off host. Tokens only go up. The store never accepts a token lower than one it has accepted |
| D3 | Queues: every acknowledged send is delivered. No two deliveries of a message carry the same `attempts`. Outside a fault window, no attempt is counted for a delivery that never reached `queue()`. Retried messages come back. Poison stops at `max_retries + 1` and reaches the dead-letter queue |
| D4 | Every acknowledged D1 insert is there at the end |
| KV | Only what docs/kv.md documents: a read sees the workload's own last acknowledged write (or a later one) and nothing never written, across restarts, moves and takeovers, and after healing. A key put with `expirationTtl` 60 s is there until it expires and gone after, in real time shifted by the writer's and reader's clock offsets |
| D5 | Convergence: after healing, the deployment is active and serving within 60 s, both queues drain within 120 s, and the lock answers |
| D6 | Availability: reported. Gated only outside fault windows (+60 s): ≥ 90% of ops succeed |
| D7 | No workerd outlives its hostd: 10 s after a `crash-hostd` restart, none of the crashed hostd's workerds is still running (#432) |

## Each iteration

1. Read `LEDGER.md` and pick one item: a harness fix, an open finding to
   re-check, or the next seed of a scenario that has fewer than 3
   consecutive clean seeds.
2. Run it. Artifacts go to `results/<stamp>-<scenario>-<seed>/`:
   `history.jsonl`, `report.json`, `faults.json`, `store.json`, the
   process logs and `pids`.
3. Classify the result: `CONFIRMED` (with the invariant and evidence),
   `REFUTED`, `FLAKY` (rerun the seed up to 3 times), or `HARNESS-BROKEN`.
4. For CONFIRMED, find the root cause (`file:line`), file a
   `dataplane-chaos` issue, and add it to the ledger.
5. Update the ledger's status table and run log.

**Done** (issue #415): every scenario passes 3 consecutive seeds, plus 2
clean 10-minute `mixed` soaks; every confirmed finding has a deterministic
regression test and a ledger entry.
