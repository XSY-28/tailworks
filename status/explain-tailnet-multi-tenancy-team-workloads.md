Last update: 2026-11-15 PT. Drafted ADR-team-tenants (Proposed). Design only, no code.

## ADR-team-tenants: several tenants per tailnet, one per team

Draft: `docs/design/adr/team-tenants.md`. The question was how two teams in one
tailnet each manage their own workloads.

What I found in the code, which the ADR builds on:

- mgmt's tailnet gate (`TailnetHandler`, `mgmt/server.go`) gives full admin to
  every caller holding `twd.dev/cap/admin` `[{}]`, across all tenants. The
  registry's user branch also admits any admin to any tenant's images
  (`mgmt/registry.go`).
- Trust credentials and generated `tagOwners` use `tag:twd` as the owner of
  every workload tag (`tagOwnersFor`, the deploy preflight). If two tenants
  shared a tailnet, either one could mint the other's tags.
- The `tenants.tailnet` column has no uniqueness constraint, and I found no
  code that refuses a second tenant on the same tailnet.
- Generated names (`<workload>-<host>`, `svc:<workload>`,
  `tag:twd-wl-<workload>`) would collide between tenants on one tailnet.
- The reconciler's "delete tagged devices twd didn't mint", with the
  tailnet-wide lifecycle credential, would delete another tenant's devices.

The ADR has an implementation plan in four PRs: the scoped admin cap, owner
tags, name prefixes, then docs. It's meant to be implemented later, after the
in-flight PRs land and master CI is stable.

Tests: none run. This is a docs-only change, and the ADR's own wording is the
only content.
