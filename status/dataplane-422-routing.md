# dataplane-422-routing

Issue #422: the data-plane chaos loop for routing (public ingress, service
bindings, WebSockets across moves). Ledger: `e2e/dataplane/ledger/routing.md`.
Process and invariants: `e2e/dataplane/LOOP.md` "Routing".

## What's here

- `e2e/dataplane/routing_test.go`: `TestRouting`, three stateful workloads
  (`testdata/routing/{a,b,ws}`), a public client on netd's public
  listener, a tailnet caller, WebSocket clients, a recorder; the routing
  nemesis (`move`, `paths`, `revoke-binding`, `hibernate`, takeovers, and
  TestDataplane's host faults); `dedupeNames` keeps testcontrol's MagicDNS
  names unique.
- `e2e/dataplane/check_routing_test.go`: N1–N6 and `TestRoutingReplay`.
- `e2e/dataplane/redeploy_test.go`: `TestRedeployAfterUndeploy`, the
  reproduction of #593 (skipped until it's fixed).
- `tw-hostd -test-public-listen-dir` (`internal/hostdrun/testfunnel.go`;
  `-local-dev` only): public nodes listen on loopback, so a hermetic test
  drives netd's public listener as Funnel would.
- `make e2e-dataplane-routing`, in nightly (30 min).
- Docs cite the tests: `docs/public-ingress.md` "Under faults",
  `docs/wrangler.md` service bindings ("When the target moves"), and the
  COMPAT rows for WebSockets, Hibernation, Service bindings and Public
  routes.

## Findings

- #572 (fixed in #579, merged into this branch): a fenced stateful
  workload kept answering open WebSockets for up to 3 s after its lease
  was revoked.
- #593 (open, needs a decision): redeploying after an undeploy onto a host
  that once held the workload never gets ready.

## Results

RESULTS

## Tests run

TESTS
