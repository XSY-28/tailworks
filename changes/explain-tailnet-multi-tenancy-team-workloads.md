---
kind: security
upgrade: [protocol, cli-json]
---
Tenant admins: a team can manage only its own tenants. Grant it
`twd.dev/cap/tenant-admin` on `tag:twd-mgmt` with values like
`{"tenants": ["tn_payments"], "actions": ["read", "deploy", "kill"]}`
(actions: `read`, `deploy`, `kill`, `secrets`, `data`). A tenant admin can
`twd link`, deploy, list, read logs and audit, and kill in those tenants.
Hosts, tenant settings, releases, upgrades, the admin console and `/mcp`
stay with admins. Refusals are `403 tenant_forbidden` with the grant to
add; another tenant's deployment or object is `404`. `twd whoami` shows
what the grants allow, and why an invalid value grants nothing. See
docs/teams.md.

Fix: a tailnet user holding only `twd.dev/cap/approve` could push and
pull every tenant's images in mgmt's registry. The registry now admits only admins, hosts and tenant admins.

Operator action: none. Don't scope a team by adding values to
`twd.dev/cap/admin`; older mgmts ignore those values and treat the holder
as a full admin.
