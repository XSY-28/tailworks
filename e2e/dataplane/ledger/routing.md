# Routing loop ledger

Issue #422. Process: `LOOP.md` ("Routing" for this loop's workloads,
faults and invariants N0–N6). Findings are filed as GitHub issues labelled
`dataplane-chaos`.

## Status

| Scenario | Clean seeds | Notes |
|---|---|---|
| public | RUNS_PUBLIC | |
| bindings | RUNS_BINDINGS | |
| websockets | RUNS_WEBSOCKETS | |
| routing | RUNS_ROUTING | |

Linux (lima twd-dev, arm64): `public` 11, `bindings` 11, `websockets` 11
and `routing` 11 are clean. Nightly runs `make e2e-dataplane-routing`
(`routing`, today's date as the seed).

## Findings

| ID | Verdict | Invariant | Issue | Summary |
|---|---|---|---|---|
| R1 | FIXED (#579) | N1, N5 | #572 | A fenced stateful workload kept answering WebSockets that were already open for up to 3 s after its lease was revoked. `undeploy` stopped workerd first (SIGTERM, then up to 3 s of graceful drain) and reset the nodes' netd flows only after that; #501's stall fence gates new requests, not frames on an upgraded connection. After a takeover of a stalled host the old copy answered 105 (routing 1) and 159 (websockets 1) messages sent after the new copy was serving. Fix: close a fencing workload's nodes before stopping workerd. Regression: `hostd:TestFenceClosesOpenWebSockets` (closed 3.0 s after the revoke with 122 late answers → at the revoke, none) |
| R2 | CONFIRMED, open | N0 | #593 | Redeploying a workload after an undeploy onto a host that once held it never gets ready: the host rejoins the entry node from saved state, but mgmt purged that object at the undeploy and told only the lease holder, so the ready report is `not_found` and nothing re-creates it. Seen on Linux (routing 11) and macOS (bindings 2); deterministic in `TestRedeployAfterUndeploy` (skipped until fixed; `TW_DP_KNOWN_FAILURES=1`). The fix changes hostd's object-identity path (re-confirm a non-live object with mgmt, keep fail-static restarts), so it waits for a decision. `revoke-binding` now redeploys onto the host it undeployed from |

## Harness notes (not product bugs)

- **testcontrol keeps duplicate names.** mgmt deletes a moved object's
  previous device through the API, which the fake API ignores, and a node
  rejoining from saved state can come back as a new testcontrol node while
  the old one stays. Several nodes then share one MagicDNS name, and
  netd's name lookup (`resolveEgress`) takes the first match from an
  unordered map, so after a move back a binding call could go to a dead
  node and hang until its TCP connect timed out (a binding call took 132 s
  and failed `dial_failed`). Real control keeps names unique. The harness
  now keeps them unique in testcontrol (`dedupeNames`): the node that
  started last owns the name. The first version renamed nodes for good,
  which broke a public node that rejoined as the same node (`public` 1:
  `public_name_taken`, N0); it is a rerun.
- **Reordered WebSocket replies** in the first `routing` run were the
  workload's doing: `webSocketMessage` awaited an RPC (`tailnet.self()`),
  which lets the next message in. The Echo object now reads its identity
  once, in `blockConcurrencyWhile`.
- **N4 and the deployment watcher.** The watcher polls mgmt every 300 ms,
  so a `/gate` request answered `not_public` right after a route change
  could be judged against the previous listing (routing 2). Requests in a
  route change's window (+10 s) are no longer judged; routing 2 is clean
  on replay.
- **Binding latency after B moves** is 0–9 s in clean runs (cache TTL 5 s
  plus a first path to the new node, `PeerPathWait` 5 s); N1 bounds it at
  20 s.

## Run log

| Run (results dir) | Scenario / seed | Verdict |
|---|---|---|
RUN_LOG
