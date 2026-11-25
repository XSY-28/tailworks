# Data-plane loop ledger

Issue #415. Process: `LOOP.md`. Findings are filed as GitHub issues
labelled `dataplane-chaos`.

Deploys, rollback and secrets (#421, scenarios `deploys-hosts`, `deploys-mgmt`, `deploys-mixed`): [ledger/deploys.md](ledger/deploys.md).
Workflows and approvals (#418, scenarios `workflows`, `workflows-move`): [ledger/workflows.md](ledger/workflows.md).

## Status

| Scenario | Clean seeds (consecutive) | Notes |
|---|---|---|
| queues | 1, 2, 5, 6 | Attempt gaps inside fault windows: see F1 (#431) |
| fencing | 2, 7 | Seed 2 counts only the rerun with safe takeovers |
| d1 | 4 (on replay after the checker fix) | |
| mixed | 3, 8, 9 (macOS) | Linux: 20261125 failed D1 before the F3 fix (#501) |
| 10-minute mixed soak | none yet | |

The issue's stop condition is not met yet: 3 consecutive seeds on every
scenario (d1 has 1) and 2 clean 10-minute soaks. Nightly runs `mixed` with
the date as the seed.

## Findings

| ID | Verdict | Invariant | Issue | Summary |
|---|---|---|---|---|
| F1 | FIXED (#431) | D3 (attempts count deliveries) | #431 | A hand-back (`Release`) from a host that has just lost the lease is refused with `lease_lost`. The batch waits out the visibility timeout and is counted as an attempt it never ran. Poison can be dead-lettered after fewer real runs than `max_retries + 1`. Root cause: `mgmt.AckQueue` checks the current epoch before it looks at a release-only settle. Refused 1–9 times per run, every time on the host that had just lost the lease |
| F3 | FIXED (#501) | D1 (one live copy: side effects) | #501 | A host stalled (SIGSTOP of hostd alone, or of the whole host) past the lease, then resumed after a `--force-takeover`, made DO egress calls 2.7 s and 4.8 s after the new copy's first. hostd fences only after a *failed* heartbeat (`hostd/host.go` `tick`). On resume the first heartbeat succeeds and carries the revoke, and until it's processed nothing gates stateful egress or ingress on heartbeat freshness (cron does, `hostd/cron.go`). Not seen on macOS in 26 takeovers. Fix: a stateful workload's ingress and egress are gated on heartbeat freshness (`hostd/stallfence.go`), and freshness counts from when the heartbeat was sent. Regression: `hostd:TestStallFence`, `TestStallFenceAnswerFromBeforeStall` |
| F2 | FIXED (#432) | D1 (one live copy), resources | #432 | workerd runs in its own process group, so it outlives a hostd crash (SIGKILL / OOM / panic), and no later hostd reaps it: 8 of 8 `crash-hostd` faults left the old workerd running. systemd's `KillMode=mixed` covers Linux service hosts. launchd, `tw up --separate-host`'s supervisor and `tw dev` don't. Two workerds sharing one DO's storage is a hypothesis, not yet shown. Also: `e2e/queues` step 3 believes it kills workerd and doesn't |

## Hypotheses

| ID | Status | Hypothesis | Result |
|---|---|---|---|
| H1 | CONFIRMED on Linux → F3 | A host SIGSTOPped past its lease, resumed after a takeover, answers or acts (Pulse alarm egress) before its first heartbeat tells it the lease is gone. hostd fences only after a *failed* heartbeat (`hostd/host.go` `tick`) | No stale answer or side effect in 26 takeovers of a stopped or cut-off host (fencing 2, 7; d1 4; mixed 3, 8, 9). On SIGCONT the first heartbeat succeeds and carries the revoke first. The window is real in principle (a request that reaches the ingress before that heartbeat returns). Keep it in the mix. **On Linux it shows:** the first CI run (ubuntu-24.04, mixed 20261125) had 2 stale side effects, 2.7 s and 4.8 s after the new copy's first: F3, #501 |
| H2 | OPEN | Two workerds on one DO's storage after a hostd crash (F2) corrupt or lose DO writes | Next: keep the orphan alive, drive `Reg` increments and Pulse alarms on the new copy, then compare storage |
| H3 | OPEN | Wall-clock skew on a host doesn't matter (ADR-028) | Needs a skew hook in hostd and workerd (see LOOP.md "Not yet") |

## Harness notes (not product bugs)

- `--replace --force-takeover` moves the lease at once, even off a host that
  is cut off or stopped. The first fencing run used it for takeovers and D1
  failed (78 stale answers, 132 stale side effects, lock holders
  overlapping). That's the documented failure mode in docs/locks.md
  ("`tw deploy --replace --force-takeover` on a live host"). Takeovers now
  use `--force-takeover` alone, which waits for the lease, as
  `TestLocksFailover` does. The run shows the checkers catch a real split
  brain.
- `faultAt` picked the first fault whose widened window held the time, not
  the latest. A `move` right after a `pause-takeover` was blamed on the
  takeover (d1 seed 4, 3 false D2 overlaps). Fixed. `TestReplay` on that
  run's history is clean.
- A stateful workload can't move without `--force-takeover`
  (`move_unconfirmed`). The `move` fault uses `--replace
  --force-takeover`.
- hostd's workerds are not in its process group. Host-wide faults signal
  hostd's descendants (`descendants()`), and the `crash-hostd` fault
  deliberately doesn't.

## Run log

| Run (results dir) | Scenario / seed | Verdict |
|---|---|---|
| 20261125T015744Z-queues-1 | queues 1, 60 s smoke | pass. Harness fixes after it: workerd lookup, stateful moves, ack-before-throw consumer |
| 20261125T020048Z-fencing-2 | fencing 2 | HARNESS-BROKEN: replace+force takeovers (see notes) |
| 20261125T020048Z-queues-2 | queues 2 | pass. F1 seen (7 refused hand-backs) |
| 20261125T020609Z-fencing-2 | fencing 2 | pass. 6 safe takeovers at 43–48 s |
| 20261125T021247Z-mixed-3 | mixed 3 | pass |
| 20261125T021247Z-d1-4 | d1 4 | D2 false positive (faultAt). Clean on replay after the fix |
| 20261125T021247Z-queues-5 | queues 5 | pass. F1 (8 refused hand-backs) |
| 20261125T022506Z-queues-6 | queues 6, crash-hostd + move | pass on the invariants. F2: 8 of 8 crashes left workerd running |
| 20261125T022948Z-fencing-7 | fencing 7, pause-hostd-takeover + pause-takeover | pass. 6 takeovers of a host whose hostd was stopped and workerd kept running |
| 20261125T022948Z-mixed-8 | mixed 8 | pass. F2 seen again (2 of 2) |
| 20261125T022948Z-mixed-9 | mixed 9 | pass. F2 seen again (1 of 1) |
| fix-431 branch, queues 5 | queues 5 | pass. Refused hand-backs 8 → 0 (of 11), attempt gaps 38 → 22 (the rest are crashes and pauses) |
| make e2e-dataplane (macOS, merged tree) | mixed 20261125 | pass |
| CI ubuntu-24.04, nightly dispatch run 45535497016 | mixed 20261125 | **D1 fail: F3** (2 stale Pulse side effects after takeovers of stalled hosts) |
| fix-432 branch, queues 6 | queues 6, crash-hostd + move | pass. Workerds alive 10 s after a hostd crash and restart: 8 of 8 → 0 of 7 (D7 now gates it) |
| fix-501 branch, Linux arm64 (Lima) | fencing 20261125, pause-takeover + pause-hostd-takeover, 5 min | pass. 6 takeovers of a stalled host, 7 epochs, 0 stale answers, 0 stale side effects (of 371 pulses) |
