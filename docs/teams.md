# Teams: tenant admins

Several teams can share one twd install and one tailnet. Give each team its
own tenant, then grant the team `twd.dev/cap/tenant-admin` for that tenant.
A tenant admin manages its own tenant's workloads, and nothing else: not
other teams' tenants, not hosts, not releases or upgrades, and not the
tenant's own limits.

The design is
[ADR-team-tenants](design/adr/team-tenants.md), tracked in issue #80.

## Grant a team its tenant

In the tailnet policy file, grant the team the capability on
`tag:twd-mgmt`, naming its tenants and what it may do there:

```jsonc
"grants": [
  {"src": ["group:payments"], "dst": ["tag:twd-mgmt"], "ip": ["tcp:443", "tcp:80"],
   "app": {"twd.dev/cap/tenant-admin": [
     {"tenants": ["tn_payments"], "actions": ["read", "deploy", "kill", "secrets", "data"]}
   ]}}
]
```

`tenants` are tenant IDs (`twd tenant ls` as an admin). Both fields are
required. A value without `tenants` or `actions`, with a wildcard, an
unknown action or an unknown field grants nothing. `twd whoami` and
`GET /v1/whoami` (`invalidTenantGrants`) say why.

| Action | Lets the team |
|---|---|
| `read` | list and read its deployments, objects, logs, audit records, stores, queues, secrets' names, public routes, telemetry and notification settings; pull its images |
| `deploy` | deploy, roll back, promote previews, undeploy, change public routes, release public names, set telemetry and notifications; push its images |
| `kill` | kill its objects and set their TTL |
| `secrets` | set and delete secret values |
| `data` | read and write its data through mgmt: KV, D1, store journals, queue messages, and create or delete stores and queues |

**Each value is checked on its own.** A request is allowed only if one
single value names both the tenant and the action.
`{"tenants": ["a"], "actions": ["read"]}` plus
`{"tenants": ["b"], "actions": ["deploy"]}` doesn't allow deploying to `a`.

## What a team sees

- `twd link` works for a tenant admin and prints the tenants and actions.
  `twd deploy`, `twd ls`, `twd logs`, `twd kill` and `twd audit` work in
  the granted tenants.
- Lists (`GET /v1/tenants`, `/v1/deployments`, `/v1/objects`,
  `/v1/audit`) show only the granted tenants. A team with `read` in
  several tenants names one with `?tenant=`.
- Asking for a tenant it wasn't granted is `403 tenant_forbidden`, with the
  grant to add. Another tenant's deployment or object, by ID, is
  `404 not_found`, so its existence doesn't leak.
- Admin-only routes are `403 tenant_forbidden`: hosts (create, cordon,
  drain, delete), creating, changing or deleting tenants, purging all of a
  tenant's objects, releases, upgrades, `GET /v1/status`, container
  reservations and pushing operator base images.
- The admin console (`/ui`) and twd's MCP server (`/mcp`) aren't open to
  tenant admins yet.
- Approvals are unchanged: deciding them needs `twd.dev/cap/approve`
  ([approvals.md](approvals.md)).

## Admins

`twd.dev/cap/admin` is unchanged: an admin can do everything, in every
tenant. Holding both capabilities makes a caller an admin; a tenant-admin
value never narrows an admin.

Use a separate capability, not values on `twd.dev/cap/admin`. Versions of
twd-mgmt before this one treat *holding* `twd.dev/cap/admin` as full admin
and ignore its values, so a value meant to limit a team would make it an
admin there. Those versions don't know `twd.dev/cap/tenant-admin`, so they
refuse it.

## Not yet

Two teams' tenants on one tailnet aren't fully separated on the data plane
yet. Until the rest of the ADR lands, their workloads' tags share an owner
(`tag:twd`), so either tenant's OAuth client could mint the other's tags,
and workload names can collide. Separate tailnets, or one trusted team per
tailnet, are the safe setups for now. See the ADR's implementation plan.
