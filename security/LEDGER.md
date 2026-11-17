# tailworks red-team loop ledger

FIX_MODE: propose
LIVE_TARGETS: none
LAST_ITERATION: (none yet)
LAST_CLAIMS_AUDIT: 2026-11-14 (Pass 3, master 4f23a96)

Seeded 2026-11-14 from `security/RED-TEAM.md` Pass 3. Severity scale: Pass 1.
Roles: see `security/LOOP.md`.

## Hypotheses

| ID | Sev | Role | Hypothesis | Source | Status | Test |
|---|---|---|---|---|---|---|
| H01 | High | Hostile host | A registered host can create/spawn objects in a workload whose lease another host holds | RT3-01 | **fixed** (F1, 2026-11-14) | `mgmt.TestRT3_HostWritesAreLeaseBound` |
| H02 | High | Hostile host | `PATCH /v1/objects/{id} {status:ready}` without `epoch` is unfenced; node IDs and cert are attacker-chosen | RT3-02 | **fixed** (F1) | `mgmt.TestRT3_HostWritesAreLeaseBound` |
| H03 | High | Internet attacker | A `not_public` flood (or tenant `audit()` flood) overflows hostd's pending queue, drops host-source records, and leaves the chain `BrokenAt` forever | RT3-03 | **fixed** (F2) | `attacks.TestRT3_H03_AuditFloodKeepsHostRecordsAndChain`, `netd.TestPublicRoutes`, `hostd.TestAuditSpill*` |
| H04 | Medium | Hostile host | A host can transfer another host's ready `_pool` node to an object it creates | RT3-04 | **fixed** (F1) | `mgmt.TestRT3_HostWritesAreLeaseBound` |
| H05 | Medium | Hostile host | Audit ingest accepts unsigned records from hosts without `AuditPubKey`, naming any tenant's object and any `actingFor` | RT3-05 | **fixed** (F1/F3 part) | `mgmt.TestRT3_HostWritesAreLeaseBound` |
| H06 | Medium | Hostile tenant code | Facet create through the raw-binding bypass is not refused: `isPublic(req)` reads tenant-controlled `request.cf` | RT3-06 | **fixed** (F4) | `hostd.TestPublicIngress` §3b, `gen.TestGeneratedWrapperPublic/HostDORechecksWithHostd` |
| H07 | Medium | Hostile tenant code | `env.__TW` is present in the public env; `call("spawn"\|"ensure"\|"ready")` mints nodes from a public request | RT3-07 | **fixed** (F4) | `hostd.TestPublicIngress` §3b (`/tw` probe), `public.test.js` |
| H08 | Medium | Hostile tenant code | Tenant code forges `request.cf` to a sibling, and sets `actingFor`/`principal` on ensure/spawn; the platform writes it as a `source: host` record | RT3-08 | **partial**: principal/actingFor args closed (F4); in-process `cf` forgery open (F5) | `TestRT3_H08_ForgedPrincipal` (cf part) |
| H09 | Medium | Internet attacker | 256 idle TLS connections from one address close a workload's public ingress; upgraded conns have no lifetime | RT3-09 | **fixed** (F6) | `netd.TestRT3_H09_MaxConnsOneIP` |
| H10 | Medium | Internet attacker / Tailnet insider | Two tenants can deploy the same public name; the later owner intercepts the earlier's webhooks after a kill/redeploy | RT3-10 | **fixed** (F7) | `mgmt.TestRT3_H10_PublicNameSquat` | `TestRT3_H10_PublicNameSquat` |
| H11 | Medium | Infra attacker | A crafted uffd region table (wrapped `Offset`, unaligned `Base`) panics the root vmhelper | RT3-11 | open (arithmetic executed; crash needs Linux) | `TestRT3_H11_UffdRegionValidation` (`//go:build linux`, pure validation unit test is portable: extract `validateRegions`) |
| H12 | Medium | Hostile host | Heartbeat `deployments[]` sets status/error of any deployment by ID | RT3-12 | **fixed** (F1) | `mgmt.TestRT3_HostWritesAreLeaseBound` |
| H13 | Low/Med | Internet attacker | Control chars in a public path / object name / tenant note reach the operator's terminal unescaped via `tw audit` and `tw ls` | RT3-13 | open (probe showed ESC+LF stored) | `TestRT3_H13_TerminalInjection` (render functions in `cmd/tw/ops.go`) |
| H14 | Low | Hostile host | Hosts enumerate every tenant/object incl. credential file paths | RT3-14 | open | `TestRT3_H14_HostEnumeratesTenants` |
| H15 | Low | Tailnet insider | A pending `tw host add` row is claimable by any unregistered `tag:tw-host` node | RT3-15 | open | `TestRT3_H15_PendingHostClaim` |
| H16 | Low | Infra attacker | vmhelper accepts a loaded tenant VM as a snapshot base (only hostd prevents it) | RT3-16 | open | `TestRT3_H16_BaseGuard` (linux) |
| H17 | Low | Hostile tenant code | A `scheduled()` that outlives the 15-min timeout overlaps with the next run | RT3-17 | open | `TestRT3_H17_CronOverlap` |
| H18 | Low | Internet attacker | `Tailscale-Ingress-Src`, `Tailscale-Node-Caps`, `X_TW_CF`, `Via` reach upstream | RT3-18 | **fixed** (F11 strip part, 2026-11-16; `Via` is not an identity header and still passes) | `netd.TestRT3_H18_StripByPrefix` |
| H19 | Low | Hostile tenant code | Admit cache lets a previously admitted plain-DO name re-mint after kill+purge | RT3-19 | open | `TestRT3_H19_AdmitCacheAfterKill` |
| H20 | Low | Tailnet insider | RT2-L1: whitespace-padded `tag:tw ` passes `policy.Check` | RT2-L1 | open (test exists, skipped: `rt06_policy_json_tricks_test.go:51`) | `TestRT2L1_CheckAcceptsWhitespacePaddedTag` |
| H21 | Info | Hostile tenant code | Public-request double-decoding differential: an app that decodes twice sees `/` or `..` | RT3-22 | open (app-side; pin netd's once-decoded gate) | `TestRT3_H21_OnceDecoded` |
| H22 | Medium | Hostile tenant code | **Unexplored:** Tailscale Services ingress (`internal/tssvc` fallback TCP handler) — does WhoIs on a VIP flow ever resolve to the wrong peer, and do `svc:` app caps appear in CapMap (docs/services.md says "not yet verified")? | docs/services.md | open | — |
| H23 | Medium | Hostile host | **Unexplored:** placement poisoning — a host reporting fake path measurements for its own tenants to attract or evict stateful workloads (`--replace` moves, "unreachable host" confirmation) | placement_service.go:71 | open | — |
| H24 | Medium | Infra attacker | **Unexplored:** snapshot-restored guests share KASLR/net_secret/siphash for ≤1 h (ADR-030 residual). Is any in-guest secret derived from them (TCP ISN, port randomisation, hash-flooding protection) exploitable across tenants? | ADR-030 | open (design-level; may be DISCLOSED) | — |
| H-105 | High | Web attacker (admin's browser) | DNS rebinding: a name the attacker controls, rebound to mgmt's tailnet IP, makes the attacker's page same-origin with mgmt's plain-HTTP :80 listener; it reads `/v1` and posts `/ui` forms with the admin's WhoIs identity (B6) | issue #105 | **fixed** (Host allowlist, `mgmt/hostguard.go`) | `mgmt.TestHostGuardRebinding`, `mgmt.TestBrowserGuardOriginAllowlist` |
| H25 | Low | Tailnet insider | **Unexplored:** warm keys are 30-min preauthorized join keys in custody; a custody read yields up to 5 per hot class. Does `tw doctor`/status surface the count, and does `tw down` revoke them? | warmkeys.go:38 | open | — |

## Claims register (pin each with a test or mark overstated)

| ID | Claim (where) | Enforcing code | Pinned by | Status |
|---|---|---|---|---|
| C01 | "hostd authenticates by lease epoch" (THREAT-MODEL §2 B4) | `createObjectTx`, `Ready`, `SetDeploymentStatus`, `IngestAudit` (F1) | `TestRT3_HostWritesAreLeaseBound` | pinned (2026-11-14) |
| C02 | "a raw binding gets create_not_admitted for new names" (docs/public-ingress.md, rewritten) | Host DO `__admitted` + hostd `ensure` (F4) | `TestPublicIngress` §3b | pinned |
| C03 | "a refused flood can't grow the chain" (docs/public-ingress.md Audit) | `netd/public_serve.go` rolls up 429/413/431 and `not_public`/`bad_request` (F2) | `netd.TestPublicRoutes` | pinned |
| C04 | "`actingFor` comes from the platform" (STATUS.md:554, :612) | spawn `principal` is inherit/none only; `actingFor` stripped from tenant args (F4); Host DO `principalOf(req.cf)` still forgeable in-process | — | **partially overstated** (H08, F5) |
| C05 | "request.cf caller comes only from WhoIs" (THREAT-MODEL §5) | netd strips headers; in-process `cf` not sealed | — | **overstated** (H08) |
| C06 | "Client copies of every identity header are stripped: Tailscale-*" (docs/public-ingress.md) | `ingress.go:125` strips five names | — | **resolved** (H18 fixed by F11: netd strips `tailscale-*`, `x_tw_*`, `tw_*` by prefix) |
| C07 | "pool node serves nothing" (STATUS Fast identity) | `http.go:59`; but `paths.go:169` probes from it | — | minor (RT3-22) |
| C08 | `lim` narrows a child: `CapMap ∩ lim` at the receiver (RT-01 fix) | `netd/ingress.go:applyCert` | `rt01_*_test.go` | pinned |
| C09 | Tenant audit is `tenant_note`/`source: tenant` only (RT-02 fix) | `hostd/http.go:171`, `mgmt/service.go:2548` | `rt02_*_test.go` | pinned |
| C10 | Unix socket needs the host token, even healthz (RT-03 fix) | `mgmt/server.go:311` | `rt03_*_test.go` | pinned |
| C11 | Egress MAC replay refused; per-object keys (RT-05 fix) | `hostd/gen/mac.go` | `rt05_replay_test.go` | pinned |
| C12 | `--policy-apply` is additive within `tag:tw*` (RT-06 fix) | `internal/policy` | `hostile_test.go` | pinned (H20 gap) |
| C13 | Public requests never reach `/_tw` or a DO directly | `publicspec.go:280`, `platform.js:128` | `netd/public_test.go` (partial) | pin fully (path-trick table) |
| C14 | A public request cannot make `getCaller()` succeed | `caller.ts:55` | — | pin |
| C15 | Snapshot base holds no tenant secret; guest reseeds CRNG on load | `main_linux.go:320`, `snapshot_linux.go:146` | `snapshot_test.go` (partial) | pin: assert `fsscan` empty + reseed ioctls called before workerd start |
| C16 | Warm VM is single use; pre-claim VM is unreachable | `pool_linux.go:243`, `vnet_linux.go:104` | `pool_test.go` | pinned (verify coverage) |
| C17 | No egress path skips netd's ADR-019 guard (`connect`, cache, redirect, WebSocket, node:net) | `gen.go:317`, `egress.go:66` | — | pin (hermetic workerd sink test, see Pass 3 artifacts) |
| C18 | Service takeover refused unless comment matches exactly | `services.go:113` | `services_test.go` | pinned |
| C19 | mgmt's node is never Funnel-capable; funnel attr only on `tag:tw-public`/`tag:tw-issuer` | `policy.go:219-249` | `policy_test.go` | pinned |
| C20 | Bundle extraction rejects traversal, symlinks, bombs; no capnp injection | `bundle.go:418`, `gen.go:398` | `bundle_test.go`, `gen` tests | pinned (verify NUL/CR cases) |

## Fix queue (used in `FIX_MODE: fix`; ordered)

| ID | Fix | For | Regression test (fail before, pass after) | Status |
|---|---|---|---|---|
| F1 | `CreateObject`/`Ready`/pool transfer: host callers must hold `LeaseName(dep.TenantID, dep.Workload)` (holder always; epoch when sent, hostd always sends); `SetDeploymentStatus` scoped to host; audit ingest requires key + signature + served workload | H01, H02, H04, H05, H12 | `mgmt.TestRT3_HostWritesAreLeaseBound` (fails on 4f23a96, passes after) | **DONE 2026-11-14** |
| F2 | Audit queue: never drop host-source records; in-chain `audit_dropped{n}`; roll `not_public`/`bad_request` into the per-minute summary; token bucket on tenant notes | H03 | `attacks.TestRT3_H03_AuditFloodKeepsHostRecordsAndChain` (with the old head-trimming queue it fails: `prevHash does not link to the head of chain`) | **DONE** (branch audit-chain-flood-issue-68) |
| F3 | Audit ingest: require `AuditPubKey`, verify `hostSig`, scope `objectID` to the host's leases; `SetDeploymentStatus … AND host_id=?` | H05, H12 | folded into F1 | **DONE** (with F1) |
| F4 | Create admission (`docs/create-admission.md`): hostd-issued ticket on tailnet requests and cron runs; guarded env for every handler; `__TW` stripped; Host DO asks hostd, never `req.cf`; `ensure` creates only admitted names; `ready` never creates; `spawn.principal` inherit/none only | H06, H07, H08(b) | `hostd.TestPublicIngress` §3b, `gen.TestGeneratedWrapperPublic`, `gen.TestCreateTicket`, `public.test.js` | **DONE 2026-11-14** (before/after on master not re-run: the base worktree could not build the fixture; the bypass was shown hermetically in Pass 3) |
| F5 | Seal netd's caller blob (HMAC under the instance MAC key) and verify in the runtime; drop `principal`/`actingFor` from tenant-reachable op args | H08 | `TestRT3_H08_*` | queued (design) |
| F6 | Per-IP connection cap on the public listener; shorter idle timeout; bounded upgrade lifetime | H09 | `netd.TestRT3_H09_MaxConnsOneIP` | **DONE** |
| F7 | mgmt uniqueness of `(tailnet, public name)` with first-owner persistence | H10 | `mgmt.TestRT3_H10_PublicNameSquat`, `mgmt.TestRT3_H10_PublicNameLegacyOwner` | **DONE** |
| F8 | `validateRegions` in `uffd_linux.go` + `recover()` per session; `Base` flag in vmhelper sessions | H11, H16 | `TestRT3_H11_*`, `H16_*` | queued |
| F9 | `strconv.Quote`/C0 stripping in every `cmd/tw` renderer; refuse control bytes in `CanonicalPath` and object names | H13 | `TestRT3_H13_*` | queued |
| F10 | Scope host reads to leased tenants; bind pending host rows to the join key's node | H14, H15 | `TestRT3_H14_*`, `H15_*` | queued |
| F11 | Strip by prefix `tailscale-`, `x_tw`, `tw_`; `okTag` boundary + no whitespace | H18, H20 | `TestRT3_H18_*`, `TestRT2L1_*` | strip by prefix **DONE 2026-11-16** (#100); `okTag` part queued |
| F12 | Cron: abort/restart instance on timeout or hold the running mark until the platform answers; evict admit cache on kill | H17, H19 | `TestRT3_H17_*`, `H19_*` | queued |

## Run log

| UTC | Item | Role(s) | Verdict | Commit | Notes |
|---|---|---|---|---|---|
| 2026-11-14 | seed | lead + 4 reviewers | — | — | Pass 3 code review; no tests committed; see RED-TEAM.md |
| 2026-11-14 | F1 | fixer (lead) | DONE | (uncommitted) | lease-bound host writes; `TestRT3_HostWritesAreLeaseBound` 202→409 on master |
| 2026-11-14 | F4 | fixer (lead) | DONE | (uncommitted) | create admission; cron dispatch moved to fetch; all suites green |
