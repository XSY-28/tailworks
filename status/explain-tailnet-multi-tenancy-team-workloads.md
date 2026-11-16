Last update: 2026-11-15 PT. ADR-team-tenants revised: team access is a new `twd.dev/cap/tenant-admin` cap, and route scope goes in `x-twd-role`. Tracked in #80.

## Revision: a new cap instead of values on `twd.dev/cap/admin`

`allowed` (`mgmt/server.go`) treats holding `twd.dev/cap/admin` as full admin
and ignores its values. If scope were a value on that cap, a mgmt that
predates the ADR would read a team-scoped grant as full operator admin,
hosts included. The draft now uses `twd.dev/cap/tenant-admin` (unknown to old
versions, so they refuse it) and keeps `twd.dev/cap/admin` operator-only.
MCP and approval scoping are sub-values of the tenant-admin cap, for the same
reason. Route scoping extends the existing `x-twd-role` (read at runtime by
`OperationRoles`, enforced by `spec_test.go`) with `tenant:<action>`, instead
of adding a new `x-twd-scope` field.

Who manages hosts, today and after the ADR: only admins (operators). Host,
pool, release and upgrade routes are `x-twd-role: admin`, and the ADR keeps
them closed to tenant-admins.

## First draft: several tenants per tailnet, one per team

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
