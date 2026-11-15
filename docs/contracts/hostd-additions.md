> **Note (M11):** this file describes the v0 era. The mgmt API is now `/v1`, specified in `api/openapi.yaml`; `mgmt-api.md` is **generated** from it (`make api`). The hostd-side facts below (audit hash input, push API, platform wrapper sockets) still hold; the `/v0` route names are historical and map to `/v1` per the spec's "Replaces …" notes.

# Contract: twd-hostd additions (M3, backward compatible)

These extend `mgmt-api.md` (v0 + the M2 "v0 clarifications"). All of them are optional: M2's twd-mgmt (origin/m2-mgmt b08b5e4) works with twd-hostd unmodified (`make test-m2mgmt`). The one exception is the workload entry node; see gap 1.

## What hostd sends and expects (it matches M2)
- **Host header.** Every request carries `Twd-Host-ID: <hostID>`.
- **Register.** `auditPubKey` is base64 ed25519. `hostSig` = base64(ed25519.Sign(key, utf8(hash))). The hash is the canonical sorted-key hash of the record JSON without `hash`/`hostSig`.
- **Heartbeat.**
  - `leases:[{namespace:"<tenantID>/<workload>", epoch}]`, one per workload.
  - Kills arrive in `revokeObjects`, and hostd's kill is idempotent.
  - If mgmt revokes a lease while a newer assignment for the same workload is on this host (a redeploy bumped the epoch), hostd adopts the new epoch instead of fencing itself.
- **Objects.**
  - `POST /v0/objects` `namespace` is the **DO class name**.
  - hostd proposes `hostname`. If the response carries `hostname`, hostd uses it; M2 uses the object ID.
  - `GET credential?epoch=N&wait=10` is retried while it returns `identity_pending`, 429 or 503.
  - `POST ready` carries `epoch`. A 410 `killed` from ready makes hostd kill the node locally.

## Additions (optional fields)
| Where | Field | Meaning |
|---|---|---|
| register | `pushURL` | `unix:///…/hostd.sock`. When set, mgmt may push `POST /v0/push/kill {objectID, reason}` and `POST /v0/push/assignments {}`. This is the fast kill path (~1 ms tier 1 vs ≤ one heartbeat). M2 does not push yet (gap 2) |
| heartbeat req | `deployments:[{deploymentID,status,error?}]` | `starting\|active\|stopped\|failed` |
| heartbeat resp | `kill:[{objectID,reason}]` | Same as `revokeObjects`, with a reason (fakemgmt only) |
| assignment | `workloadTags` | Tags of the workload entry node (twd.jsonc `tailnet.tags`) |
| assignment | `service` | Tailscale Service the workload entry node advertises while the deployment is active and healthy (twd.jsonc `tailnet.service`; [services.md](../services.md)) |
| deployment | `GET /v0/deployments/{id}` | `{deploymentID, host, endpoint, status}` |
| tenant | `mintMode:"static"` | fakemgmt only: a fixed key for hermetic testcontrol runs |

## Gaps that need an M2 change
1. **Workload entry node.** Every deployment gets a node for its default export: the "hello" node, whose egress is the Worker's identity.
   - hostd creates it as `POST /v0/objects {namespace:"_workload", objectName:<workload>}`.
   - M2 resolves namespaces by class, so today the deploy must declare `namespaces:[{class:"_workload", tags:[…]}, …classes]` explicitly. `make test-m2mgmt` does this.
   - M2 rejects bundles that have no `tailnet.objects` ("no namespaces"), so a plain Worker like `hello` can't be deployed to M2 as-is.
   - **Ask:** mgmt implicitly declares `_workload` with `tailnet.tags` for every deployment.
2. **Kill latency.** With heartbeat delivery only, `twd kill` → tier 1 waits up to one heartbeat (3 s).
   - **Ask:** support `pushURL` (above) or a long-poll `GET /v0/hosts/{id}/events`.
   - `ctx.tailnet.kill()` from inside an app is immediate on its own host either way.

## hostd ⇄ platform wrapper (local, per workerd instance; not a network API)
`hostapi.sock`, which only the trusted platform worker can reach (as the `HOSTD` binding).

**Ops.** `POST /v1/{ensure,spawn,ready,kill,audit,self}` with JSON `{as?:{class,name}, …}`. `as` is the calling object, attested by the platform's `TailnetApi` props.

**Errors.** Typed `{code, message, detail, doc_url}` with a `twd-error` header.

**Egress sockets.**
- `egress-obj.sock` (facets): `x-twd-class`, `x-twd-name`, `x-twd-ts`, `x-twd-nonce` and `x-twd-mac`.
  - The MAC is HMAC-SHA256 under a per-process key over `class\nname\nmethod\nurl\nts\nnonce`, with ±2 min skew allowed.
- `egress-wl.sock`: the Worker and plain-DO objects. This socket is the workload node's identity.

## M4 status of the gaps
Both gaps are closed in twd-mgmt (see `mgmt-api.md` § "M4 additions"): `_workload` is implicit from `tailnet.tags`, and mgmt pushes kills to `pushURL`.

**M4 hostd changes.**
- Hostnames: when mgmt names the node (M2: the object ID), hostd still prepends `-hostname-prefix` (`twd-` by default under `twd up`; `twd-e2e-<run>-` in E2E-MAC).
- `actingFor`: the platform sends the first caller-bearing request's principal (the untagged caller's login, else the caller's node name) with `ensure`; hostd passes it to `POST /v0/objects`. A spawn acts for `principal` if given, else the parent's. If the object already existed (an RPC such as the Agents SDK's `__unsafe_ensureInitialized` came first), hostd writes a host audit record `kind:"principal"`.
- Kill tombstone: the killed node's ingress port now answers every new request with `410 {"code":"object_killed"}` (`twd-error: object_killed`, `Connection: close`) for the tombstone window, instead of a bare RST. Via the workload node, a call into a killed object is a typed `410 killed` (the runtime maps the platform's `[killed]` exception) instead of a 500.

## Push API authentication (M12, H1)
hostd serves two push surfaces, with different routes and different callers:

| Surface | Routes | Who may call |
|---|---|---|
| `<run-dir>/hostd.sock` (unix, mode 0600, `procsb.Guard`: no sandboxed workload may connect) | `POST /v1/push/kill`, `POST /v1/push/assignments`, `GET /v1/status`, `POST /v1/purge`, `POST /v1/vm/check` | the operator on this machine (`twd status`, `twd down --purge`, `twd host vmcheck`) and local-dev mgmt (`pushURL: unix://…`) |
| this host's own tailnet node, `-push-port` (7474) | `POST /v1/push/kill`, `POST /v1/push/assignments` **only** | mgmt's node |

The tailnet surface authenticates every request by WhoIs on the host's node (`hostd.Config.WhoIs`, set from the tsnet `LocalClient`); hostd refuses to start a tailnet push listener without it. A push is accepted only when:
1. WhoIs resolves the peer (no identity → refused), and
2. the peer's node carries `tag:twd-mgmt` (`hostd.MgmtTag`), and
3. the peer's stable node ID is the one hostd pinned as mgmt's: at `Start` hostd resolves the `-mgmt` address (a tailnet IP → WhoIs; a MagicDNS name → the netmap via `Config.TailnetStatus`) and pins that node if it carries the tag. If the address is not resolvable yet, the first `tag:twd-mgmt` pusher pins it. A later push from another `tag:twd-mgmt` node is accepted only if the `-mgmt` address now resolves to it (mgmt's node was recreated; re-resolved at most every 2 s), and re-pins.

Refusals are `403 {"code":"push_unauthorized","detail":…}` with `twd-error: push_unauthorized`, and one log line per refusal carrying the peer address and its tags (never key material). `GET /v1/status`, `POST /v1/purge` and `POST /v1/vm/check` are not routed on the tailnet surface at all (404 even for mgmt): the status dump (tenant IDs, node IDs/IPs, PIDs, dirs, ports) is the operator's.

## Embedded host (single-process mode, M13)
The default `twd up` runs mgmt and the host agent in **one process** (`twd __supervise`; `twd.json` `"mode": "single"`): mgmt's tsnet node is the only infrastructure device, the host has no node of its own, no join key and no registration secret. `twd up --separate-host` keeps the two-process layout above; `twd host add` machines are unchanged.

**Transport** (`internal/inproc`). hostd's `mgmtapi.Client` gets an `http.RoundTripper` that serves each request through mgmt's `/v1` handler (`mgmt.Server.Handler()`, i.e. *below* `TailnetHandler` / `LocalHandler`) in a goroutine with a piped body, so long polls (`POST /v1/objects/{id}/credentials?wait=`) and bundle downloads stream and honour the request context. The request context carries:
- `mgmt.WithPrincipal(ctx, Principal{Kind: "host", HostID: <hostID>})`: the host role with its ID, so the role gate, the `/v1/hosts/{id}` ID check, `requireLease` and `hostScope` apply exactly as for a host on its own tailnet node (an admin route is `403 forbidden`, another host's routes `403`); and
- `mgmt.WithEmbeddedHost(ctx, <hostID>)`: the embedded-host marker. It is a context value, so no listener can supply it: `TailnetHandler` and `LocalHandler` attach only a `Principal`.

`RemoteAddr` is the sentinel `inproc` (no `host:port`, so `RegisterHost` derives no node address).

**Registration.** The embedded host registers with `push: {url: "inproc://"}` (`mgmt.EmbeddedPushURL`). `RegisterHost` accepts it only when the context's embedded-host marker equals the host ID and the caller has no node ID; otherwise `403 host_identity_mismatch` with a `deny` audit record, whether the request came over the tailnet (a `tag:twd-host` node) or the local-dev socket. A record already bound to a tailnet node is never taken over (`403`; `twd host rm` first). The stored `Host` has `nodeID: ""`, `pushPort: 0`; the `host_register` audit detail carries `"transport": "embedded"`. `GET /v1/hosts/{id}` shows the host `live` by heartbeat as usual, with an empty `push` object (no port, no unix URL).

**Push.** `mgmt.Mgmt.SetInProcessPush(hostID, h)` registers the embedded host's push handler (`hostd.Host.PushHandler()`: `POST /v1/push/kill` and `POST /v1/push/assignments` only, no status/purge). A push to a host whose target is `inproc://` is served through that handler with a `PushTimeout`-bounded context and counted as delivered on a 2xx; nothing is dialled, and no `tag:twd-mgmt → tag:twd-host` grant is involved. hostd's local socket `<run-dir>/hostd.sock` is still served for the operator (`twd status`, `twd down --purge`).

**Lifecycle.** `twd __supervise` starts mgmt (`internal/mgmtd`, the same runtime as the `twd-mgmt` binary), builds the host (`internal/hostdrun`, the same as `twd-hostd`) with the in-process client, registers the push handler, then starts the host. SIGTERM stops the host first (nodes close in parallel), then mgmt (flushes pending device deletes). Each component logs to its own file (`<state>/logs/mgmt.log`, `hostd.log`).
