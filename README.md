# twd

**twd is a self-hosted, tailnet-native runtime for Cloudflare-style Workers and
Durable Objects.** You run it on your own machines (macOS or Linux). It runs
your Workers code on [workerd](https://github.com/cloudflare/workerd). Every
workload, and **every Durable Object instance**, becomes its own node on your
Tailscale tailnet, with its own MagicDNS name, tags and grants:
- callers are identified by WireGuard identity (`getCaller`), not tokens;
- outbound `fetch()` leaves from the object's own node, so your tailnet
  policy governs what each object can reach;
- `twd kill` cuts an object off in under a second.

It runs Cloudflare Agents SDK agents, MCP servers and Vercel AI SDK agents
unmodified (see [docs/COMPAT.md](docs/COMPAT.md)).

## Quick start: deploy your first Durable Object

You need:
- a Mac (Apple silicon) or a Linux machine (arm64 or amd64);
- a Tailscale tailnet where you're an admin.

**1. Install** (no sudo; everything goes into `~/.twd`). Download the latest
[release](https://github.com/rosskukulinski/twd/releases), verify its
checksum and install it. Read [scripts/get.sh](scripts/get.sh) first; it's
short:

```sh
curl -fsSLO https://raw.githubusercontent.com/rosskukulinski/twd/master/scripts/get.sh
sh get.sh                                      # or: sh get.sh v0.1.0
export PATH="$HOME/.twd/bin:$PATH"             # add this to your shell profile too
twd doctor                                     # every check prints a fix if it fails
```

Or build the release yourself (needs Go ≥ 1.27 and Node.js ≥ 20; the
installed twd needs neither):

```sh
git clone https://github.com/rosskukulinski/twd && cd twd
make release RELEASE_TARGETS=$(go env GOOS)-$(go env GOARCH)   # → dist/twd-<version>-<os>-<arch>.tar.gz
mkdir -p /tmp/twd && tar -xzf dist/twd-*-$(go env GOOS)-$(go env GOARCH).tar.gz -C /tmp/twd
/tmp/twd/twd-*/install.sh
```

**2. Let twd mint keys on your tailnet.** In the admin console:
- add this to your **tailnet policy**:
  ```jsonc
  "tagOwners": {
    "tag:twd":      ["autogroup:admin"],
    "tag:twd-mgmt": ["autogroup:admin", "tag:twd"],
    "tag:twd-host": ["autogroup:admin", "tag:twd"],
  },
  "grants": [
    {"src": ["tag:twd-host"], "dst": ["tag:twd-mgmt"], "ip": ["tcp:443", "tcp:80"]},
    {"src": ["tag:twd-mgmt"], "dst": ["tag:twd-host"], "ip": ["tcp:7474"]},
    {"src": ["autogroup:admin"], "dst": ["tag:twd-mgmt"], "ip": ["tcp:443", "tcp:80"], "app": {"twd.dev/cap/admin": [{}]}},
  ],
  ```
- then generate an **OAuth client**: Settings › OAuth clients, with tag
  `tag:twd` and these scopes:
  - **Auth Keys › Write**: required;
  - **Devices › Core › Write**: so kills also delete the device;
  - **Policy File › Read**: so deploys check your grants first;
  - **Services › Write**: so workloads with `tailnet.service` get a
    Tailscale Service ([docs/services.md](docs/services.md)).

  Only the first is required, but pick all four now. Adding a scope later
  means generating a new client.

Save the client where only you can read it:

```sh
mkdir -p ~/.config/twd
printf 'TS_OAUTH_CLIENT_ID=%s\nTS_OAUTH_CLIENT_SECRET=%s\n' '<client id>' '<client secret>' > ~/.config/twd/oauth.env
chmod 600 ~/.config/twd/oauth.env
```

**3. Start twd.** One process: the control plane (`twd-mgmt`) with the host
agent embedded in it, joining your tailnet as a single tagged node
(`twd-mgmt`). No browser login is needed:

```sh
twd up --oauth-client-file ~/.config/twd/oauth.env --service --yes
```

It prints the **admin console**, `http://twd-mgmt.<tailnet>.ts.net/ui/`:
open it from any tailnet admin's browser (WhoIs identity, no login) to see
hosts, deployments, every object's lineage and the audit trail, and to kill
or undeploy. Need more machines later? `twd host add` joins another host;
`twd up --separate-host` keeps mgmt and the host agent as two nodes.

**4. Create and deploy a Durable Object app.** The `agent` template is a
Cloudflare Agents SDK `Agent`. Each instance is a Durable Object with its own
SQLite storage, **and** its own tailnet node.

```sh
twd init support --template agent              # also: hello, mcp, ai-agent, node-workflow
cd support
twd deploy                                     # bundles, uploads, waits until the node is ready
```

`twd init` names the app's tags after it (`tag:twd-wl-support`,
`tag:twd-support-support-agent`, …). `twd deploy` checks them first. If
your policy has no `tagOwners` line for them, it prints the exact snippet
to add; add it and run `twd deploy` again. Every key of `twd.jsonc` is in
[docs/twd-jsonc.md](docs/twd-jsonc.md), and `twd dev` / `twd deploy`
refuse a key they don't know. When it's done, it prints the workload's address,
e.g. `http://support-<host>.<tailnet>.ts.net/`.

**5. Call it** from any device on your tailnet. Each name is a separate
Durable Object instance. The JSON reply shows its verified `caller` (your
tailnet identity) and its persisted `state.visits`:

```sh
curl http://support-<host>.<tailnet>.ts.net/agents/support-agent/alice
curl http://support-<host>.<tailnet>.ts.net/agents/support-agent/alice   # same object: its visit count goes up
twd ls                                         # deployments and objects (each with its own node)
twd audit <object-id>                          # who created, called, spawned or killed it
twd kill <object-id> --cascade                 # cut it (and its sub-agents) off the tailnet
```

## More

- **Install details, other modes and troubleshooting:**
  [docs/INSTALL.md](docs/INSTALL.md).
  - Other ways to mint keys: workload identity federation (`twd up --wif`)
    or an API key.
  - A separate developer laptop: `twd link`.
  - Adding hosts: `twd host add`.
  - The Linux microVM tier (Firecracker).
- **Serve chosen paths to the public internet** (webhooks, OAuth callbacks)
  over Funnel: [docs/public-ingress.md](docs/public-ingress.md).
- **API keys and other secrets** (`env.NAME`, like `wrangler secret`):
  `twd secret put`, and `.dev.vars` for `twd dev`. See
  [docs/secrets.md](docs/secrets.md).
- **Run an app locally with no tailnet at all:** `twd dev`.
- **`twd.jsonc`, every key:** [docs/twd-jsonc.md](docs/twd-jsonc.md)
  (with a JSON Schema for editor completion).
- **An existing Workers project** (`wrangler.toml`, environments, bundling
  with wrangler): [docs/wrangler.md](docs/wrangler.md).
- **What runs and what doesn't:** [docs/COMPAT.md](docs/COMPAT.md)
  (Agents SDK, MCP, Vercel AI SDK) and [docs/node-tier.md](docs/node-tier.md)
  (Node.js apps).
- **Cron triggers** (`triggers.crons` → `scheduled()`):
  [docs/cron-triggers.md](docs/cron-triggers.md).
- **mgmt's API:** [api/openapi.yaml](api/openapi.yaml), rendered in
  [docs/contracts/mgmt-api.md](docs/contracts/mgmt-api.md).
- **The admin console:** [docs/ADMIN-UI.md](docs/ADMIN-UI.md).
- **Every typed error code and its fix:** [docs/errors.md](docs/errors.md).
- **Security:** [security/THREAT-MODEL.md](security/THREAT-MODEL.md) and
  [security/RED-TEAM.md](security/RED-TEAM.md) (the attacker's view,
  findings and fixes).
- **Running several mgmt replicas on Postgres:** the chaos and soak harness
  and its findings are in [e2e/multimgmt/](e2e/multimgmt/)
  (`LEDGER.md`). Replicas can share one Tailscale Service VIP
  (`-tsnet-service svc:twd-mgmt`).
- **Stable names for workloads (Tailscale Services):** `"service": true` in
  `twd.jsonc`; stateless workloads can also run on several hosts behind it
  (`"replicas": 3`). See [docs/services.md](docs/services.md).
- **Build status and measured results:** [status/](status/) (one file per workstream) and the frozen history in [STATUS.md](STATUS.md).
- **Design:** the RFC is [docs/design/RFC.md](docs/design/RFC.md) and the
  ADRs are in [docs/design/adr/](docs/design/adr/), one file each.
