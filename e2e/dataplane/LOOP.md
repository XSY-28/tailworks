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
  makes an egress call every 250 ms), and a D1 database.
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
| `move` | `deploy --replace --force-takeover` of the live workload to another host |
| `restart-mgmt` | SIGKILL or SIGTERM mgmt, restart it after 0.5–5 s |

Scenarios are fault mixes: `queues`, `fencing`, `d1`, `mixed`
(`nemesis_test.go`). The nemesis always targets the current lease holder.

Workflows and approvals (#418) have their own test, workload and checkers
(W1–W5): see [ledger/workflows.md](ledger/workflows.md).

Not yet: wall-clock skew on a host. `TW_TEST_WALL_CLOCK_OFFSET` only exists
in `tw-mgmt` (and with SQLite mgmt uses the local clock). hostd fences on its
monotonic clock and workerd reads the wall clock directly, so skewing a
host needs a hook in both. Multiple mgmt replicas on Postgres are covered by
`e2e/multimgmt`. Joining that harness here is the next step.

## Workloads and the history

Every client op is tagged with a unique ID and timed on the client
(`Sent` before the request leaves, `Recv` when the answer is in). Events go
to `history.jsonl` as they happen. The ops are queue sends (every 7th one
is retried once, every 23rd is poison), Reg increments and reads, probes of
every endpoint ever seen (to catch a copy that should be fenced), D1
inserts, and lock acquire/renew/release with a fenced-store write after
each grant and renew.

## Invariants (`check_test.go`)

| ID | Invariant |
|---|---|
| D1 | One live serving copy: once a copy at a newer epoch has answered, no older epoch answers a request sent after that. No older epoch's Pulse egress arrives more than 1 s after the newer copy's first |
| D1-reg | The DO counter, per epoch: increments are unique and increase in real-time order, and a read sees every increment acknowledged before it was sent. Writes survive a restart on the same host. Across a move the copy starts empty (documented) |
| D2 | Fencing: two lock holders never overlap within an epoch, or across a takeover of a stalled or cut-off host. Tokens only go up. The store never accepts a token lower than one it has accepted |
| D3 | Queues: every acknowledged send is delivered. No two deliveries of a message carry the same `attempts`. Outside a fault window, no attempt is counted for a delivery that never reached `queue()`. Retried messages come back. Poison stops at `max_retries + 1` and reaches the dead-letter queue |
| D4 | Every acknowledged D1 insert is there at the end |
| D5 | Convergence: after healing, the deployment is active and serving within 60 s, both queues drain within 120 s, and the lock answers |
| D6 | Availability: reported. Gated only outside fault windows (+60 s): ≥ 90% of ops succeed |
| D7 | No workerd outlives its hostd: 10 s after a `crash-hostd` restart, none of the crashed hostd's workerds is still running (#432) |

## Cron triggers and alarms (issue #419)

`TW_DP_SCENARIO=cron` or `alarms` (`make e2e-dataplane-cron` runs both)
replaces `dp` with two workloads of its own in `testdata/cron`, on the
same cluster and nemesis (`cron_test.go`, checkers in
`check_cron_test.go`, ledger in `ledger/cron-alarms.md`):

- `cron`, stateless: three cron triggers. Nothing fences a stateless
  workload, so only hostd's heartbeat-freshness rule keeps a cut-off host
  from running them.
- `cronobj`, stateful: the same triggers, plus alarm chains on a plain DO
  (`Alm`), a facet (`FAlm`, whose alarm tailworks delegates to the Host DO)
  and an Agents SDK agent (`Sch`, `scheduleEvery(2)`). The client re-arms
  them on every new epoch, as storage doesn't move.

Every run reports `start`, a `tick` every 5 s while held, and `end` to
the recorder. `*/1 * * * *` holds every run 75 s, and `0-59 * * * *` holds
70 s in `ctx.waitUntil()` on minutes divisible by 3, so the next minute
comes due while a run is in flight. A move or takeover moves both
workloads. The stateless one goes without `--replace` on a takeover, so
mgmt waits out the old host's lease. Four extra faults:

| Fault | What it does |
|---|---|
| `clock-back` | step every host's wall clock back 70–150 s, hold it 70–100 s (half the time restarting hostd half way), then step it forward again. The binaries are a `twtestclock` test build, and hostd follows `TW_TEST_WALL_CLOCK_FILE` (#564). workerd keeps the true clock: cron is due by hostd's |
| `clock-forward` | the same, stepped forward 70–150 s |
| `pause-boundary` | SIGSTOP the host 2–8 s before a minute boundary, for 10–20 s (under `FenceAfter`) or 32–40 s (past it, under the lease) |
| `reschedule` | redeploy both workloads in place with `* * * * *` swapped for `*/2 * * * *`, or back |

| ID | Invariant |
|---|---|
| C1 | At most one run per (workload, trigger, scheduled minute), across all hosts. Notes, not failures: a rerun after hostd restarted under a clock step (#594), and a rerun on the host a workload moved to, with a clock step between (#600) |
| C2 | No two runs of one trigger overlap: no run reports anything more than 1 s after a later run of the trigger started |
| C3 | No run starts on a host cut off from mgmt for more than `FenceAfter` (by `scheduledTime`), or on a host after the workload was seen active elsewhere (5 s for a live move). Every due minute runs, unless the trigger's previous run is in flight or the minute is in a fault window (+30 s) |
| C4 | After a schedule change is active, a trigger only the old schedule has never fires, and a new one fires at its first matching minute |
| A1 | No alarm fires on a host after the workload was seen active elsewhere, and no alarm (chain, seq) fires on two hosts. A set alarm fires unless the object moved or was re-armed before it was due (extended over fault windows). `scheduleEvery` has no gap over 10 s outside fault windows |
| C5 | Convergence: after healing, both workloads are active, every trigger runs and every alarm class fires |

`TestCronCheckers` plants one violation of each in a synthetic history.
`TW_DP_REPLAY=<dir> go test -tags e2edataplane -run TestReplayCron
./e2e/dataplane/` re-checks a recorded run.

Not yet: hosts whose clocks *differ* (each host its own offset). The
harness gives every process the same environment, so the clock faults step
every host together. #600 already shows the cross-host rerun.

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
