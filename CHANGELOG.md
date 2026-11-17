# Changelog

What changed in each tailworks release, newest first. This file is generated
from the release catalog (`internal/catalog/releases/`, written from the
fragments in `changes/released/`); don't edit it by hand. How versions
work and what may break: [docs/RELEASING.md](docs/RELEASING.md#versions).
What may run together, and how to upgrade:
[docs/upgrading.md](docs/upgrading.md).

## v0.2.0 (2026-11-17)

workerd 1.20261114.1.

### Breaking

#### delete-confirm-issue-72

Every command that deletes data or a tenant's resources now asks first:
`tw d1 delete`, `tw kv namespace delete`, `tw queues rm`,
`tw queues purge`, `tw r2 bucket rm`, `tw tenant rm` and `tw host rm`.
On a terminal it says what will be lost and asks you to type the name.
Without a terminal (a script, a coding agent, `--json`) it refuses with
`confirmation_required` and does nothing; add `--yes` to go ahead.
`tw d1 delete` without `--yes` now prompts on a terminal, and its refusal
code is `confirmation_required` (it was `bad_request`).

Operator action: scripts that run these commands need `--yes`.

#### observability-logs-metrics-traces-20

`tw logs <workload>` now prints one structured line per request, with filters (`--errors`, `--status`, `--trace`, …) and `--json` request records. Pass `--console` for the app's raw output, which used to be the default. tailworks also gains Prometheus `/metrics` and OTLP export of logs, metrics and traces (`tw telemetry`).

Operator action: scripts that read `tw logs` output need `--console`. tw-hostd and tw-mgmt now serve `/metrics` on port 9464 of their tailnet node by default (`-metrics-port 0` turns it off); scraping it needs a policy grant.

#### remove-wif-apikey

tailworks now mints node keys on Tailscale with an OAuth client only. The
`apikey` and `wif` (workload identity federation) mint modes are removed
(ADR-042). The Headscale profile is unchanged: it still
mints with a Headscale API key.

Removed: `tw up --wif`, `--wif-client-id`, `--wif-lifecycle-client-id`,
`--issuer-authkey-file`, `--login-timeout`, and `--api-key-file` outside
`--control=headscale`; `tw tenant set --wif-client-id` and
`--wif-lifecycle-client-id`; `tw doctor --tailnet/--api-key-file`; mgmt's
OIDC issuer (`tw-mgmt -issuer-url`, `-issuer-listen`, `-funnel`,
`-issuer-tsnet-*`, and the discovery and JWKS endpoints);
`POST /v1/tenants/{id}/wif-check`; the tenant fields `wifClientID`,
`wifLifecycleClientID` and `wif`; `tw-netd -mint-api-key-file` (now
`-mint-oauth-client-file`). New: `tw tenant add --oauth-client-file`,
`tw tenant set --oauth-client-file`, and the error code
`mint_mode_removed`.

Operator action: if `tw doctor` fails its `mint_mode` row, or `tw up`
says `mint_mode_removed`, the install mints with an API key or WIF. Create
an OAuth client (docs/INSTALL.md, "The OAuth client"), then run
`tw up --oauth-client-file <file>`. This keeps the tenant, its deployments
and mgmt's node. Do the same for each added tenant with
`tw tenant set <tenant> --oauth-client-file <file>`. Then revoke the old
API key, or delete the WIF trust credentials, the `tw-issuer` machine and
the `tag:tw-issuer` policy entries. Running nodes keep serving until then,
but nothing new can be minted.

#### rename-twd-to-tailworks-86

twd is now **tailworks** (issue #86). The project, the repo
(`github.com/rosskukulinski/tailworks`), the Go module, the npm packages
(`@tailworks/tailnet`, `@tailworks/node`, `@tailworks/mcp`,
`npm create tailworks`), the install root (`~/.tailworks`), the per-app
config (`tailworks.jsonc`, top-level key `"tw"`) and the release tarballs
(`tailworks-<version>-<os>-<arch>.tar.gz`) use the long name. The command is
`tw` (`install.sh` also links `tailworks` → `tw`), and everything else uses
the short name: the daemons (`tw-mgmt`, `tw-hostd`, `tw-netd`,
`tw-vmhelper`, `tw-vz`, `tw-guestd`), env vars (`TW_*`), tags (`tag:tw`,
`tag:tw-mgmt`, `tag:tw-host`, `tag:tw-public`, `tag:tw-wl-*`), the Service
`svc:tw-mgmt`, node hostnames (`tw-`), headers (`x-tw-*`, `Tw-Error`,
`Tw-Actor`, `Tw-MCP-Tools`), metrics (`tw_*`), the launchd label
`dev.tailworks.up` and the systemd units `tw.service` and
`tw-vmhelper.service`. Peer capabilities move from `twd.dev/cap/*` to
`github.com/rosskukulinski/tailworks/cap/*`.

There is no upgrade path from twd 0.x: nothing reads `~/.twd`,
`twd.jsonc`, the old env vars, tags or headers.

Operator action: tear down and reinstall. On each machine, run
`twd down --purge --service`, remove any hand-made agents
(`dev.twd.hostd`, `twd-vmhelper.service`) and delete `~/.twd`. In the
admin console, remove the `tag:twd*` devices, then the `tag:twd*` tag
owners, `svc:twd-mgmt` and the `twd.dev/cap/*` grants; revoke the OAuth
client and create one scoped to `tag:tw`. Rename each app's `twd.jsonc` to
`tailworks.jsonc` (key `"tw"`) and its `@twd/*` imports to
`@tailworks/*`. Then install with `get.sh` and run `tw doctor`.

### Security

#### audit-chain-flood-issue-68

The host audit chain survives a flood (red-team H03, #68). Before, a flood
of tenant `audit()` notes or of public requests to unlisted paths, while
mgmt was slow or unreachable, made hostd drop the oldest queued records.
Those records were already chained, so mgmt refused every later batch and
the host's chain stayed broken for good. Now hostd never drops a chained
record: host records spill to `<data-dir>/audit-spill.jsonl` once 10 000
wait in memory, and are flushed in order, in batches. Records that may be
shed are refused before they are chained and counted in a new in-chain
`audit_dropped` record, so the chain still verifies and the gap shows in
`tw audit`. Those records are tenant notes over a per-object rate (10/s,
burst 200) or sent while the queue is backed up, and `public_request`
records once the spill reaches 256 MiB. Public `404 not_public` and
`400 bad_request` refusals are no longer one `public_request` record each.
They are counted in the per-minute `public_rejected` summary
(`notPublic`, `badRequest`).

`audit_dropped` is a new `AuditKind`. An older mgmt stores it like any host
record.

#### explain-tailnet-multi-tenancy-team-workloads

Several teams on one tailnet (ADR-043, docs/teams.md).

- Tenant admins: grant a team `github.com/rosskukulinski/tailworks/cap/tenant-admin` on `tag:tw-mgmt`
  with values like `{"tenants": ["tn_example-pay"], "actions": ["read",
  "deploy", "kill"]}`. The actions are `read`, `deploy`, `kill`, `secrets`
  and `data`.
  - The team can `tw link`, deploy, list, read logs and audit, and kill in
    those tenants, through the CLI, the API, the registry and the admin
    console.
  - An `"mcp"` entry admits tailworks's MCP server for those tenants, and an
    `"approve"` entry makes the team an approver there only.
  - Hosts, tenant settings, releases and upgrades stay with admins.
  - Refusals are `403 tenant_forbidden` with the grant to add; another
    tenant's deployment or object is `404`.
- Owner tags and name prefixes: `tw tenant add <tailnet> --owner-tag
  tag:tw-t-<team> --name-prefix <p>` adds a second team's tenant to a
  tailnet. Each tenant on a shared tailnet must have its own owner tag
  (`409 shared_owner_tag`), and the OAuth client scoped to it can't mint
  another team's tags. Its tailnet names get the prefix (`<p>-…`,
  `svc:<p>-…`).
- `tw policy` and `tw deploy` warn about grants to `tag:tw-mgmt` that
  don't do what they look like (`accessWarnings`), and about tags another
  team owns (`tagOwnerProblems`).

Fix: a tailnet user holding only `github.com/rosskukulinski/tailworks/cap/approve` could push and
pull every tenant's images in mgmt's registry. The registry now admits only
admins, hosts and tenant admins.

Operator action: none for a tenant alone on its tailnet. To add a second
team's tenant to a tailnet, first move the existing tenant to its own owner
tag (`tw tenant set <tenant> --owner-tag … --oauth-client-file …`). Don't
scope a team by adding values to `github.com/rosskukulinski/tailworks/cap/admin`: older mgmts ignore
them and treat the holder as a full admin.

#### prelaunch-100-security-model

`tw status` and `GET /v1/status` no longer report Node-tier egress as `enforced` on a Linux host whose Landlock ABI is below 4 (kernels before 6.7, such as Ubuntu 22.04's 5.15). Landlock can't restrict TCP there, so egress is now reported as `unenforced`, as on macOS. netd now strips client copies of every `Tailscale-*` header and the underscore spellings `X_TW_*` and `tw_*` by prefix, not only a list of exact names. docs/security-model.md now lists what the process-tier sandbox confines on each platform and Landlock ABI, which hosts may be shared between tenants, and whom a tenant trusts (host operator, mgmt operator, tailnet admin) (#100).

#### prelaunch-105-host-allowlist

tw-mgmt now refuses tailnet requests whose `Host` header is not one of its own names (its MagicDNS FQDN or short name, `-tsnet-hostname`, `-tsnet-service`) or an IP literal, with the new error `421 misdirected_request`. This closes a DNS-rebinding hole: before, a web page on an attacker's domain rebound to mgmt's tailnet IP was same-origin with mgmt's plain-HTTP `:80` listener and could read `/v1` and post admin UI forms with the admin's identity. The `tw` CLI, hosts and the admin UI address mgmt by those names or its IP and are unaffected. The admin UI's `Origin` check is now tied to the same allowlist.

Operator action: if you reach tw-mgmt through a CNAME or another DNS name of your own, use its tailnet name or IP instead. If you renamed the mgmt node, restart tw-mgmt so it learns the new name.

#### prelaunch-98-upgrade-sig

`tw upgrade` on a `tw up` machine now checks the release signature. It extracts the tarball with the hardened extractor tw-hostd uses, then checks its `SHA256SUMS.sig` against the release keys built into `tw` (or `TW_RELEASE_SIGNERS`) with the same verifier as tw-hostd's self-upgrade, before anything in the release runs. Before, it only compared the tarball with a `SHA256SUMS` downloaded from the same place, and `--from <dir>` checked nothing, so a tampered tarball's own `install.sh` would have run. An unsigned release is refused with `release_unsigned` and a badly signed one with `release_bad_signature`, on `--plan` too and for `--from`. `tw upgrade --allow-unsigned` (or `TW_ALLOW_UNSIGNED=1`) installs a build you made yourself, with a warning in the plan. `tw upgrade <version>` also refuses a download whose release is a different version (#98).

#### public-conn-limit-issue-69

One client can no longer fill a workload's public (Funnel) connections and lock everyone else out. The public listener now caps connections per client address (`public.limits.maxConnsPerIp`, default 32; an IPv6 client counts per /64), gives a new connection 5 seconds to send its first request, and closes upgraded (WebSocket) connections after `public.limits.upgradeLifetimeSeconds` (default 3600). Refused connections are counted in the per-minute `public_rejected` audit summary (`ipConnsRefused`) and in the new `tw.public.rejected` metric (#69).

Operator action: WebSocket clients of a public workload must reconnect when the lifetime ends; raise `upgradeLifetimeSeconds` or `maxConnsPerIp` in `tailworks.jsonc` if your users need longer sessions or share one address. A host on this version keeps the new defaults even while mgmt is older.

#### public-name-claim-issue-70

Public ingress: a public name now has one owner per tailnet (red-team
H10). The first tenant to deploy `"public"` routes under a workload name
owns `https://<name>.<tailnet>.ts.net/` on that tailnet, across undeploys,
redeploys and kills, until it releases the name. Another tenant on the same
tailnet that declares `"public"` under that name is refused with
`409 public_name_owned`, at deploy, at `--dry-run`, and when its host asks
for the public node. Before this, a second tenant could take the name after
a kill and redeploy and receive the first tenant's public traffic, such as
signed webhooks.

New API: `GET /v1/tenants/{id}/public-names` lists a tenant's names, and
`DELETE /v1/tenants/{id}/public-names/{name}` releases one once nothing
serves it (`409 public_name_in_use` otherwise). Deleting a tenant releases
its names. New audit kinds: `public_name_claim`, `public_name_release`.

Operator action: none. The store migration adds the `public_names` table.
Names already served before the upgrade are claimed for the tenant serving
them the first time anyone deploys or recreates them, so the upgrade can't
hand a name to another tenant. If several tenants share a tailnet, give
them all the same `tailnet` name (its MagicDNS suffix), because ownership is
scoped by that name.

#### release-signing-issue-71

Releases are signed. Each release's `SHA256SUMS`, and each tarball's, gets a `SHA256SUMS.sig`: an SSH signature (`ssh-keygen -Y`) by a tailworks release key that is pinned in `scripts/get.sh`, `install.sh` and the tailworks binaries. `get.sh` checks the signature before it trusts the checksums, and `install.sh` checks it before it installs. tw-hostd's self-upgrade checks it in Go against the keys built into the running binary, so a compromised tw-mgmt can no longer push a release of its own. An unsigned or badly signed release is refused. `install.sh --allow-unsigned` (or `TW_ALLOW_UNSIGNED=1`) installs a build you made yourself, and local builds (`x.y.z-<commit>`) install with a warning. `get.sh` refuses an unsigned release whatever its tag (no unsigned release was ever published). tw-hostd also checks that a release it is ordered to install is that version (`VERSION`), and extracts tarballs so that no entry can write or link outside the extract directory. To check a release by hand, see INSTALL.md, "Verify a release" (#71).

Operator action: a host that self-upgrades now refuses unsigned releases. Until a release key is pinned (docs/RELEASING.md, "Signing"), upgrade hosts by hand.

#### review-fixes

Security fixes from review. `tw up` runs as a single process by default (mgmt with the host agent embedded; `--separate-host` keeps the old layout), and tw-mgmt serves an admin UI at `/ui/`.

### Upgrade actions

#### benchmark-dx-infra-vs-cloudflare-workerd-vercel

`tw dev` prints each request with its `console.*` lines and the stack of any uncaught exception (`--json`: a new `request` event), and picks up edits within about 0.2 s. An unknown command suggests the nearest one (`tw tail` → `tw logs`), `--help` shows each flag's type, and `tw ls` shows why a deployment failed. `tw init` names `package.json` after the app and adds `npm run dev`. The admin console and `tw audit` render audit records the same way (no raw JSON); the approvals page has one comment box with Approve and Reject buttons. `tw docs search` ranks the guides above the API contract and the ADRs.

#### cli-ergonomics-issue-46

`tw completion bash|zsh|fish` prints a tab-completion script for every command, subcommand (`kv key put`, `d1 migrations apply`) and flag, generated from the CLI's own command table. `tw whoami [--json]` shows who the CLI acts as (your tailnet login through `tw link`, or the local operator), the mgmt it talks to, the tenant, and what that identity may do: admin, approve and the MCP tools. `GET /v1/whoami` adds `caps`, `approver` and `mcpTools` for it. `tw help` groups the commands (Set up · Develop · Deploy · Data · Observe · Operate) with one short line each; `tw help --all` prints every usage line. `tw version --check` compares with the latest GitHub release, prints `tw upgrade`, and says whether the running `tw up` needs a restart; an interactive `tw` prints a newer-release notice at most once a day (off with `--json`, `CI`, or `TW_NO_UPDATE_CHECK=1`). From a source checkout, `tw doctor` and a `build_failed` for an unresolved `@tailworks/*` import point at `make deps`.

#### delete-confirm-issue-72

Every command that deletes data or a tenant's resources now asks first:
`tw d1 delete`, `tw kv namespace delete`, `tw queues rm`,
`tw queues purge`, `tw r2 bucket rm`, `tw tenant rm` and `tw host rm`.
On a terminal it says what will be lost and asks you to type the name.
Without a terminal (a script, a coding agent, `--json`) it refuses with
`confirmation_required` and does nothing; add `--yes` to go ahead.
`tw d1 delete` without `--yes` now prompts on a terminal, and its refusal
code is `confirmation_required` (it was `bad_request`).

Operator action: scripts that run these commands need `--yes`.

#### deploy-history-rollback-issue-45

`tw deployments <workload>` lists a workload's deployment history, newest
first: id, created, who deployed, bundle SHA-256, status, error and host.
`tw rollback <workload> [<deployment>]` redeploys an earlier deployment's
stored bundle as a new deployment, through the same path as `tw deploy`
(placement, lease epoch, host fencing, preflight); with no ID it goes back
to the previous deployment that ran. It is audited as `kind: rollback`. A
rollback changes code and configuration only: Durable Object storage, D1
(migrations are forward-only), KV, R2 and queues keep their data, and
secrets are the workload's current ones (ADR-046).

mgmt now keeps the bundles of each workload's newest 10 deployments and
prunes older ones, keeping their record and hash (tw-mgmt
`-bundle-retention`, `-1` keeps every bundle). New API:
`GET /v1/tenants/{id}/workloads/{workload}/deployments` and
`POST /v1/tenants/{id}/workloads/{workload}/rollbacks`.

Operator action: none. The store migration adds columns to `deployments`.
The first deploy of each workload after the upgrade prunes that workload's
bundles beyond the newest 10; pass `-bundle-retention -1` to tw-mgmt
beforehand to keep them all.

#### observability-logs-metrics-traces-20

`tw logs <workload>` now prints one structured line per request, with filters (`--errors`, `--status`, `--trace`, …) and `--json` request records. Pass `--console` for the app's raw output, which used to be the default. tailworks also gains Prometheus `/metrics` and OTLP export of logs, metrics and traces (`tw telemetry`).

Operator action: scripts that read `tw logs` output need `--console`. tw-hostd and tw-mgmt now serve `/metrics` on port 9464 of their tailnet node by default (`-metrics-port 0` turns it off); scraping it needs a policy grant.

#### prelaunch-104-update-check

`tw init` and `tw dev` no longer run the once-a-day update check, so they make no outbound request (before, they could ask `api.github.com` for the latest release after they finished). `tw version --json` adds `updateCheck`: whether this tw runs the check (a release build without `CI` or `TW_NO_UPDATE_CHECK` set). The README ("What tw sends") and `docs/security-model.md` ("Telemetry and the update check") now say what the check sends, when it runs and how to turn it off; tailworks has no other telemetry.

#### prelaunch-105-host-allowlist

tw-mgmt now refuses tailnet requests whose `Host` header is not one of its own names (its MagicDNS FQDN or short name, `-tsnet-hostname`, `-tsnet-service`) or an IP literal, with the new error `421 misdirected_request`. This closes a DNS-rebinding hole: before, a web page on an attacker's domain rebound to mgmt's tailnet IP was same-origin with mgmt's plain-HTTP `:80` listener and could read `/v1` and post admin UI forms with the admin's identity. The `tw` CLI, hosts and the admin UI address mgmt by those names or its IP and are unaffected. The admin UI's `Origin` check is now tied to the same allowlist.

Operator action: if you reach tw-mgmt through a CNAME or another DNS name of your own, use its tailnet name or IP instead. If you renamed the mgmt node, restart tw-mgmt so it learns the new name.

#### public-conn-limit-issue-69

One client can no longer fill a workload's public (Funnel) connections and lock everyone else out. The public listener now caps connections per client address (`public.limits.maxConnsPerIp`, default 32; an IPv6 client counts per /64), gives a new connection 5 seconds to send its first request, and closes upgraded (WebSocket) connections after `public.limits.upgradeLifetimeSeconds` (default 3600). Refused connections are counted in the per-minute `public_rejected` audit summary (`ipConnsRefused`) and in the new `tw.public.rejected` metric (#69).

Operator action: WebSocket clients of a public workload must reconnect when the lifetime ends; raise `upgradeLifetimeSeconds` or `maxConnsPerIp` in `tailworks.jsonc` if your users need longer sessions or share one address. A host on this version keeps the new defaults even while mgmt is older.

#### release-signing-issue-71

Releases are signed. Each release's `SHA256SUMS`, and each tarball's, gets a `SHA256SUMS.sig`: an SSH signature (`ssh-keygen -Y`) by a tailworks release key that is pinned in `scripts/get.sh`, `install.sh` and the tailworks binaries. `get.sh` checks the signature before it trusts the checksums, and `install.sh` checks it before it installs. tw-hostd's self-upgrade checks it in Go against the keys built into the running binary, so a compromised tw-mgmt can no longer push a release of its own. An unsigned or badly signed release is refused. `install.sh --allow-unsigned` (or `TW_ALLOW_UNSIGNED=1`) installs a build you made yourself, and local builds (`x.y.z-<commit>`) install with a warning. `get.sh` refuses an unsigned release whatever its tag (no unsigned release was ever published). tw-hostd also checks that a release it is ordered to install is that version (`VERSION`), and extracts tarballs so that no entry can write or link outside the extract directory. To check a release by hand, see INSTALL.md, "Verify a release" (#71).

Operator action: a host that self-upgrades now refuses unsigned releases. Until a release key is pinned (docs/RELEASING.md, "Signing"), upgrade hosts by hand.

#### remove-wif-apikey

tailworks now mints node keys on Tailscale with an OAuth client only. The
`apikey` and `wif` (workload identity federation) mint modes are removed
(ADR-042). The Headscale profile is unchanged: it still
mints with a Headscale API key.

Removed: `tw up --wif`, `--wif-client-id`, `--wif-lifecycle-client-id`,
`--issuer-authkey-file`, `--login-timeout`, and `--api-key-file` outside
`--control=headscale`; `tw tenant set --wif-client-id` and
`--wif-lifecycle-client-id`; `tw doctor --tailnet/--api-key-file`; mgmt's
OIDC issuer (`tw-mgmt -issuer-url`, `-issuer-listen`, `-funnel`,
`-issuer-tsnet-*`, and the discovery and JWKS endpoints);
`POST /v1/tenants/{id}/wif-check`; the tenant fields `wifClientID`,
`wifLifecycleClientID` and `wif`; `tw-netd -mint-api-key-file` (now
`-mint-oauth-client-file`). New: `tw tenant add --oauth-client-file`,
`tw tenant set --oauth-client-file`, and the error code
`mint_mode_removed`.

Operator action: if `tw doctor` fails its `mint_mode` row, or `tw up`
says `mint_mode_removed`, the install mints with an API key or WIF. Create
an OAuth client (docs/INSTALL.md, "The OAuth client"), then run
`tw up --oauth-client-file <file>`. This keeps the tenant, its deployments
and mgmt's node. Do the same for each added tenant with
`tw tenant set <tenant> --oauth-client-file <file>`. Then revoke the old
API key, or delete the WIF trust credentials, the `tw-issuer` machine and
the `tag:tw-issuer` policy entries. Running nodes keep serving until then,
but nothing new can be minted.

#### rename-twd-to-tailworks-86

twd is now **tailworks** (issue #86). The project, the repo
(`github.com/rosskukulinski/tailworks`), the Go module, the npm packages
(`@tailworks/tailnet`, `@tailworks/node`, `@tailworks/mcp`,
`npm create tailworks`), the install root (`~/.tailworks`), the per-app
config (`tailworks.jsonc`, top-level key `"tw"`) and the release tarballs
(`tailworks-<version>-<os>-<arch>.tar.gz`) use the long name. The command is
`tw` (`install.sh` also links `tailworks` → `tw`), and everything else uses
the short name: the daemons (`tw-mgmt`, `tw-hostd`, `tw-netd`,
`tw-vmhelper`, `tw-vz`, `tw-guestd`), env vars (`TW_*`), tags (`tag:tw`,
`tag:tw-mgmt`, `tag:tw-host`, `tag:tw-public`, `tag:tw-wl-*`), the Service
`svc:tw-mgmt`, node hostnames (`tw-`), headers (`x-tw-*`, `Tw-Error`,
`Tw-Actor`, `Tw-MCP-Tools`), metrics (`tw_*`), the launchd label
`dev.tailworks.up` and the systemd units `tw.service` and
`tw-vmhelper.service`. Peer capabilities move from `twd.dev/cap/*` to
`github.com/rosskukulinski/tailworks/cap/*`.

There is no upgrade path from twd 0.x: nothing reads `~/.twd`,
`twd.jsonc`, the old env vars, tags or headers.

Operator action: tear down and reinstall. On each machine, run
`twd down --purge --service`, remove any hand-made agents
(`dev.twd.hostd`, `twd-vmhelper.service`) and delete `~/.twd`. In the
admin console, remove the `tag:twd*` devices, then the `tag:twd*` tag
owners, `svc:twd-mgmt` and the `twd.dev/cap/*` grants; revoke the OAuth
client and create one scoped to `tag:tw`. Rename each app's `twd.jsonc` to
`tailworks.jsonc` (key `"tw"`) and its `@twd/*` imports to
`@tailworks/*`. Then install with `get.sh` and run `tw doctor`.

#### upgrade-prefix-health-issue-63

`tw upgrade` on a `tw up` machine now replaces the install that runs the state dir (the binary the service or the running supervisor was started from, or the state dir itself), not whichever directory the `tw` binary happens to live in. Running it from an extracted release, as the first upgrade from 0.1.0 needs, used to plan against the extracted directory; it now refuses with `prefix_mismatch` unless `--prefix` names the install. docs/upgrading.md covers the first upgrade.

The health check after the restart now only waits for deployments that were active before the upgrade. One that had already failed no longer rolls a good upgrade back; `--plan` and the start of the run warn about it, and the result lists it (`notActiveBefore` in `--json`) (#63).

#### workerd-restart-issue-43

A workerd that crashes after its deployment has served is now restarted until it comes back. hostd retries with exponential backoff (200 ms doubling to 5 min) and never gives up. Before, six quick exits marked the deployment `failed` for good. Before each restart hostd kills the old workerd's process group and waits for it to empty. The stale ingress socket is then removed, so a restart no longer fails with `bind(): Address already in use`. hostd also no longer misses an exit when a child process still holds workerd's log. From the second exit in a row the deployment reports a new status, `crashloop`, and its error gives the attempt count, the next delay and the last log lines. `tw ls`, `--json`, the console and the API show this status. The deployment goes back to `active` by itself once workerd answers again. `tw deploy` stops waiting with `deploy_crashloop` when the deployment enters a crash loop. A deploy whose workerd never becomes healthy (a bad bundle or config) still ends `failed` within a few seconds.

Operator action: clients that match on deployment status should treat `crashloop` as "assigned here, not serving right now". A deployment left `failed` by this bug on an older host needs one `tw deploy` after the upgrade.

#### workflows-approvals

Cloudflare **Workflows** (wrangler `workflows`) run on workerd's built-in engine, and `step.waitForApproval` is decided by tailnet identities holding `github.com/rosskukulinski/tailworks/cap/approve` (`tw approvals`, `tw approve`; ADR-040).

Operator action: workerd moves to 1.20261114.1.

### Features

#### agent-first-dx-issue-25

Coding agents can drive tailworks: `tw agent setup` installs skills, `--json` output is a stable, documented contract (docs/cli-json.md, `schema/cli.schema.json`), errors carry `hint` and `doc_url`, `tw docs` searches the docs offline, and tw-mgmt serves an MCP server at `/mcp` (ADR-035).

#### app-testing-issue-47

Testing tailworks apps (docs/testing.md). `@tailworks/tailnet/testing` adds
`withCaller` / `withPublic` and a fake `ctx.tailnet` for unit tests, in Node
or under `@cloudflare/vitest-plugin`, which runs tailworks apps' Worker and Durable
Object logic as on Cloudflare. `tw dev --caller <name>[=<tag>,…]` adds extra
dev identities, each with its own local URL, listed as `callers` in the
`ready` event and in `tw dev --once --json`; `@tailworks/tailnet/testing/dev`
(`startDev`) runs an app under `tw dev` for black-box tests. Every template
now has a test and an `npm test` script. `tw dev` no longer misses an edit
made right after its `ready` event.

#### approver-ux-issue-50

Approvers no longer need a tenant ID, and they hear about waiting approvals
(issue #50).

- `GET /v1/whoami` now returns `tailnet` (the caller's own), `tenants` (the
  tenants on that tailnet; every tenant for the local operator) and
  `approveGrants` (the caller's `github.com/rosskukulinski/tailworks/cap/approve` grants). The admin
  UI's approvals page and `tw approvals` / `tw approve` from a linked
  machine default to those tenants; the page hides the tenant box when
  there is exactly one.
- `tw notify set --webhook -` sets a tenant webhook (stored in custody;
  only its origin is ever shown). When a Workflow starts waiting for an
  approval, mgmt POSTs a JSON `approval_requested` event with a deep link
  to the request's row (`/ui/approvals?tenant=…#<instance>`); Slack-style
  incoming webhooks show its `text`. New API: `GET/PUT/DELETE
  /v1/tenants/{id}/notifications`, `POST …/notifications/test`; new audit
  kinds `notification_put`, `notification_delete`, `notification_failed`,
  `notification_test`.
- `GET /v1/audit?order=desc` lists newest first. The console's audit page
  uses it and combines tenant, event, caller, object, trace and app
  filters; a new **Objects** page filters by tenant, app, status, tag and
  acting-for, with paging; every activity list is newest first.
- The approvals page matches the rest of the console: card filter bar,
  facts as a definition list, an anchor per row, and "What you may decide".
- Browsers on public (Funnel) routes get a minimal HTML error page (status,
  code, docs link, nothing about the request) instead of JSON when they ask
  for `text/html`. JSON stays the default.
- `tw dev` serves a read-only data console on `127.0.0.1` (press `e`, or
  open the printed `console on …` URL): Workers KV namespaces, keys and
  values, and D1 tables and rows of the run. `--console-port <port>` picks
  the port (default: a free one); `--console-port -1` turns it off. With
  `--json`, a `console` event gives its URL.

#### benchmark-dx-infra-vs-cloudflare-workerd-vercel

`tw dev` prints each request with its `console.*` lines and the stack of any uncaught exception (`--json`: a new `request` event), and picks up edits within about 0.2 s. An unknown command suggests the nearest one (`tw tail` → `tw logs`), `--help` shows each flag's type, and `tw ls` shows why a deployment failed. `tw init` names `package.json` after the app and adds `npm run dev`. The admin console and `tw audit` render audit records the same way (no raw JSON); the approvals page has one comment box with Approve and Reject buttons. `tw docs search` ranks the guides above the API contract and the ADRs.

#### cli-ergonomics-issue-46

`tw completion bash|zsh|fish` prints a tab-completion script for every command, subcommand (`kv key put`, `d1 migrations apply`) and flag, generated from the CLI's own command table. `tw whoami [--json]` shows who the CLI acts as (your tailnet login through `tw link`, or the local operator), the mgmt it talks to, the tenant, and what that identity may do: admin, approve and the MCP tools. `GET /v1/whoami` adds `caps`, `approver` and `mcpTools` for it. `tw help` groups the commands (Set up · Develop · Deploy · Data · Observe · Operate) with one short line each; `tw help --all` prints every usage line. `tw version --check` compares with the latest GitHub release, prints `tw upgrade`, and says whether the running `tw up` needs a restart; an interactive `tw` prints a newer-release notice at most once a day (off with `--json`, `CI`, or `TW_NO_UPDATE_CHECK=1`). From a source checkout, `tw doctor` and a `build_failed` for an unresolved `@tailworks/*` import point at `make deps`.

#### critique-developer-experience-admin-deployers

Worker secrets (`tw secret`, ADR-034), `tw logs` from a linked machine, and many developer-experience fixes.

#### d1-databases

**D1** databases (wrangler `d1_databases`) with wrangler-layout migrations: `tw d1` creates, queries, exports and migrates them (ADR-038).

#### deploy-history-rollback-issue-45

`tw deployments <workload>` lists a workload's deployment history, newest
first: id, created, who deployed, bundle SHA-256, status, error and host.
`tw rollback <workload> [<deployment>]` redeploys an earlier deployment's
stored bundle as a new deployment, through the same path as `tw deploy`
(placement, lease epoch, host fencing, preflight); with no ID it goes back
to the previous deployment that ran. It is audited as `kind: rollback`. A
rollback changes code and configuration only: Durable Object storage, D1
(migrations are forward-only), KV, R2 and queues keep their data, and
secrets are the workload's current ones (ADR-046).

mgmt now keeps the bundles of each workload's newest 10 deployments and
prunes older ones, keeping their record and hash (tw-mgmt
`-bundle-retention`, `-1` keeps every bundle). New API:
`GET /v1/tenants/{id}/workloads/{workload}/deployments` and
`POST /v1/tenants/{id}/workloads/{workload}/rollbacks`.

Operator action: none. The store migration adds columns to `deployments`.
The first deploy of each workload after the upgrade prunes that workload's
bundles beyond the newest 10; pass `-bundle-retention -1` to tw-mgmt
beforehand to keep them all.

#### deploy-plan-issue-73

`tw policy [dir]` prints everything an app needs in the tailnet policy, as
one snippet, before it is deployed. It runs the same check as `tw deploy`'s
preflight and deploys nothing. The snippet holds the missing `tagOwners`, a
grant for each `tailnet.connects`, the Tailscale Service auto-approval and
the public-ingress `nodeAttrs`, plus the grant that lets people call the app
(which tailworks can't check). It exits `acl_denied` while a checked part is
missing, so CI can run it before a deploy. `tw init` and the README's first
deploy now use it, so a first deploy takes one policy edit. With `--json`
the result is `$defs/policy`.

#### design-component-upgrade-release-process

You can now plan, check and undo upgrades (ADR-041).

- **Versions everywhere.**
  - Every binary carries the release version (`--version`).
  - Hosts report their version, protocols and features to mgmt, and mgmt
    replicas record their own versions.
  - `/v1/status` shows every version running in the fleet.
- **Skew policy.** A host may run one release line behind mgmt, but never
  ahead of it.
  - mgmt refuses a host outside that window with `version_skew`, and places
    nothing new on it.
  - The CLI warns you when it is too old or too new for mgmt.
- **Numbered mgmt migrations.**
  - The store records each migration it applies in `schema_migrations`.
  - A binary older than the store's minimum version refuses to start, so a
    downgrade can't corrupt the store.
  - `tw-mgmt migrate --check` lists the pending migrations.
- **Feature-gated placement.** A deployment that uses KV, R2 or D1 goes
  only to hosts that advertise that feature. If no host does, the deploy
  fails with `feature_unavailable`.
- **Host cordon and drain.** `tw host cordon`, `uncordon` and `drain`
  take a host out of placement and move its workloads off it before
  maintenance.
- **`tw upgrade`.**
  - It plans the upgrade, then backs up the store and custody together.
  - It installs and restarts the new release, then health-checks it.
  - It rolls back automatically if the health check fails.
  - `install.sh` keeps the previous install as `bin.prev` and `share.prev`,
    and `install.sh --rollback` restores it.
- **Release catalog.** tw-mgmt embeds the notes and upgrade facts of every
  release up to its own. `tw release import` adds a newer one, so
  `tw upgrade --plan` works offline.
- **Upgrade runs.**
  - mgmt drives a fleet upgrade host by host, starting with a canary and
    health-checking each host before the next.
  - The run keeps going if the CLI disconnects: it is stored in mgmt and
    driven by whichever replica holds its lease.
  - A host started with `tw-hostd -self-upgrade`, under a service manager,
    downloads the release tarball from mgmt, installs it and restarts.
    Other hosts show an operator step, and the run waits for them.
  - `tw upgrade status`, `pause`, `resume` and `abort` control the run.
- **Versioned data formats.** `objects.json`, node `object.json`,
  `tw.json` and the R2 index entries now carry a version. A `tw` reads older
  formats and refuses newer ones.

Operator action: none for a homelab. For a fleet, upgrade tw-mgmt before
any host. Hosts that predate version reporting count as 0.1.x, so they stay
within the window of a 0.2 tw-mgmt. See docs/upgrading.md.

#### docs-product-issue-51

Docs: runnable recipes (`docs/recipes.md`: internet APIs and LLMs through a tailnet gateway, a Durable Object per user authorized by `getCaller`, a signed public webhook, cron into KV), a CLI reference generated from every command's `--help` (`docs/cli.md`, found by `tw docs search`), a limits page with the code behind every limit (`docs/limits.md`), a README in every template, a one-page security model (`docs/security-model.md`) and a private disclosure process (`SECURITY.md`), and a user-facing `CHANGELOG.md` rendered from the release catalog. `tw docs search` now finds the README's quick start, the changelog and the security policy (`tw docs show ../README.md`).

#### implement-adr-15-head-scale

tailworks runs on a self-hosted **Headscale** control plane (ADR-015): `tw up --control=headscale` and `tw tenant add --control=headscale`. It needs Headscale v0.29 or later. There is no Funnel (public routes) and there are no Tailscale Services there; see docs/headscale.md.

#### implement-do-lease-locks-issue-26

Distributed locks and leader election on Durable Object leases: measured guarantees in docs/locks.md, and a `lock` template whose fencing tokens carry the lease epoch.

#### implement-queues-issue-18

Cloudflare **Queues** (wrangler `queues`): producers, push consumers, retries and dead-letter queues, on a sealed, lease-fenced broker in tw-mgmt (`tw queues`; ADR-039).

#### npm-packages-types-issue-41

**Typed templates and `tw types`.** Every `tw init` template has a `tsconfig.json` and `@cloudflare/workers-types` and `typescript` dev dependencies, so `npm install && npx tsc --noEmit` passes and editors know `getCaller`, `Request`, `DurableObjectState` and `ctx.tailnet` (`@tailworks/tailnet` now declares `ctx.tailnet` on `DurableObjectState` and `ExecutionContext`). New `tw types`, like `wrangler types`, writes `worker-configuration.d.ts` with the app's `Env` (vars, Durable Object, KV, R2, D1, Queue and Workflow bindings, secret names from `.dev.vars`, `.dev.vars.example`, `secrets.required` or `--remote`); `tw types --check` fails with `types_stale` in CI. The `@tailworks/*` packages and `create-tailworks` are ready to publish to npm, and `npm create tailworks` runs `tw init` when tailworks is installed.

#### platform-stores

Platform stores: a SQLite Durable Object per store, backed by a fenced, encrypted journal in tw-mgmt, so a store survives restarts and moves between hosts. KV, D1, R2 and Queues build on it.

#### preview-deploys-issue-45

`tw deploy --preview` runs a bundle next to the workload instead of
replacing it, as the workload `<workload>--preview-<alias>` (the alias is
`--preview-alias`, or the start of the bundle's SHA-256). A preview has the
workload's tags and secrets, but its own lease, Durable Object storage and
KV, R2 and D1 stores, which start empty; it joins no Tailscale Service, has
no public routes and runs one replica. Bundles that bind queues can't be
previewed (queues are tenant-wide). `tw previews <workload>` lists the
running previews. `tw promote <workload> <alias>` deploys the preview's
bundle to the workload through the same path as `tw deploy` (lease epoch,
host fencing, preflight), audited as `kind: promote`, then undeploys the
preview (`--keep-preview` keeps it). The preview's data is not copied
(ADR-048).

New API: `POST /v1/deployments` `preview` and `previewAlias`,
`GET /v1/tenants/{id}/workloads/{workload}/previews` and
`POST /v1/tenants/{id}/workloads/{workload}/promotions`.

#### r2-bucket-binding-issue-16

**R2** buckets (wrangler `r2_buckets`): the index is a platform store, and the bytes go to a host `fs` or `s3` backend (`-r2-backend`; `tw r2`; ADR-036). Bytes on the `fs` backend stay on their host.

#### workers-kv-binding-issue-15

**Workers KV** (wrangler `kv_namespaces`): the KV API inside Workers and `tw kv` for operators. Read-your-writes on the namespace's host (ADR-037).

#### workflows-approvals

Cloudflare **Workflows** (wrangler `workflows`) run on workerd's built-in engine, and `step.waitForApproval` is decided by tailnet identities holding `github.com/rosskukulinski/tailworks/cap/approve` (`tw approvals`, `tw approve`; ADR-040).

Operator action: workerd moves to 1.20261114.1.

#### wrangler-cli-integration-issue-19

tailworks works with the wrangler CLI: `wrangler.toml`, `--env`, and apps bundled by wrangler (`--wrangler`, `--from-wrangler-out`); see docs/wrangler.md.

### Fixes

#### docs-first-deploy-issue-48

Docs for a first deploy. The README quick start now grants callers access to the workload (without that grant, the `curl` hangs on any tailnet whose policy isn't allow-all), and `tw deploy` prints that grant next to the tagOwners lines it prints for new tags. New pages: `docs/troubleshooting.md` (a first deploy that doesn't answer, `identity_pending`, tagOwners after renaming an app, `peer_not_visible`, a `failed` deployment), `docs/concepts.md` (a glossary) and `docs/install-testing.md` (the Linux support matrix and measured timings, moved out of `docs/INSTALL.md`). INSTALL's intro is shorter, and its isolation notes and `tw dev` section come after the install steps.

#### fix-linux-ci-tests-issue-75

Two netd fixes found while making the test suite pass on Linux.

- **Joins after a restart no longer hang.** A node whose saved key control
  had expired could stay in `NoState`, retrying its login, without ever
  reaching NeedsLogin. Its rejoin then never ended, and it held a join slot
  while it waited. A login error that persists during a rejoin now counts as
  a rejection, and the object is re-minted. Join slots are now handed out in
  the order objects were created.
- **The first call to a just-created object no longer takes up to 5 s.** An
  object that calls one created a moment ago now waits for the new node to
  answer a disco ping, not just for a path to it. Without this, the first
  WireGuard handshake could arrive before the new node knew the caller, and
  was retried only after 5 s.

#### fix-orphan-workerd-issue-44

`tw dev` no longer leaves workerd running and `/tmp/tw-dev-*` behind when it is killed with SIGKILL (an IDE, a coding agent or a CI timeout). A small watchdog stops its workerd and node processes and removes the dir however `tw dev` exits, and the next `tw dev` cleans up after any earlier run whose process is gone. Another `tw dev` that is still running is left alone. The hostd and netd test helpers do the same for their workerd processes (#44).

#### fix-web-ui-refresh-jitter-restyle

Admin UI: no more jitter on refresh, a more readable layout, and it uses only the public `/v1` API.

#### prelaunch-102-license-notices

Release tarballs now include the project's `LICENSE` and a `THIRD_PARTY_NOTICES` file. The notices cover workerd, the Go modules compiled into the binaries, the vendored JavaScript packages and, where a tarball includes the microVM guest, Firecracker, the Linux kernel and the guest image's Ubuntu packages. Tarballs with a microVM guest also include `share/tailworks/vm/KERNEL-SOURCE.md` and `ROOTFS-SOURCE.md`, which say where to get the corresponding source and include a written offer for it. The guest image now carries the Debian copyright files of the Ubuntu packages it takes files from (in `/usr/share/doc`), and its `manifest.json` lists those packages (#102).

#### prelaunch-104-update-check

`tw init` and `tw dev` no longer run the once-a-day update check, so they make no outbound request (before, they could ask `api.github.com` for the latest release after they finished). `tw version --json` adds `updateCheck`: whether this tw runs the check (a release build without `CI` or `TW_NO_UPDATE_CHECK` set). The README ("What tw sends") and `docs/security-model.md` ("Telemetry and the update check") now say what the check sends, when it runs and how to turn it off; tailworks has no other telemetry.

#### prelaunch-93-audit-chain

`tw audit` and audit verification no longer report an intact chain as
broken while records are being appended (#93). Verification read a chain's
records and its head in two separate queries, so a record appended between
them made the head look one record ahead and the chain failed with "chain
head mismatch" (`audit_unverified`). Both are now read in one read
transaction. Detection of real truncation and tampering is unchanged.

#### upgrade-prefix-health-issue-63

`tw upgrade` on a `tw up` machine now replaces the install that runs the state dir (the binary the service or the running supervisor was started from, or the state dir itself), not whichever directory the `tw` binary happens to live in. Running it from an extracted release, as the first upgrade from 0.1.0 needs, used to plan against the extracted directory; it now refuses with `prefix_mismatch` unless `--prefix` names the install. docs/upgrading.md covers the first upgrade.

The health check after the restart now only waits for deployments that were active before the upgrade. One that had already failed no longer rolls a good upgrade back; `--plan` and the start of the run warn about it, and the result lists it (`notActiveBefore` in `--json`) (#63).

#### workerd-restart-issue-43

A workerd that crashes after its deployment has served is now restarted until it comes back. hostd retries with exponential backoff (200 ms doubling to 5 min) and never gives up. Before, six quick exits marked the deployment `failed` for good. Before each restart hostd kills the old workerd's process group and waits for it to empty. The stale ingress socket is then removed, so a restart no longer fails with `bind(): Address already in use`. hostd also no longer misses an exit when a child process still holds workerd's log. From the second exit in a row the deployment reports a new status, `crashloop`, and its error gives the attempt count, the next delay and the last log lines. `tw ls`, `--json`, the console and the API show this status. The deployment goes back to `active` by itself once workerd answers again. `tw deploy` stops waiting with `deploy_crashloop` when the deployment enters a crash loop. A deploy whose workerd never becomes healthy (a bad bundle or config) still ends `failed` within a few seconds.

Operator action: clients that match on deployment status should treat `crashloop` as "assigned here, not serving right now". A deployment left `failed` by this bug on an older host needs one `tw deploy` after the upgrade.

## v0.1.0 (2026-11-15)

workerd 1.20261108.1.

### Features

#### initial

First release. It includes `twd up` (the all-in-one homelab mode),
`twd dev`, and twd-mgmt and twd-hostd on the process and microVM tiers,
running the pinned workerd.
