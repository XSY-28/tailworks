# ADR-051: Raw TCP egress: `connect()` handlers on the platform's egress entrypoints, CONNECT tunnels to hostd authenticated in band, dialled with netd's `DialEgress`

**Date:** 2026-11-23
**Status:** Accepted (2026-11-23, issue #294; built on branch
`tcp-egress-294`). Implements RFC §4.8 ("stock `connect()` egresses as the
workload node; `ctx.tailnet.connect()` egresses as the object") and §6.1
(`ctx.tailnet.connect()`), with the differences below.

**Context.**

- Workers had HTTP egress only. Stock `connect()` (`cloudflare:sockets`)
  failed with "cannot connect to the specified address" in the Worker, in
  facet-mode and in plain-DO objects. The tenant's `globalOutbound`
  (`WorkloadEgress`, and the facets' `Egress`) implemented only `fetch()`,
  and the external services behind them (`egress-wl`, `netd-egress`) carried
  HTTP. `ctx.tailnet.connect()` threw `plan_feature_missing`.
- That ruled out database and cache drivers on the tailnet (Postgres,
  MySQL, Redis), which are most of what Hyperdrive is used for.
- netd already has the policy for a bare `host:port`: `DialEgress` applies
  ADR-019 (the `classify` refusals, tailnet names from the caller's netmap
  only, Service VIPs), ADR-049's internet path with its SSRF guard and
  `tailnet.egress.internet`, and flow tracking so a kill resets the flow.
  The Node process tier's CONNECT proxy (`hostd/node.go`) uses it.

**Spike: what the pinned workerd (1.20261118.1) does with a socket.**

1. **A JS `globalOutbound` gets `connect()`.** When a Worker whose
   `globalOutbound` is a JS entrypoint calls `connect(addr)`, workerd calls
   that entrypoint's `connect(socket)` handler: a `WorkerEntrypoint` method,
   with `ctx.props` intact. `await socket.opened` gives the destination as
   `localAddress`.
2. **`connect()` on an `external` service with `http = (style = proxy)`
   sends `CONNECT host:port HTTP/1.1`** and, after a 200, carries the bytes.
   workerd sends nothing before the 200. It sends the CONNECT with **no
   headers at all**: `injectRequestHeaders` is not applied, so the loopback
   token (Landlock sandbox) can't ride in a header.
3. **A refusal can't carry a reason to stock `connect()`.** Throwing in the
   handler, closing the socket or aborting its streams all reach the tenant
   as "Network connection lost." Its `opened` resolves before the handler
   has decided.
4. **TLS doesn't cross a `connect()` handler.** `secureTransport: "on"`
   fails with "Incoming CONNECT with TLS not supported", and `startTls()`
   with "not supported on this connection".
5. **Workers RPC carries the streams.** A `WorkerEntrypoint` method can
   return a socket's `readable` and `writable` to the tenant isolate.

Option (c) of #294 (a different `globalOutbound` shape for sockets) isn't
needed: (a) and (b) together do it.

**Decision.**

1. **Stock `connect()` goes through the egress entrypoints.** `Egress` (a
   facet's `globalOutbound`, props `{class, name}`) and `WorkloadEgress`
   (the tenant service's) gain a `connect(socket)` handler. It opens a
   CONNECT tunnel to the same hostd endpoint as that entrypoint's `fetch()`
   (`netd-egress` → hostd's object egress, `egress-wl` → workload egress),
   and splices the tenant's socket into it. So:
   - **Facet mode:** stock `connect()` leaves as the object's own node.
   - **Plain-DO mode and the Worker:** stock `connect()` leaves as the
     workload node (RFC §4.8).
2. **The tunnel authenticates in band.** After hostd's 200, the platform
   writes one JSON line (`gen.ConnectPreamble`): `v`, the instance's
   loopback `token` (a `LOCAL_TOKEN` text binding, only under the Landlock
   sandbox), and for a facet `class`, `name`, `ts`, `nonce` and `mac`. The
   MAC is the facet `fetch()` MAC with method `CONNECT` and the destination
   in place of the URL, under the per-object key (RT-04), with the same
   skew and replay checks (RT-05). hostd then checks the class is a facet
   class, `ensure`s the object, refuses a killed one, dials, and answers one
   JSON line (`gen.ConnectStatus`: `{ok}` or `{code, message}`) before the
   flow's bytes. Bytes the destination sends first (MySQL, Redis greetings)
   are kept. The workload endpoint refuses a preamble that names an object.
   `localToken` passes CONNECT through and the tunnel's handler checks the
   token from the preamble.
3. **hostd dials with `DialEgress`** as the object's (or workload's) netd
   node, with the Node tier's half-close splice. Every refusal `fetch()` has
   applies: `forbidden_destination` (loopback, link-local, metadata, own
   node, same-host siblings, host addresses), `peer_not_visible`, and on the
   internet path `internet_egress_unavailable` and the SSRF guard.
4. **`ctx.tailnet.connect(address, opts)`** returns a Socket at once, like
   `cloudflare:sockets`. The runtime asks `TailnetApi.tcpConnect` over RPC,
   which dials as above and returns the streams. A refusal rejects `opened`
   with a typed `TailnetError`, which stock `connect()` can't do (spike 3).
   - Facet mode: as the object (props, unforgeable).
   - The Worker: as the workload node, the Worker's own identity.
   - **Plain-DO mode: `not_supported`.** The object has no node to connect
     from until WD-3 (per-actor `globalOutbound`). Silently using the
     workload node would break the method's promise. Stock `connect()`
     there leaves as the workload node.
5. **TLS.** Stock `secureTransport: "on"` and `"starttls"` fail in workerd
   (spike 4); we document that and wait for upstream. On the tailnet,
   WireGuard already encrypts the flow, so drivers run with TLS off
   (`sslmode=disable`). `ctx.tailnet.connect(addr, { tls: "netd" })` is in
   v1: hostd originates TLS to the destination (system roots, SNI = the
   host), for internet databases. `secureTransport` other than `"off"` on
   `ctx.tailnet.connect()` is `not_supported`, pointing at `tls: "netd"`.
   Upstream ask: let a `connect()` handler serve TLS sockets (with the
   option on `opened`), so stock `secureTransport` works.
6. **`tailnet.connects` can name a port:** `"tag:db:5432"`. Preflight
   checks that some grant's `ip` covers that TCP port (`*`, `5432`,
   `tcp:5432`, `tcp:*`, a range), and the missing-grant snippet names it.
   An entry with no port keeps today's check (any `ip` grant) and its
   `tcp:80` snippet. Placement and `tagOwners` use the target without the
   port. This answers #294's open question: per port when the entry names
   one, per tag otherwise.
7. **Observability and audit.** Each flow gets a CLIENT span
   (`connect host:port`: `network.transport`, `server.address`,
   `server.port`, bytes sent and received, `error.type`) and an audit
   record: `egress` when it closes (destination, bytes, duration), `deny`
   with the code when refused. Tailnet TCP flows are audited, which
   `fetch()`'s are not. Internet flows also keep their first-flow `egress`
   record from netd (ADR-049).
8. **Limits and lifecycle.** An egress identity (an object, or the
   workload node) has at most 256 open sockets per instance; the next is
   refused with `socket_limit`. A kill resets the object's flows (netd's
   flow tracking, as for `fetch()` streams). An instance's stop (redeploy,
   teardown) closes its sockets.
9. **Process tier and microVM tier share the endpoints.** No new sockets
   or ports: CONNECT arrives on the existing unix sockets, the Landlock
   loopback listeners, or the microVM gateway's handlers, which all serve
   the same `http.Handler`s (#294's other open question).

**Consequences.**

- `pg`-style drivers work over the tailnet with TLS off. postgres.js runs
  unmodified (its `workerd` build), and `make e2e-pg-socket` runs it against
  Postgres 17 from a Worker and a facet object
  (`hostd:TestTCPEgressPostgres`).
- Stock `connect()` refusals stay opaque ("Network connection lost"). hostd
  audits each one as `deny` with its code (`tw audit`), and
  `ctx.tailnet.connect()` has the typed error.
- A plain-DO object's sockets are attributed to the workload node, and a
  kill of one plain-DO object doesn't reset them (they aren't its flows).
  Facet mode is the way to get per-object TCP identity.
- Every flow is a host-chain audit record. A driver that opens a
  connection per query writes one record per query. Pooling (`max: 1` and
  reuse, or a pool in a Durable Object) keeps that down.

**Not in this decision.** Inbound raw TCP to an object (the RFC's PROXY-v2
ingress); containers' `getTcpPort().connect()` (ADR-032); UDP; a Hyperdrive
binding (`env.HYPERDRIVE`), which a pooling proxy could build on this.
