Last update: 2026-11-25 11:00 PT. Three test flakes fixed at their cause (#429, #417, #541); test-only.

# fix-netd-mgmt-flakes

## #429 netd TestKeepEarlyAnswerNoAnswer (first POST took 12 s)

No timer in the proxy path. A scratch test with an upstream that sleeps
before closing (0 s, 2 s) showed the 503 follows the upstream's close by
under 2 ms every time (2.017 s for a 2 s sleep): `UnixUpstream` +
`KeepEarlyAnswer` wait on nothing but the close. 1,800 runs of the test
(`-race -count=300 -cpu 1,2`, 6 at once) and 6 full `./netd` runs under
12 `yes` hogs never had a POST over 500 ms. The 12 s was the upstream
goroutine not running (a loaded `make test` on macOS), and the test timed
from the POST. It now times from the upstream's close, so a proxy that
waits on a timer after the close still fails, and a late upstream doesn't.

## #417 mgmt TestWarmKeysRevokedOnCloseInParallel

The budget (1.6 s) covered all of `Close`: `FlushDeletes`,
`unregisterReplica` (up to 2 s), waiting for the loops, then the
revocations. The test now counts concurrent key DELETEs at the fake API
(`keyDeletesPeak`) and requires at least 2. Checked that it catches the
regression: with `warmRevokeParallel = 1` it fails ("at most 1 DELETE in
flight: revoked one by one?"); with 8 it sees 8.

## #541 mgmt TestWorkloadReplicas (no_host: h1, h2, h3 not live)

Hosts registered once and never heartbeat, so they were live for
`HostLeaseTTL` (10 s) only; a loaded run is 21 s. The test now uses
`keepHostsLive` (the #183 helper). Each subtest gets its own client
(`replicasClientFor`), so a failure is reported by the subtest, not as
`panic(nil) or runtime.Goexit` from a FailNow on the parent. Reproduced:
an 11 s sleep before `UndeployStopsReplicas` gives the exact CI error
without the fix and passes with it.

## Tests

See the PR body.
