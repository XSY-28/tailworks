# Running twd on Headscale

twd runs on a self-hosted [Headscale](https://github.com/juanfont/headscale)
coordination server as well as on Tailscale. This is the **Headscale
profile** ([ADR-015](design/adr/015-headscale-profile-framework-scope.md),
RFC §7.2). twd needs no changes to Headscale itself.

Needs Headscale **v0.29 or later**: twd's caps travel in policy `grants`,
which v0.28 and earlier reject. twd checks the version up front and refuses
an older server with `profile_unsupported`. Tested against v0.29.7 with
`make e2e-headscale`, which runs a pinned release in Docker. The RFC's
"latest two minor versions" promise applies from v0.30 on.

## What's different from Tailscale

| | Tailscale | Headscale profile |
|---|---|---|
| Minting node keys | OAuth client, WIF trust credential or API key | A Headscale **API key** mints tagged, single-use pre-auth keys through Headscale's REST API (`/api/v1`) |
| Long-lived secret | none with WIF | **yes**: the API key. Keep its file `0600` and rotate it (below) |
| Workload address | Tailscale Service `svc:<name>` (`tailnet.service`) | The workload's own node: `<workload node>.<base_domain>` (MagicDNS). `tailnet.service` is refused with `profile_unsupported` |
| Public routes (Funnel) | `twd.jsonc` `public`, `twd expose` | Not available: Headscale has no Funnel. `publicIngress` is always `deny` |
| Kill switch | Tier 1 (netd), Tier 2 (device delete), Tier 3 (posture) | Tiers 1 and 2. Tier 2 deletes the Headscale node |
| Caps (`app`) | in grants | in grants (the same). `nodeAttrs` carry no `app` |
| HTTPS on mgmt's node | when the tailnet has HTTPS certificates | plain HTTP on `:80` (WireGuard-encrypted, WhoIs-authenticated) |
| Policy owners | `autogroup:admin`, `tag:twd` | `tag:twd` only. Headscale rejects `autogroup:admin` |

Everything else works the same way: Durable Objects with their own nodes,
WhoIs callers (`getCaller`), on-behalf-of assertions, spawn narrowing, the
pre-joined node pool, audit and deploy preflight.

## Set up Headscale

You need a Headscale server with:

- `dns.magic_dns: true` and a `dns.base_domain` (for example
  `hs.example.net`). twd treats the base domain as the tailnet name.
- `policy.mode: database`, if you want `--policy-apply` to write twd's
  policy. With `policy.mode: file`, twd prints the snippet and you add it to
  the file yourself.

Create an API key on the Headscale server and put it in a `0600` file:

```sh
headscale apikeys create --expiration 90d
umask 077; echo 'HEADSCALE_API_KEY=<the key>' > ~/.config/twd/headscale.env
```

## Start twd

```sh
twd up --control=headscale \
  --control-url https://headscale.example.net \
  --tailnet hs.example.net \
  --api-key-file ~/.config/twd/headscale.env \
  --policy-apply --yes
```

`--control-url` is Headscale's `server_url`. Its REST API is served at the
same URL. `twd up` checks the key against the server, then writes the
profile's policy snippet (validated by Headscale's own parser first). After
that it mints mgmt's node key and joins.

**Headscale refuses to register a node whose tags its policy doesn't
define.** So on this profile the policy goes in *before* anything joins.
Without `--policy-apply`, `twd up` stops with `policy_missing_tag_owners`
and prints the snippet:

```hujson
"tagOwners": {
  "tag:twd": [],
  "tag:twd-host": ["tag:twd"],
  "tag:twd-mgmt": ["tag:twd"],
},
"grants": [
  {"src":["tag:twd-host"],"dst":["tag:twd-mgmt"],"ip":["tcp:443","tcp:80"]},
  {"src":["tag:twd-mgmt"],"dst":["tag:twd-host"],"ip":["tcp:7474"]},
],
```

`tag:twd` has no owners. Only the API key mints it, and no user can tag
their own device with it.

An existing twd host can also serve a Headscale tailnet as a second tenant:

```sh
twd tenant add hs.example.net --control=headscale \
  --control-url https://headscale.example.net --api-key-file ~/.config/twd/headscale.env
```

## Deploy

Deploying works as usual. Each tag in `twd.jsonc` must be in Headscale's
`tagOwners` (owned by `tag:twd`); deploy preflight prints any that are
missing. Reach the workload at the MagicDNS name `twd deploy` prints
(`endpoint.magicDNS`). Grants work as on Tailscale, including `app` caps:

```hujson
"tagOwners": { "tag:twd-wl-hello": ["tag:twd"] },
"grants": [
  { "src": ["group:dev"], "dst": ["tag:twd-wl-hello"], "ip": ["tcp:80"] },
],
```

Don't set `tailnet.service` or `public` in `twd.jsonc` on this profile.
Both need Tailscale features (Services, Funnel) that Headscale lacks, and
deploy refuses them with `profile_unsupported`.

## Security notes

- **The API key is a full Headscale admin credential.** Headscale doesn't
  check `tagOwners` when the API mints a key, so the key can mint any tag,
  delete any node and rewrite the policy. twd reads it from the file at
  each use and never copies or logs it. Keep it on the mgmt host only, mode
  `0600`.
- **Rotate the key.** Create a new one, write it to the same file (twd
  re-reads the file, so no restart is needed), then expire the old one:
  `headscale apikeys expire --prefix <old prefix>`. A short `--expiration`
  with a scheduled rotation limits the damage from a leaked key.
- Object keys are single-use and expire after 5 minutes. A killed object's
  node is deleted from Headscale (Tier 2). Lineage lives only in twd's
  audit log (`twd audit`).

## Testing the profile

`make e2e-headscale` starts the pinned Headscale release in Docker (pick
another with `HEADSCALE_IMAGE=headscale/headscale:<tag>`) and runs:

- the policy conformance check: the profile's policy is accepted, and what
  it leaves out (`autogroup:admin`, `funnel`, `svc:`, postures, `app` in
  `nodeAttrs`) really is rejected by that release;
- the node lifecycle: mint, join, single-use, node-side rename (the node
  pool), delete;
- the version gate: v0.28.0 is refused before anything is minted;
- `twd up --control=headscale` end to end, in both process layouts: deploy,
  a call from another node with a verified caller, the `tailnet.service`
  refusal, kill and `twd down`.

To run it against your own server instead, set `TWD_HEADSCALE_URL`,
`TWD_HEADSCALE_API_KEY_FILE` and `TWD_HEADSCALE_BASE_DOMAIN` (it needs
`policy.mode: database`, and it writes the policy).
