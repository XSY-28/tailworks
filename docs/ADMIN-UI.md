# twd admin UI

twd-mgmt serves a small admin web UI next to its `/v1` API. It is rendered
by mgmt itself from Go `html/template` pages and two embedded static files
(one CSS, one vanilla-JS); there is no build step, no Node, no CDN, no
external font, and nothing is stored in the browser. The code is
`mgmt/ui`; it is mounted by `mgmt.Server.Handler` (`mgmt/v1.go`).

## URL and who gets in

    http(s)://twd-mgmt.<tailnet>.ts.net/ui/

Open it from any admin's browser over the tailnet. `GET /` from a browser
(`Accept: text/html`) redirects there; API clients hitting `/` still get
the typed `not_found`.

Admission is the connection's WhoIs identity, exactly as for the API
(`mgmt/server.go` `TailnetHandler`):

| principal | result |
|---|---|
| a user or tagged node holding `twd.dev/cap/admin` (granted to `tag:twd-mgmt`; `autogroup:admin` gets it from the base snippet), or a login in `-admins` | 200, the UI |
| mgmt's own node (the operator console on the host machine) | 200, the UI |
| a user or tagged node holding only `twd.dev/cap/approve` (toward `tag:twd-mgmt`) | the **approvals** page only (with `?tenant=<tenant ID>`); 403 `forbidden` elsewhere |
| any other tailnet user | 403 `not_admin`, with the grant snippet to add |
| a `tag:twd-host` node (registered or not) | 403 `forbidden` (or `host_not_registered`); hosts never reach the UI or its assets |

There is no login, no cookie and no token: the tailnet connection is the
credential, so the UI shows exactly what the API would show the same
caller. Every page is built by running in-process requests through the
`/v1` handler with the caller's principal and decoding the JSON (one
source of truth: `api/openapi.yaml`, `docs/contracts/mgmt-api.md`).

## Pages

- **`/ui/` overview**: mgmt itself (DNS name, tailnet, version, listeners,
  transport, WIF issuer, the tenants' mint modes); the audit chain
  verification; **hosts** (status live / pending / stale, heartbeat age,
  nodes live/capacity, tiers, pool, push transport, leases, node ID);
  **tenants** (tailnet, mint mode, quotas, registrations/min and the
  effective rate, mint queue, pool/tier); **deployments** (latest per
  workload: workload, deployment ID, tenant, host, deployed by, age,
  status, endpoint MagicDNS link and status); **objects** as a lineage tree
  across every tenant (indented by depth; status badge; node name linking
  to the object page; MagicDNS link; tags; TTL; short node ID; acting-for;
  age; killed and failed rows dimmed); the **25 most recent audit
  records** with `twd audit`'s descriptions.
- **`/ui/objects/{id}`**: every field of the object, the lineage up (root
  to parent), the subtree below (this object and its descendants), caps,
  the node certificate when the object has a node, the object's audit
  records with chain status, and the **Kill** form (cascade checkbox,
  reason). A killed object renders its tombstone / kill record (the
  `/v1` 410 body: killed at, by, reason, cascade root, device deletion)
  and offers no kill form.
- **`/ui/deployments/{id}`**: the deployment, its endpoint, namespaces
  (class, tags, agent, ttl), the objects of the workload, and
  **Undeploy** (reason) unless already stopped or superseded.
- **`/ui/hosts/{id}`**: heartbeat, nodes, tiers, pool, push transport,
  node, the leases it holds, its assignments, the live objects it runs, and
  **Drain & remove** (drain checked by default; without it the API refuses
  with `host_holds_leases` while leases are held). This is the scale-out
  view: every host registered with this mgmt, live or not.
- **`/ui/tenants/{id}`**: the configuration as the API returns it (mint
  mode, client IDs and file paths, quotas, OBO public key; secrets are
  never returned by the API and so never shown), object counts by status,
  deployments, objects, and **Delete tenant** (force checkbox).
- **`/ui/approvals`**: the Workflow approvals waiting in every tenant the
  caller can list (or `?tenant=`), oldest first: workload, Workflow,
  instance, step, the instance's numeric params, age, host; `all` adds the
  decided ones with who decided. Each waiting row has **Approve** and
  **Reject** forms with a comment. The decision is made with the browser's
  own tailnet identity and checked against its `twd.dev/cap/approve` grants
  by the host running the Workflow; an admin without a grant gets
  `cap_missing`, and the operator console `no_identity`
  ([approvals](approvals.md)).
- **`/ui/audit`**: one filter at a time, like `twd audit`: object, human
  (acting-for login), trace, or all; page size; `nextCursor` paging that
  keeps the filter; the verification report and the per-chain status
  (`ok`, `signature ok` for host chains, `BROKEN at <seq>`). Tenant-written
  notes are rendered as `~tenant_note` and labelled unverified (RT-02).

Vocabulary and status colours follow the CLI: ready / live / running
green, pending / minted / stale amber, killed / stopped / superseded grey,
failed red. IDs are monospace; the full ID is in the cell's tooltip when
abbreviated. Light and dark follow `prefers-color-scheme`.

## Mutations

Each action is a plain HTML form. With JavaScript, `app.js` intercepts the
submit, asks for confirmation, and performs the same `/v1` call the CLI
makes with `fetch`:

| form | call |
|---|---|
| Kill | `DELETE /v1/objects/{id}?cascade=true&reason=…` (202) |
| Undeploy | `DELETE /v1/deployments/{id}?reason=…` (202) |
| Drain & remove | `DELETE /v1/hosts/{id}?drain=true` (202 / 204) |
| Delete tenant | `DELETE /v1/tenants/{id}?force=true` (202 / 204) |
| Approve / Reject | `POST /v1/tenants/{id}/workloads/{w}/approvals/{workflow}/{instance}` with JSON `{decision, comment}` (200) |

The result is a toast; a refusal shows `code: message` and the error's
`doc_url` (docs/errors.md). Without JavaScript the form POSTs to its
`/ui/.../kill` (etc.) action, which performs the identical call in-process
and redirects back with the outcome. The actor recorded in the audit trail
is the WhoIs login (tagged nodes: tag and node), as for the CLI.

`app.js` also refreshes the page's live regions every 3 seconds by
re-fetching the page (the server's templates stay the single renderer) and
updates the header counters from `GET /v1/status`. Pages are complete and
readable with JavaScript off; only the live refresh and the toasts need it.

## Browser-origin guard (CSRF, DNS rebinding)

The API has no cookies, so a classic CSRF cannot carry a credential; the
risk is a page on another origin (or a rebound DNS name) driving the
admin's tailnet connection. `mgmt.Server.Handler` wraps everything,
API and UI, in a guard (`browserGuard`, `mgmt/server.go`):

- Requests that are not browser-originated (no `Sec-Fetch-Mode`, no
  `Origin`) are untouched: the `twd` CLI and `twd-hostd` send neither and
  are never affected.
- `GET`, `HEAD` and `OPTIONS` are never guarded.
- A browser-originated mutation must have `Sec-Fetch-Site` in
  `{same-origin, none}` (when present) **and** an `Origin` whose host equals
  the request's `Host` (when present); `/v1` mutations must additionally
  carry `X-Twd-UI: 1`, which `app.js` sets. A custom header forces a CORS
  preflight that mgmt never answers, so cross-origin JavaScript cannot
  reach the handler at all; the no-JS `/ui/...` form posts are covered by
  the `Sec-Fetch-Site` / `Origin` checks.
- Anything else is 403 `forbidden` with the message `cross-site request
  refused: …`. (The spec's `ErrorCode` enum is closed, so no separate
  `cross_site_request` code was added.)

Every UI response carries
`Content-Security-Policy: default-src 'self'; img-src 'self' data:; frame-ancestors 'none'`
(no inline script or style anywhere in the templates),
`X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`,
`X-Frame-Options: DENY` and `Cache-Control: no-store`. The UI stores nothing
client-side: no cookies, no localStorage, no tokens.

## Tests

- `mgmt/ui/*_test.go`: every page template over an empty and a populated
  fake `/v1` (including a 410 tombstone, a broken chain and a hostile
  tenant note), the no-JS form fallbacks, the static assets and headers,
  the tree and formatting helpers.
- `mgmt/ui_integration_test.go`: a real `Server` over the SQLite store with
  a fake WhoIs: admission per principal, the `/` redirect, the browser
  guard (cross-site refused, same-origin with `X-Twd-UI` and CLI-style
  requests succeed), the tombstone page, and every `/v1` call the UI makes
  passing the kin-openapi validator.
- `mgmt/approvals_test.go`: the approvals page per principal (approver,
  admin, no grant), and the no-JS Approve / Reject fallback forwarding the
  browser's identity to the host.

    go test -race -count=1 ./mgmt/...
