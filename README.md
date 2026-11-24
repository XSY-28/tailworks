# tailworks

**Run Cloudflare-style Workers and Durable Objects on your own machines, on
your Tailscale tailnet.** Every workload, and every Durable Object, is its own
tailnet node with its own name, tags and grants. Callers are identified by
their tailnet identity, not tokens; your tailnet policy decides who can call
what, and what each object can reach.

It runs your code on [workerd](https://github.com/cloudflare/workerd) on
macOS (Apple silicon) or Linux. Agents SDK agents, MCP servers and Vercel AI
SDK agents run unmodified.

tailworks is a community project, not affiliated with or endorsed by
Tailscale Inc.

## Quick start

You need macOS 14+ on Apple silicon or Linux (arm64/amd64, glibc ≥ 2.35),
and admin rights on a Tailscale tailnet. No Go or Node.js.
[Prerequisites](docs/INSTALL.md#prerequisites) has the details; Headscale works too
([docs/headscale.md](docs/headscale.md)).

### 1. Install

```sh
curl -fsSL https://raw.githubusercontent.com/rosskukulinski/tailworks/master/scripts/get.sh | sh
export PATH="$HOME/.tailworks/bin:$PATH"    # also add this to your shell profile
tw doctor
```

### 2. Add tailworks to your tailnet policy

In the admin console, open **Access controls** and add:

```jsonc
"tagOwners": {
  "tag:tw":      ["autogroup:admin"],
  "tag:tw-mgmt": ["autogroup:admin", "tag:tw"],
  "tag:tw-host": ["autogroup:admin", "tag:tw"],
},
"grants": [
  {"src": ["tag:tw-host"], "dst": ["tag:tw-mgmt"], "ip": ["tcp:443", "tcp:80"]},
  {"src": ["tag:tw-mgmt"], "dst": ["tag:tw-host"], "ip": ["tcp:7474"]},
  {"src": ["autogroup:admin"], "dst": ["tag:tw-mgmt"], "ip": ["tcp:443", "tcp:80"], "app": {"github.com/rosskukulinski/tailworks/cap/admin": [{}]}},
],
```

### 3. Create an OAuth client

In the admin console, go to **Settings › Trust credentials**, click
**Credential**, and choose **OAuth**. Select these scopes. When a scope asks
for a tag, choose **`tag:tw`**.

| Scope | Why |
|---|---|
| **Auth Keys › Write** | Required. tailworks mints a key for every node it runs. |
| **Devices › Core › Write** | `tw kill` also removes the device. |
| **Policy File › Read** | `tw deploy` checks your policy first. |
| **Services › Write** | For apps that use a Tailscale Service. |

Generate it and keep the page open: you'll paste the **Client Secret** in the
next step (it is shown only once).

### 4. Start tailworks

```sh
tw up
```

Paste the Client Secret when asked. `tw up` saves it to
`~/.config/tailworks/oauth.env` (readable by you only), joins your tailnet
as `tw-mgmt`, and installs a service so tailworks starts at login. It
prints the admin console URL: `http://tw-mgmt.<tailnet>.ts.net/ui/`.

### 5. Deploy an app

```sh
tw init hello && cd hello
tw policy        # prints the lines this app needs in your tailnet policy
```

Paste what `tw policy` prints into **Access controls**. It includes a grant
that lets everyone on your tailnet call the app; narrow it if you like. Then:

```sh
tw deploy
```

### 6. Call it

`tw deploy` prints the app's URL. From any device on your tailnet:

```sh
curl http://tw-hello-<id>.<tailnet>.ts.net/    # {"hello":"<you>","caller":{…}}
tw logs hello -f                               # one line per request
```

Stuck? See [docs/troubleshooting.md](docs/troubleshooting.md).

### Try it: a static site with a Worker

wrangler `assets` work: a folder of files (a plain site, or a Vite or React
build) served next to a Worker. The `static-site` template greets you by
your tailnet login, has an API, and a folder only people (not tagged
nodes) may read. It runs with no tailnet:

```sh
tw init my-site --template static-site && cd my-site
tw dev                                     # open http://127.0.0.1:8787/ ; edit public/index.html and reload
curl -i http://127.0.0.1:8787/style.css    # a file: Content-Type and ETag
curl http://127.0.0.1:8787/api/whoami      # the Worker: your verified caller
```

How the routing works: [docs/wrangler.md#assets](docs/wrangler.md#assets).

### Next

- Try it locally first, with no tailnet: `tw dev` serves an app on
  `127.0.0.1:8787`.
- Other templates: `tw init <dir> --template agent` (also `mcp`, `ai-agent`,
  `lock`, `static-site`, `refund-approval`, `node-workflow`). Each Durable Object instance
  gets its own tailnet node.
- An existing Workers project: [docs/wrangler.md](docs/wrangler.md).
- More machines: `tw host add`. A separate laptop: `tw link`.
- Let tailworks write policy lines for you: give the OAuth client
  **Policy File › Write**, then `tw deploy --apply-policy`.

## Using tailworks with a coding agent

```sh
tw agent setup    # installs skills for Claude Code, Codex, Cursor, Gemini CLI, OpenCode
```

Then ask your agent to "build me an MCP server and run it on tw". Every
command takes `--json`. Details: [docs/agents.md](docs/agents.md).

## Compatibility at a glance

What of Cloudflare Workers works on tailworks, surface by surface. Details
and the tests behind each row: [docs/COMPAT.md](docs/COMPAT.md).

<details>
<summary>✅ works · ⚠️ partial or different · ❌ not yet · 🚫 not by design</summary>

| Area | Surface | Status | Note |
|---|---|---|---|
| Runtime | [`fetch` handler, outbound `fetch()`](docs/COMPAT.md#fetch) | ⚠️ | outbound reaches tailnet peers from the workload's (or object's) own node, and public internet hosts from the host's network (on by default; `tailnet.egress.internet: false` turns it off, [call an internet API](docs/recipes.md#call-an-internet-api)) |
| Runtime | [WebSockets](docs/COMPAT.md#websockets) | ✅ | |
| Runtime | [TCP sockets (`connect()`)](docs/COMPAT.md#connect) | ❌ | egress is HTTP only |
| Runtime | [WebAssembly](docs/COMPAT.md#wasm) | ✅ | |
| Runtime | [`nodejs_compat`](docs/COMPAT.md#nodejs-compat) | ✅ | |
| Runtime | [Python Workers](docs/COMPAT.md#python) | ❌ | Python runs in containers |
| Runtime | [Node.js apps (`runtime: "node"`)](docs/COMPAT.md#node-runtime) | ✅ | tw-only |
| Durable Objects | [One tailnet node per object](docs/COMPAT.md#do-identity) | ✅ | tw-only: own name, tags, grants, egress |
| Durable Objects | [Storage (KV API, SQLite)](docs/COMPAT.md#do-storage) | ✅ | one host, no replication |
| Durable Objects | [Alarms](docs/COMPAT.md#alarms) | ✅ | |
| Durable Objects | [Hibernation](docs/COMPAT.md#hibernation) | ✅ | |
| Durable Objects | [Locks / leader election (pattern)](docs/COMPAT.md#locks) | ⚠️ | manual failover; [docs/locks.md](docs/locks.md) |
| Durable Objects | [RPC](docs/COMPAT.md#do-rpc) | ⚠️ | no cross-Worker `script_name` |
| Durable Objects | [Facets, `subAgent()`](docs/COMPAT.md#facets) | ❌ | use `ctx.tailnet.spawn` |
| Bindings | [KV](docs/COMPAT.md#kv) | ⚠️ | per workload; values ≤ 2 MiB |
| Bindings | [R2](docs/COMPAT.md#r2) | ⚠️ | bytes on the host's disk or an S3-compatible service |
| Bindings | [R2 multipart upload](docs/COMPAT.md#r2-multipart) | ⚠️ | unfinished uploads are kept |
| Bindings | [D1](docs/COMPAT.md#d1) | ⚠️ | one workload's database; survives host moves |
| Bindings | [D1 sessions](docs/COMPAT.md#d1-sessions) | ⚠️ | bookmarks are journal positions |
| Bindings | [D1 migrations](docs/COMPAT.md#d1-migrations) | ✅ | wrangler's layout |
| Bindings | [`tw d1` commands](docs/COMPAT.md#d1-cli) | ⚠️ | reach the running workload |
| Bindings | [Queues](docs/COMPAT.md#queues) | ✅ | queues live in mgmt; producer and consumer on any hosts |
| Bindings | [Workflows](docs/COMPAT.md#workflows) | ✅ | workerd's built-in engine; approval events are tailworks' |
| Bindings | [Workers AI](docs/COMPAT.md#workers-ai) | ❌ | use an LLM gateway on your tailnet |
| Bindings | [Vectorize](docs/COMPAT.md#vectorize) | ❌ | |
| Bindings | [Hyperdrive](docs/COMPAT.md#hyperdrive) | ❌ | |
| Bindings | [Browser Rendering](docs/COMPAT.md#browser) | ❌ | |
| Bindings | [Email](docs/COMPAT.md#email) | ❌ | |
| Bindings | [Service bindings](docs/COMPAT.md#service-bindings) | ❌ | call the workload over the tailnet |
| Bindings | [Secrets](docs/COMPAT.md#secrets) | ⚠️ | `tw secret`; applied on the next deploy |
| Bindings | [Vars](docs/COMPAT.md#vars) | ✅ | |
| Bindings | [Static assets](docs/COMPAT.md#assets) | ⚠️ | `_redirects`, `_headers` and assets-only work; within the 64 MiB bundle |
| Triggers | [Cron](docs/COMPAT.md#cron) | ✅ | |
| Triggers | [Queue consumers](docs/COMPAT.md#queue-consumers) | ⚠️ | competing consumers allowed; fixed concurrency |
| Triggers | [Queue pull consumers](docs/COMPAT.md#queue-pull) | ⚠️ | tailworks' API, admin only |
| Platform | [Caller identity and auth](docs/COMPAT.md#identity) | 🚫 | `getCaller` from WireGuard, not tokens or Access JWTs |
| Platform | [Workflow approvals by tailnet identity](docs/COMPAT.md#approvals) | ✅ | tw-only: `tw approve`, decided under `github.com/rosskukulinski/tailworks/cap/approve` grants |
| Platform | [Placement](docs/COMPAT.md#placement) | ⚠️ | at deploy, by tailnet path; never moves on its own |
| Platform | [Public routes](docs/COMPAT.md#public-routes) | ⚠️ | opt-in paths over Funnel; tailnet-only by default |
| Platform | [Containers, Sandbox SDK](docs/COMPAT.md#containers) | ⚠️ | one microVM per container |
| Platform | [Logs and tail](docs/COMPAT.md#logs) | ⚠️ | `tw logs`: a structured line per request; no Tail Workers |
| Platform | [Metrics](docs/COMPAT.md#metrics) | ⚠️ | Prometheus `/metrics` on the tailnet, and OTLP; no CPU time |
| Platform | [Tracing (`traceparent`)](docs/COMPAT.md#traces) | ⚠️ | across DO calls and egress; not DO RPC |
| Platform | [Export to OpenTelemetry](docs/COMPAT.md#otlp-export) | ⚠️ | `tw telemetry set`; collector on the tailnet |
| Platform | [Wrangler config (`wrangler.toml`/`.json`/`.jsonc`)](docs/COMPAT.md#wrangler-config) | ⚠️ | keys tailworks can't use are warned, one line each |
| Platform | [Environments (`--env`)](docs/COMPAT.md#wrangler-env) | ✅ | one workload per environment |
| Platform | [Bundling (`define`, `alias`, `rules`, `--wrangler`)](docs/COMPAT.md#wrangler-bundling) | ✅ | built-in esbuild, or wrangler's own |
| Platform | [`wrangler deploy` / `secret` / `tail`](docs/COMPAT.md#wrangler-api) | 🚫 | use `tw deploy` / `secret` / `logs` |
| SDKs | [Cloudflare Agents SDK](docs/COMPAT.md#agents-sdk) | ⚠️ | core and `AgentWorkflow` run unmodified; no `subAgent()` |
| SDKs | [MCP servers](docs/COMPAT.md#mcp) | ✅ | tools gated by tailnet grants |
| SDKs | [Vercel AI SDK](docs/COMPAT.md#vercel-ai) | ⚠️ | MCP client needs `mcpFetch` |
| Tooling | [CLI `--json` output](docs/COMPAT.md#cli-json) | ✅ | schema for every command; NDJSON events |
| Tooling | [Coding-agent skills (`tw agent`)](docs/COMPAT.md#agent-skills) | ✅ | Claude Code, Codex, Cursor, Gemini CLI, OpenCode |
| Tooling | [Testing: `@cloudflare/vitest-plugin`](docs/COMPAT.md#vitest-plugin) | ⚠️ | Worker and DO logic as on Cloudflare; callers and `ctx.tailnet` via `@tailworks/tailnet/testing` |
| Tooling | [tailworks' MCP server (`/mcp`, `tw mcp`)](docs/COMPAT.md#tw-mcp) | ✅ | tools gated per tool by a tailnet grant |
| Tooling | [Docs for agents (`llms.txt`, `tw docs`)](docs/COMPAT.md#docs-for-agents) | ✅ | offline search |

</details>

## Privacy

No telemetry. The CLI checks GitHub for a new release at most once a day
(`TW_NO_UPDATE_CHECK=1` turns it off).
[Details](docs/security-model.md#telemetry-and-the-update-check).

## Docs

- [All docs, by role and task](docs/README.md) ·
  [CLI reference](docs/cli.md) · [Concepts](docs/concepts.md) ·
  [Install, upgrade, back up](docs/INSTALL.md) ·
  [Troubleshooting](docs/troubleshooting.md) · [Errors](docs/errors.md)
- [`tailworks.jsonc`](docs/tailworks-jsonc.md) · [Secrets](docs/secrets.md) ·
  [Queues](docs/queues.md) · [Cron](docs/cron-triggers.md) ·
  [Locks](docs/locks.md) · [Approvals](docs/approvals.md) ·
  [Services](docs/services.md) · [Public routes](docs/public-ingress.md) ·
  [Observability](docs/observability.md)
- [Security model](security/THREAT-MODEL.md) · [Design](docs/design/RFC.md) ·
  [Changelog](CHANGELOG.md) · [Contributing](CONTRIBUTING.md) ·
  [Security reports](SECURITY.md)

Build from source: Go ≥ 1.27 and Node.js ≥ 20, then
`make release RELEASE_TARGETS=$(go env GOOS)-$(go env GOARCH)`
([docs/INSTALL.md](docs/INSTALL.md)).
