Last update: 2026-11-23 PT. Drafted `ADR-exit-node-egress` (Proposed).

# deranged-dog: internet egress through a tailnet exit node

## What's here

- `docs/design/adr/exit-node-egress.md`: Proposed ADR. A host's internet
  path gains `exit-node=<node>` (`tw up --worker-internet=...`,
  `tw-hostd -worker-internet=...`, `-container-internet=exit-node`),
  carried by the host's own tsnet node (`tw-host-<id>`, `tag:tw-host`)
  with `ExitNodeID` set through `EditPrefs`. A workload can require it
  with `tailnet.egress.via: "exit-node"`, enforced at placement by a new
  host feature `internet-exit-node`. The SSRF guard stays in netd (it
  still resolves, checks every answer and dials the IP literal); names
  resolve through the exit node's peer-API DNS, never the host resolver.
  New error code `exit_node_unavailable`. No code yet.

## Findings that shaped it

- tsnet documents exit nodes (`tsnet.go` "Using an exit node"):
  `Server.Dial` of a public IP goes through `PeerForIP`, which falls
  through to the route table where the selected exit node owns the
  default routes (`ipn/ipnlocal/resolve.go`), so the dial is netstack over
  WireGuard. Verified in the pinned tailscale.com module.
- tsnet's `Dial(name)` resolves with its own resolver hook, which bypasses
  tsdial's exit-node DoH path; hence netd resolves itself against the exit
  node's peer API and dials the IP.
- `hostd.features()` is already config-dependent (`FeatureSelfUpgrade`),
  so a config-dependent `internet-exit-node` feature fits the existing
  placement gate (ADR-041 §7).
- ADR-049's objections to "an exit node per workload" (object identity,
  per-tenant grants, guard moving off the host) don't apply when the
  carrier is the host node.

## Commands run

- `go test ./docs/`: ok (the slug file is logged, not failed, outside CI).
- `CI=true go test ./docs/ -run TestADRIndex`: FAIL, as designed: the
  slug file must be numbered by the PR that merges it (AGENTS.md rule 5).
  Not a problem for review on the branch; the PR must number it before
  CI goes green.

## Next

- Review the ADR; settle the three open questions.
- Number it on merge (`git grep ADR-exit-node-egress`), rename the file,
  add the README row.
- Build: `hostd/exitnode.go`, `internal/features`, `bundle` `via`,
  `cmd/tw/up_workerinternet.go` values, `docs/errors.md`,
  `docs/tailworks-jsonc.md`, `docs/security-model.md`.
