# ADR-team-tenants: Several tenants per tailnet, one per team: tenant-scoped admin grants, a per-tenant owner tag, and per-tenant name prefixes

**Date:** 2026-11-15
**Status:** Proposed (results in `status/explain-tailnet-multi-tenancy-team-workloads.md`).
Not implemented; tracked in issue #80. The implementation plan is at the end.

**Context.**

- The question: how can two teams in one tailnet each manage their own
  workloads, without being able to deploy, kill, read or impersonate the
  other team's?
- In the RFC (§4.3, identity table), a tenant *is* a tailnet. In code it's a
  row with a `tailnet` column (`mgmt/sqlstore.go`). Nothing enforces one
  tenant per tailnet, and nothing is designed for more than one either.
- Everything that isolates tenants is already per tenant: `netd[tenant]` and
  `stord[tenant]` on each host, the per-tenant bucket prefix, the OBO root
  key, the WIF trust credentials and pinned subjects, the namespace leases,
  the quotas and the pool (ADR-020). By default, plain DO classes get
  `isolation: "tenant"`, which means one VM per tenant.
- mgmt access is all or nothing. `TailnetHandler` (`mgmt/server.go`) admits
  a tailnet user only as an admin (`twd.dev/cap/admin` from a grant to
  `tag:twd-mgmt`, or `autogroup:admin`), or as an approver or an MCP
  caller. An admin can act on every tenant, every host and every release.
  `registryAuthorize` lets any user principal pull any tenant's images.
- Tag ownership isn't separated either. Every trust credential is scoped to
  `tag:twd` (RFC §4.4), and twd's generated `tagOwners` make `tag:twd` the
  owner of every tag a workload asks for (`tagOwnersFor`, the deploy
  preflight snippet). If two tenants share a tailnet, either tenant's mint
  credential can mint the other tenant's tags, and so can join a node that
  the other tenant's grants trust.
- Names collide: a workload node is `<workload>-<host>`, its Service is
  `svc:<workload>`, and its default tag is `tag:twd-wl-<workload>`. Two
  teams that each deploy `api` in one tailnet get the same names and tag.
- The reconciler deletes tagged devices "that twd didn't mint" (RFC §8, threat model).
  With two tenants in one tailnet and the lifecycle credential's tailnet-wide
  `devices:core` (TS-5), tenant A's reconciler would find tenant B's devices
  and delete them.
- Scoped app capabilities are already an established pattern:
  `twd.dev/cap/mcp` values name tools (ADR-035), and `twd.dev/cap/approve`
  values narrow by workload, workflow and step, failing closed (ADR-040).

**Options considered.**

1. **One tenant shared by both teams, with namespaces or projects inside
   it.** This is the lightest change to the data model, but the teams would
   share a VM under `isolation: "tenant"` (the RFC's V8-escape residual
   risk), a stord, a mint credential, an OBO root and a quota. Every
   per-tenant boundary would have to be rebuilt one level down. Rejected.
2. **One tailnet per team (what works today).** This is the strongest
   boundary and needs no change. It doesn't fit teams that need to share a
   tailnet so they can reach each other's services and the same databases.
   It stays the recommended answer when teams don't trust each other's
   tailnet admins.
3. **Roles stored in mgmt's database (RBAC tables, `twd role grant`).**
   This duplicates the tailnet policy, and it contradicts the RFC's "grants
   are the source of truth". Who may do what would then live in two places.
   Rejected.
4. **Several tenants in one tailnet, one per team, scoped by grant values.**
   This reuses every existing per-tenant boundary, and authorization stays
   in the tailnet policy. Chosen.

**Decision.**

1. **A tenant is a trust domain, not a tailnet.** A tailnet may hold several
   tenants. The RFC identity table changes from "Tenant: a tailnet" to
   "Tenant: a trust domain in one tailnet, bound by its own trust
   credentials and owner tag". One tenant per tailnet remains the default.
   When there's only one tenant, nothing below changes for it (D7).

2. **`twd.dev/cap/admin` values can name tenants and actions.**

   ```jsonc
   {"src": ["group:payments"], "dst": ["tag:twd-mgmt"], "ip": ["tcp:443"],
    "app": {"twd.dev/cap/admin": [
      {"tenants": ["payments"], "actions": ["read", "deploy", "kill", "secrets"]}
    ]}}
   ```

   - `tenants` absent means all tenants. `actions` absent means all
     actions. So the existing `[{}]` keeps meaning a full operator admin,
     and existing policies keep working.
   - A request is allowed if **one single value** permits both the tenant
     and the action. Values aren't merged: `{tenants:[a], actions:[read]}`
     plus `{tenants:[b], actions:[deploy]}` doesn't give deploy on `a`.
     This is the same per-value matching that `internal/approvecap` uses.
   - The actions are a short fixed list:
     - `read`: list, status, logs, objects, deployments, audit, telemetry.
     - `deploy`: deploy, rollback, undeploy, public routes, telemetry and
       secret grants.
     - `kill`: kill tiers.
     - `secrets`: write secret values.
     - `data`: read and write the tenant's data through mgmt (KV, D1, R2
       and queue admin and journal routes).
   - **Operator routes** need a value with neither `tenants` nor `actions`
     set. These are creating tenants, changing a tenant's limits, pool, tier
     or credentials, and everything under hosts, releases, upgrades, public
     images and device-delete runs. A team can't raise its own quota or put
     itself on another pool.
   - A caller whose only values are tenant-scoped is a new principal kind,
     `tenant-admin`, which carries its scope. The `Admins` operator override
     (`-admins`) stays operator-wide.
   - `twd.dev/cap/mcp` and `twd.dev/cap/approve` values take the same
     optional `tenants` field. The MCP tools and the approver's inbox see
     only those tenants.
   - `local` (mgmt's own node and the local-dev socket) and `host`
     principals are unchanged.

3. **The spec says which scope every operation needs.** Each operation in
   `api/openapi.yaml` gets `x-twd-scope: operator | tenant` and, for
   `tenant`, `x-twd-action: <action>`. `make api` generates the
   operationId → (scope, action) table, and api-check fails if an operation
   is missing either field. A new route can't ship unscoped (fail closed,
   ADR-027).
   - Routes with `{id}` in the path are checked against it.
   - Routes keyed by a resource (`/v1/deployments/{id}`,
     `/v1/objects/{id}`, `/v1/container-reservations/{id}`,
     `/v1/volumes/{id}/keys`) resolve the resource's tenant first, then
     check. A resource in another tenant returns `404`, not `403`, so
     tenant names and IDs don't leak.
   - List routes (`/v1/tenants`, `/v1/deployments`, `/v1/objects`, the
     audit index) filter their results to the caller's tenants.
   - One helper, `authorizeTenant(p, tenantID, action)`, does the check, in
     a new file (`mgmt/tenantscope.go`). `/v1`, `/ui`, `/mcp` and the
     registry all call it. The registry's user branch also requires `read`
     on the repository's tenant to pull, and `deploy` to push.
   - `GET /v1/whoami` reports the caller's scope, so `twd link` and the UI
     can show which tenants are visible.

4. **Each tenant has its own owner tag, and its trust credentials are
   scoped to it.** A new tenant field, `ownerTag` (for example
   `tag:twd-t-payments`), is immutable after creation. Its default is
   `tag:twd`.
   - `twd connect` and `twd tenant create` generate trust credential specs
     with `"tags": ["<ownerTag>"]`. The generated `tagOwners` snippets name
     the tenant's owner tag (and `autogroup:admin` on SaaS), not `tag:twd`.
     `tag:twd` keeps owning only twd's own tags (`tag:twd-mgmt`,
     `tag:twd-host`, `tag:twd-issuer`, `tag:twd-public`).
   - Tailscale only lets a credential mint tags that its own tag owns. So
     tenant B's credential *can't* mint tenant A's tags, even if mgmt is
     wrong or compromised. That makes the owner tag the hard boundary.
   - mgmt also checks, as a second layer: at deploy, preflight and spawn,
     every tag a workload asks for must be owned by its tenant's
     `ownerTag` in the tenant's cached policy (`PolicyCache`). It must not
     be owned by another tenant's owner tag on the same tailnet. Otherwise
     `400 tag_not_owned`, with the `tagOwners` line to add.
   - `reservedTags` gains every owner tag registered on mgmt. A workload
     can't ask for an owner tag.
   - **A second tenant on a tailnet requires owner tags.** Registering a
     tenant on a tailnet that already has a tenant fails with
     `409 shared_owner_tag` if either one has `ownerTag: tag:twd`. The fix
     names the existing tenant and the steps to move it to its own owner
     tag (D7). Two tenants with owner tag `tag:twd` would let either one
     mint the other's tags.

5. **Each tenant gets a name prefix on shared tailnets.** A new tenant
   field, `namePrefix` (a DNS label, unique per tailnet), is immutable after
   the tenant's first deploy. Its default is empty.
   - When it's set, it's added in front of every name twd generates for
     the tenant:
     - workload nodes: `<prefix>-<workload>-<host>`;
     - Services: `svc:<prefix>-<workload>`;
     - objects: `<prefix>-<class>-<hash>`, and `<prefix>-<class>-<slug>`
       with `publicNames`;
     - default tags from `twd init`: `tag:<prefix>-wl-<workload>`, owned
       by the tenant's owner tag.
   - Registering a second tenant on a tailnet requires every tenant there
     to have a distinct, non-empty prefix (`409 name_prefix_required`).
   - Deploy refuses a workload whose longest generated name would be more
     than 63 characters (`400 bad_request`, naming the label).
   - Tags a team writes by hand don't need the prefix. Ownership (D4)
     decides who may use a tag, not the tag's name.

6. **The reconciler and lifecycle credential act only on their own
   tenant's devices.** "Devices twd didn't mint" becomes "devices tagged
   with a tag owned by this tenant's `ownerTag` that this tenant didn't
   mint". When a device carries a tag owned by another tenant's owner tag
   on the same tailnet, the reconciler leaves it alone and audits it as
   `foreign_device_skipped`. Tier-3 kill (posture) changes only devices the
   tenant minted. This holds whatever the answer to TS-5 is.

7. **Moving to owner tags and prefixes.** Existing tenants keep
   `ownerTag: tag:twd` and an empty prefix, and their nodes, names and
   policy don't change.
   - `twd tenant set <t> --owner-tag tag:twd-t-<t>` prints the new
     `tagOwners` lines and the new trust credential spec. It switches mgmt
     to the new credential once the admin confirms it (`wif-check`).
     Existing nodes keep their tags, because the tags don't change, only
     their owner.
   - A prefix can only be set before the first deploy. Adding a prefix to
     a tenant that already has deployments is a rename. It goes through
     the pre-joined-node rename path (ADR-031) as an explicit `twd tenant
     rename-nodes` run, not as a side effect.

8. **Unchanged.** The data plane doesn't change. Cross-team calls go
   over the tailnet, between tenants, governed by grants
   (`tag:payments-wl-api → tag:search-wl-index`). Caps, `getCaller`, OBO
   assertions and audit work as they do between any two nodes. DO bindings
   and in-process RPC still work only within a tenant. Hosts, pools and
   placement don't change: one host may serve several tenants from the same
   tailnet, with one netd and one stord each.

**Consequences.**

- Two teams on one twd install and one tailnet each get their own tenant.
  Each manages its own workloads, secrets, data and approvals, and neither
  can deploy, kill, read or mint as the other. The operator keeps hosts,
  pools, quotas and releases.
- Authorization stays in the tailnet policy file. A team's access is one
  grant. Removing the grant revokes access at the next netmap. Audit
  records carry the WhoIs login, and they gain the matched scope
  (`scope: {tenants, actions}`).
- **Teams can't edit their own grants.** The tailnet policy file is edited
  by tailnet admins. Teams declare `tailnet.connects`, and twd prints the
  snippets. Delegating ownership of the policy file (for example GitOps
  with a code owner per team's section) is outside twd.
- **This boundary is only as strong as the tailnet's admins are
  trustworthy.** A tailnet admin can grant themselves any tenant's scope
  or retag a node. Teams that need a boundary against each other's tailnet
  admins should use separate tailnets (option 2).
- New error codes go in `docs/errors.md`, each with "When" and "Fix":
  `tenant_forbidden` (a `403` naming the missing grant value, with a
  snippet), `tag_not_owned`, `shared_owner_tag`, `name_prefix_required`.
- Two schema columns (`owner_tag`, `name_prefix`) are added as an expand
  migration (ADR-041), so the change is upgrade-relevant.
- The RFC's threat model row "issuer key stolen" narrows: within one
  tenant, the attacker can mint only that tenant's owner-tagged tags.

**Open.**

- **Headscale profile.** Headscale's tag ownership and credential scoping
  differ (ADR-015). Until they're verified, the Headscale profile keeps one
  tenant per tailnet, and registering a second one fails with
  `409 shared_owner_tag`, which names the profile.
- **Callers outside mgmt's tailnet** (a team's tailnet that differs from
  the operator's) can't be identified by WhoIs. This ADR covers callers in
  mgmt's tailnet, which is the shared-tailnet case. Cross-tailnet admin
  (node sharing or tsidp) is separate work.
- **Team self-service tenant creation** (a team creates its own tenant,
  bounded by an operator-set pool) is out of scope. Creating tenants stays
  an operator route.
- **Whether `data` should be split** into read and write.

**Implementation plan.** These are small PRs in this order. Each one is
useful on its own, and each follows AGENTS.md (new files for the logic,
small edits to the hotspots).

1. **Scoped admin cap (D2, D3).**
   - Add the `x-twd-scope` and `x-twd-action` fields to every operation,
     and add a check that fails when one is missing.
   - Add `mgmt/tenantscope.go`: value parsing, per-value matching, and
     `authorizeTenant`.
   - Add the `tenant-admin` principal kind to `TailnetHandler`, and the
     checks in `/v1`, `/ui`, `/mcp` and the registry. List routes filter.
     Cross-tenant resources return `404`.
   - Add the `tenants` field to the MCP and approve cap values.
   - Report the scope in `whoami`. Add the `tenant_forbidden` error code.
   - Tests:
     - a table test over every operationId, checking scope and action;
     - a cross-tenant `404` test for each resource-keyed route;
     - a per-value (not merged) union test;
     - the registry pull and push rules.
   - Run `make lint test` and `make test-pg`.
2. **Owner tag (D4, D6, the owner-tag half of D7).**
   - Add the `owner_tag` column (expand migration).
   - Generate the credential and `tagOwners` snippets from the owner tag.
   - Check tag ownership at deploy, preflight and spawn. Add owner tags to
     `reservedTags`. Refuse a second tenant with `409 shared_owner_tag`.
   - Scope the reconciler to its own tenant's devices. Add
     `twd tenant set --owner-tag`.
   - Run `make test-pg`, `make e2e-upgrade` and `make e2e-headscale`. The
     Headscale run checks the refusal.
   - With credentials available, run `make e2e-real` with two tenants on
     one tailnet. It should show that B's credential is refused when it
     mints A's tag.
3. **Name prefix (D5, the prefix half of D7).**
   - Add the `name_prefix` column. Apply the prefix to node, Service and
     object names and to `twd init` tags. Check the 63-character limit.
     Add `409 name_prefix_required`.
   - Run `make e2e-placement`, since names follow objects across moves, and
     `make templates-typecheck test-sdk` if templates change.
4. **Docs.**
   - RFC identity table and threat model.
   - Operator guide: "two teams, one tailnet".
   - `docs/errors.md`.
   - A `changes/` fragment.
   - Assign this ADR its number.
