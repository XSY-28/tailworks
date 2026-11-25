# Cron triggers and DO alarms: ledger

Issue #419. Process: `../LOOP.md`, section "Cron triggers and alarms".
Scenarios `cron` and `alarms` (`TW_DP_SCENARIO`, `make
e2e-dataplane-cron`). Findings are filed as GitHub issues labelled
`dataplane-chaos`.

## Status

| Scenario | Clean seeds (consecutive) | Notes |
|---|---|---|
| cron | RUNS_CRON | C2 notes #566 (a stateless run on a host taken over) instead of failing on it |
| alarms | RUNS_ALARMS | |

Not covered yet: stepping a host's wall clock back and forward (C1's
clock cases). It waits for the host clock-skew hook from #415's last slice
(branch `dataplane-415-finish`, no PR yet on 2026-11-25). When the hook
lands, add `clock-back` and `clock-forward` faults to the `cron` scenario.

## Findings

| ID | Verdict | Invariant | Issue | Summary |
|---|---|---|---|---|
| C-F1 | CONFIRMED, needs a decision | C2 (no overlap) | #566 | A **stateless** workload's cron run in flight on a host that is cut off from mgmt (or stalled) past the lease goes on after the takeover, beside the new host's next run of that trigger. `cronLeaseFresh` (`hostd/cron.go`) only stops a stale host *starting* runs, and nothing fences a stateless workload. Seen 4 times in each of 2 runs of cron seed 2, 1 in cron 4, 3 in alarms 5 (macOS). The stateful workload had no overlap once #556 was in. The fix trades long jobs surviving a mgmt outage against no-overlap across hosts, so it waits for Ross. C2 records these as notes until then |
| C-F2 | CONFIRMED, FIXED by #556 | A1, C2 (stale side effects) | #501 | Before #556, a host stalled past the lease and resumed after the takeover made alarm and cron egress calls after the new copy's first: 24 stale alarm fires (cron 2) and 18 (alarms 3, all after `pause-hostd-takeover`), and one stateful cron run still reporting 6 s after the new host's run began. That's F3 (#501), seen here on macOS, where the dp loop never saw it. With #556 merged, the same seeds give 0 stale fires and 0 stateful overlaps |

## Harness notes (not product bugs)

- The stale-host check excused the old host for a `move`'s 5 s but
  flagged the *new* host's first alarms, which can come before the watcher
  sees the deployment active. It now accepts any host the workload was
  seen on in [t − slack, t + 3 s]. `TestCronCheckers` covers it.
- An alarm `set` made by a stale copy (after the move) can never fire
  (that storage is gone). It's counted under stale fires, not "never fired".
- The workload's reports to the recorder are lossy if egress fails: a
  `crash-hostd` right as an alarm came due lost that alarm's `fire` report,
  though the alarm ran (seq 11 followed): cron 4's first run, 2 "never
  fired". `alarm()` now throws when the `fire` report doesn't get through,
  so workerd retries it.

## Run log

| Run (results dir) | Scenario / seed | Verdict |
|---|---|---|
| 20261125T062341Z-cron-1 | cron 1, 90 s smoke, `reschedule,kill-workerd` | pass. 10 schedule changes |
| 20261125T062743Z-cron-2 | cron 2 (before #556) | C2: 4 stateless overlaps (C-F1) and 1 stateful one, A1: 24 stale fires (C-F2) |
| 20261125T062743Z-alarms-3 | alarms 3 (before #556) | A1: 18 stale fires after `pause-hostd-takeover` (C-F2), 2 "never fired" (a checker gap, fixed) |
| 20261125T063630Z-cron-2 | cron 2, merged with #556 | pass on replay with the #566 note (4 known overlaps). 0 stale fires |
| 20261125T063650Z-alarms-3 | alarms 3, merged with #556 | pass |
| 20261125T064540Z-cron-4 | cron 4 | A1: 2 "never fired": HARNESS (a lost report, fixed). 1 known #566 overlap |
| 20261125T064555Z-alarms-5 | alarms 5 | pass. 3 known #566 overlaps |
| Linux arm64 (Lima), cron 11 | cron 11 | pass. 3 stalled-host takeovers |
