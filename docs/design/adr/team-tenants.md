# ADR-team-tenants: Several tenants per tailnet, one per team: tenant-scoped admin grants, a per-tenant owner tag, and per-tenant name prefixes

**Date:** 2026-11-15
**Status:** Proposed (results in `status/explain-tailnet-multi-tenancy-team-workloads.md`).
Tracked in issue #80. D2 and D3 are implemented (step 1a of the plan at
the end); the rest isn't yet.

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
4. **Several tenants in one tailnet, one per team, with access granted by
   a new tenant-scoped capability.** This reuses every existing per-tenant
   boundary, and authorization stays in the tailnet policy. Chosen.
5. **The same, but scoped by adding values to `twd.dev/cap/admin`.**
   Rejected: mgmt today ignores that cap's values, so on any older version
   a scoped grant would mean full admin (D2).

**Decision.**

1. **A tenant is a trust domain, not a tailnet.** A tailnet may hold several
   tenants. The RFC identity table changes from "Tenant: a tailnet" to
   "Tenant: a trust domain in one tailnet, bound by its own trust
   credentials and owner tag". One tenant per tailnet remains the default.
   When there's only one tenant, nothing below changes for it (D7).

2. **Team access is a new capability, `twd.dev/cap/tenant-admin`. The
   existing `twd.dev/cap/admin` stays operator-only.**

   ```jsonc
   {"src": ["group:payments"], "dst": ["tag:twd-mgmt"], "ip": ["tcp:443"],
    "app": {"twd.dev/cap/tenant-admin": [
      {"tenants": ["payments"], "actions": ["read", "deploy", "kill", "secrets"]}
    ]}}
   ```

   - **Why a new cap instead of values on `twd.dev/cap/admin`.** Today's
     gate (`allowed` in `mgmt/server.go`) treats *holding*
     `twd.dev/cap/admin` as full admin and ignores its values. If team
     scope were a value on that cap, any mgmt that predates this ADR would
     read `{"tenants": ["payments"]}` as full operator admin, over every
     tenant and every host. That happens when an admin writes the grant
     before upgrading, and on old replicas during a rolling upgrade
     (ADR-041). A cap that old versions don't know is refused by them, so
     it fails closed. The rule generalizes: **never narrow an existing
     capability by adding a field to its values. Older readers ignore the
     field and widen the access.**
   - `twd.dev/cap/admin` keeps its current meaning: an operator, with
     every value ignored as today. The `-admins` override and the `local`
     principal (mgmt's own node and the local-dev socket) stay
     operator-wide. `host` principals are unchanged.
   - **Who manages hosts:** only operators. Hosts, pools, releases,
     upgrades, public images, device-delete runs, creating tenants, and
     changing a tenant's limits, pool, tier or credentials keep
     `x-twd-role: admin` (D3). A tenant-admin can't reach them, so a team
     can't raise its own quota, put itself on another pool, or cordon or
     drain a host.
   - Both `tenants` and `actions` are **required and non-empty** in every
     value. A value that is missing either one, or that names an unknown
     action, matches nothing, and mgmt reports it in `GET /v1/whoami` and
     `twd policy lint`. There is no "all tenants" wildcard. Operator-wide
     access is `twd.dev/cap/admin`.
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
   - A caller that holds `twd.dev/cap/tenant-admin` but not
     `twd.dev/cap/admin` stays an ordinary `user` principal that isn't an
     admin; its values travel in the principal's `CapValues`. So every
     existing check that asks "is this user an admin?" still says no, and
     the only extra access is what `mgmt/tenantscope.go` grants. A caller
     that holds both is an operator.
   - A holder whose values are all invalid is admitted only far enough for
     `GET /v1/whoami` to report why (`invalidTenantGrants`). Every other
     route refuses it with `403 tenant_forbidden`, naming the reasons.
   - **MCP and approvals inside a tenant don't get a `tenants` field on
     their own caps,** for the same reason as above: an older mgmt or hostd
     would ignore it and widen access. (Not built yet: until step 1b,
     tenant admins get `403` at `/mcp` and `/ui`.)
     - A tenant-admin value may carry `"mcp": {"tools": [...]}`. That
       admits the caller to `/mcp` for those tenants, with the read tools
       plus the destructive tools it names, and only where the value's
       `actions` also allow them (`deploy` for `deploy` and `undeploy`,
       `kill` for `kill`). ADR-035's rule that destructive tools must be
       named still holds.
     - A tenant-admin value may carry `"approve": [<approve value>]`, with
       the same fields as `twd.dev/cap/approve` (ADR-040). Those are checked
       by `internal/approvecap`, and only for approval instances in the
       value's tenants. Holding `actions` doesn't make someone an approver.
       The `approve` entry is the grant, just as being an admin isn't one
       in ADR-040.
     - The existing `twd.dev/cap/mcp` and `twd.dev/cap/approve` keep their
       current meaning, which is across all tenants. On a shared tailnet,
       `twd policy lint` warns about either one when it isn't granted to an
       operator.

3. **The spec says which role and action every operation needs, in the
   existing `x-twd-role` field.** Each operation already declares
   `x-twd-role` (`admin`, `host`, `admin|host`, `admin|approver`,
   `admin|mcp`, `any`). The value is read at runtime from the embedded spec
   (`OperationRoles` in `mgmt/api/v1/validate.go`), and `spec_test.go`
   fails on an operation that lacks one.
   - This ADR adds one more part to that list: `tenant:<action>`. For
     example, `admin|tenant:deploy` on `POST
     /v1/tenants/{id}/workloads/{workload}/deployments`, and
     `admin|host|tenant:read` on a route that hosts also read.
   - When `roleAllowed` (`mgmt/v1.go`) refuses a tenant admin, `roleGate`
     hands the request to `tenantGate` (`mgmt/tenantscope.go`), which
     admits it only through a `tenant:<action>` part, and only after the
     tenant check below.
   - An operation without a `tenant:` part is closed to tenant-admins. A
     new route is therefore operator-only until someone decides otherwise
     (fail closed, ADR-027).
   - `TestTenantRolesConsistent` (`mgmt/tenantscope_test.go`) checks that
     every `tenant:` part names a known action and sits where something
     checks the tenant. It also checks that every operation under
     `/v1/tenants/{id}` either has a `tenant:` part or is on a short list of
     admin-only routes in the test (`UpdateTenant`, `DeleteTenant`,
     `PurgeTenantObjects`, the approver and host routes). That way, leaving
     a route out is a deliberate choice, not an oversight.
   - Routes with `{id}` under `/v1/tenants/` are checked by the gate.
     Naming a tenant you weren't granted is `403 tenant_forbidden`, decided
     before any lookup, so it says nothing about whether the tenant
     exists.
   - Routes whose tenant isn't in the path must be listed in
     `tenantCheckedInHandler`; otherwise the gate refuses them to tenant
     admins. Their handlers check the tenant once they know it:
     - A tenant the caller supplied (the body of `POST /v1/deployments`, a
       `?tenant=` filter) is `403` if it isn't granted.
     - A resource looked up by ID (`/v1/deployments/{id}`,
       `/v1/objects/{id}`) in another tenant is `404`, so its existence
       doesn't leak.
     - Lists (`/v1/tenants`, `/v1/deployments`, `/v1/objects`,
       `/v1/audit`) keep to the granted tenants. Without `?tenant=`, they
       default to the caller's only tenant, and a caller with several must
       name one. Host audit-chain statuses aren't shown to tenant admins.
   - Two read-only routes that hold nothing tenant-specific (operator base
     images and their aliases) are in `tenantAnyTenant`: `read` in any
     tenant is enough.
   - The registry (`/v2/`, not behind the gate) applies the same rule in
     `mgmt/tenantscope_registry.go`. A tenant admin pulls its tenants'
     namespaces with `read`, pushes them with `deploy`, and pulls but never
     pushes `public/…`. Writing this showed that the registry admitted
     *every* user principal as an admin, including one holding only
     `twd.dev/cap/approve`. It now admits only admins, hosts and tenant
     admins.
   - `GET /v1/whoami` reports `tenantGrants` and `invalidTenantGrants`, and
     lists only the granted tenants. `twd link` accepts a tenant admin, and
     `twd whoami` shows its tenants and actions.

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

1. **Tenant-admin cap (D2, D3).**
   - **1a, done** (`mgmt/tenantscope.go`, `mgmt/tenantscope_registry.go`,
     `tenant:<action>` parts in `api/openapi.yaml`):
     - the cap, value parsing and per-value matching;
     - the gate and the handler checks, list filtering, cross-tenant
       `404`s, and the registry rule;
     - `whoami`, `twd link`, `twd whoami`, and the `tenant_forbidden`
       error;
     - the registry fix for approvers;
     - docs (`docs/teams.md`).
     Tests are in `mgmt/tenantscope_test.go`: value parsing, values never
     merged, the spec/route consistency check, a tenant admin refused on
     every admin route and on the other tenant, `404` by resource ID, list
     scoping, an invalid value granting nothing, an admin never narrowed,
     and the registry rules.
   - **1b, to do:**
     - the `mcp` sub-value and `/mcp` for tenant admins;
     - the `approve` sub-value (checked through `internal/approvecap`,
       limited to the value's tenants, in mgmt's inbox filter and in
       hostd);
     - `/ui` for tenant admins;
     - `twd policy lint` reporting invalid values, plus the shared-tailnet
       warnings for `twd.dev/cap/mcp` and `twd.dev/cap/approve`.
   - Still to run, needing a real setup: `make e2e-upgrade`, with a mgmt
     from before this change refusing a caller who holds only
     `twd.dev/cap/tenant-admin`.
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
