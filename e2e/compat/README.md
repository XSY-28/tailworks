# The Cloudflare compatibility suite (`make e2e-compat`)

[COMPAT.md](../../docs/COMPAT.md) says, surface by surface, what of the
Workers platform works on tailworks, with a test for each row. Those tests
are written for tailworks. This suite asks the other question: **does code
written for Cloudflare, by Cloudflare, run?**

It takes 30 example projects from Cloudflare's own repositories, each pinned
to a commit, and deploys them unmodified with `tw deploy` to a real
`tw-hostd` host joined to a real tailnet (tailnet 1, production control,
`tw up --oauth-client-file`). Then a tagged tsnet client node calls each
workload over the tailnet the way the project's README calls it on
Cloudflare: pages, JSON APIs, form posts, WebSockets, MCP sessions, cron and
queue consumer output in `tw logs`.

```sh
make e2e-compat                                    # everything (~15 min warm)
TW_COMPAT_ONLY='^(d1-|chanfana)' make e2e-compat   # some (a regexp on the ID)
make e2e-compat-prepare                            # fetch + install + build only, no tailnet
```

It needs `npm` and the tailnet-1 key file (`make e2e-compat` skips without
it). `TW_COMPAT_PAR` deploys that many projects at once (default 3).
The results go to `e2e/real/results/compat-<time>-<run>.json` (one record
per project: outcome, timings, every request the probe made, deploy
warnings, workarounds). The run's log ends with the matrix as a table.

## What it runs

| ID | Upstream | What | Exercises (COMPAT.md) | Expect |
|---|---|---|---|---|
| `c3-hello-world` | workers-sdk C3 `hello-world/ts` | Worker only (TypeScript) | fetch | pass |
| `c3-hello-world-js` | workers-sdk C3 `hello-world/js` | Worker only (JavaScript) | fetch | pass |
| `c3-worker-assets` | workers-sdk C3 `hello-world-with-assets/ts` | Worker + static assets | assets, fetch | pass |
| `c3-assets-only` | workers-sdk C3 `hello-world-assets-only` | static site, no Worker | assets | pass |
| `c3-durable-object` | workers-sdk C3 `hello-world-durable-object/ts` | Durable Object RPC | do-rpc, do-storage | pass |
| `c3-durable-object-assets` | workers-sdk C3 `hello-world-durable-object-with-assets/ts` | Durable Object + assets | do-rpc, assets | pass |
| `c3-openapi` | workers-sdk C3 `openapi/ts` | Hono + chanfana OpenAPI | fetch | pass |
| `c3-scheduled` | workers-sdk C3 `scheduled/ts` | cron `* * * * *` + internet fetch | cron, fetch | pass |
| `c3-queues` | workers-sdk C3 `queues/ts` | queue producer + consumer | queues, queue-consumers | pass |
| `c3-workflows` | workers-sdk C3 `hello-world-workflows/ts` | Workflow: step.do, waitForEvent | workflows | pass |
| `c3-python` | workers-sdk C3 `hello-world/py` | Python Worker | python | refused |
| `hello-world-do` | templates `hello-world-do-template` | Durable Object + SQLite | do-storage, do-rpc | pass |
| `nodejs-http-server` | templates `nodejs-http-server-template` | `node:http` server via `httpServerHandler` | nodejs-compat | pass |
| `d1-template` | templates `d1-template` | D1 + migrations | d1, d1-migrations | pass |
| `d1-sessions` | templates `d1-starter-sessions-api-template` | D1 sessions, bookmarks, assets | d1, d1-sessions, assets | pass |
| `chanfana-openapi` | templates `chanfana-openapi-template` | REST CRUD on D1 | d1, d1-migrations | pass |
| `openauth` | templates `openauth-template` | OpenAuth issuer: register → code in logs → auth code | kv, d1, d1-migrations, nodejs-compat, logs | pass |
| `r2-explorer` | templates `r2-explorer-template` | R2 Explorer: basic auth from a secret, bucket API, SPA | r2, assets, secrets | pass |
| `durable-chat` | templates `durable-chat-template` | PartyServer chat: WebSocket broadcast, SQLite history | websockets, hibernation, do-storage, assets, wrangler-bundling | pass |
| `multiplayer-globe` | templates `multiplayer-globe-template` | PartyServer WebSockets, `request.cf` geo | websockets, assets, wrangler-bundling | pass |
| `workflows-starter` | templates `workflows-starter-template` | Workflow start → waiting → event → complete, Vite SPA | workflows, do-storage, websockets, assets | pass |
| `to-do-list-kv` | templates `to-do-list-kv-template` | Remix on KV: form post, list | kv, assets, nodejs-compat | pass |
| `vite-react` | templates `vite-react-template` | React SPA + Hono API (Vite plugin) | assets, fetch | pass |
| `react-router-ssr` | templates `react-router-starter-template` | React Router 7 SSR, `vars` | vars, assets, nodejs-compat | pass |
| `astro-blog` | templates `astro-blog-starter-template` | Astro + `@astrojs/cloudflare` | assets, nodejs-compat | pass |
| `microfrontend` | templates `microfrontend-template` | router over service bindings | service-bindings | fails |
| `text-to-image` | templates `text-to-image-template` | Workers AI | workers-ai | fails |
| `remote-mcp-authless` | ai `demos/remote-mcp-authless` | remote MCP server (`agents/mcp`) | mcp, agents-sdk, nodejs-compat | pass |
| `mcp-server` | agents `examples/mcp-server` | stateless MCP (`@modelcontextprotocol/server`) | mcp | pass |
| `mcp-agent` | agents `examples/mcp` | stateful `McpAgent` (state in a Durable Object) | agents-sdk, mcp, do-storage, assets | pass |

"refused": `tw deploy` must refuse it with an error that names the
surface. "fails": it deploys, and its probe must fail (a binding tailworks
doesn't provide). If one of those starts passing, that's an **XPASS** and the
run fails, so that COMPAT.md and `projects.go` get updated together.

The last run's numbers are in [`status/cloudflare-examples-compat-suite.md`](../../status/cloudflare-examples-compat-suite.md)
(or the newest results file).

## What "unmodified" means

The suite does to each project only what a user following its README
does, plus one file:

1. **Fetch** the pinned commit (a blobless partial clone, cached in
   `~/.cache/tailworks-compat`, or `$TW_COMPAT_CACHE`) and copy the
   project directory.
2. For **create-cloudflare templates**, fill `<WORKER_NAME>`,
   `<PACKAGE_NAME>` and `<COMPATIBILITY_DATE>` as `npm create cloudflare`
   does.
3. **`npm ci`** (or `npm install` without a lockfile) with
   `--ignore-scripts`, because the code is fetched, not reviewed. Then the
   README's **build** (`npm run build`), if it has one.
4. Write **`tailworks.jsonc`** with only the workload's name and the run's
   tag, `tag:tw-wl-compat`. Without it each workload would get its own
   `tag:tw-wl-<name>`, which needs one tagOwner per project in the tailnet
   policy. Durable Object classes stay internal (`"node": false`), as they
   are without the file.
5. **`tw secret put`** for the README's `wrangler secret put`, then
   **`tw deploy`**, then the README's post-deploy steps
   (`wrangler d1 migrations apply` → `tw d1 migrations apply`).

Anything beyond that is a **workaround**. It's listed in the project's
`Workarounds` and in the results, and it's a tailworks gap or an upstream
quirk to look at:

- **Vite plugin projects** (`react-router-ssr`, `workflows-starter`,
  `mcp-agent`): the build writes `.wrangler/deploy/config.json`, which
  points at a generated `dist/<worker>/wrangler.json` (`no_bundle`, ESModule
  `rules` over pre-split chunks). `wrangler deploy` follows it. `tw deploy`
  reads the project's own config, and its built-in bundler refuses
  ESModule rules. So the suite runs `tw deploy <generated dir> --wrangler`.
  (`vite-react` doesn't need this: its own config already points at the
  build output.)
- `mcp-agent`, `mcp-server` and `remote-mcp-authless` live in pnpm
  monorepos without lockfiles of their own, so they get `npm install`, with
  `agents` pinned to the published version (plus its peer
  `@modelcontextprotocol/server`, which the monorepo hoists).
- `to-do-list-kv`: `npm ci --legacy-peer-deps`. The template's lockfile
  pins wrangler 4 against `@remix-run/dev`'s optional wrangler 3 peer, and
  npm 11 refuses that (an upstream problem; `npm run deploy` hits it too).

Not a workaround: `durable-chat` and `multiplayer-globe` bundle with
`tw deploy --wrangler`, because their client bundle is wrangler's
`build.command`, which only the wrangler bundler runs (as documented in
[wrangler.md](../../docs/wrangler.md#bundling)).

## The tailnet

The run follows `e2e-public`'s rules. The API key is used only to create and
delete the run's OAuth client, mint the client node's key, apply the policy
additions and clean up by ID. The only policy entries the run may add are:

```jsonc
"tagOwners": { "tag:tw-wl-compat": ["autogroup:admin", "tag:tw"] },
"grants":    [{ "src": ["tag:ttest"], "dst": ["tag:tw-wl-compat"], "ip": ["tcp:80"] }]
```

If anything else is missing, the run refuses to write the policy. Every
node it creates is named `ttest-compat-<run>-*` / `tw-compat-<run>-*`; the
cleanup deletes them by ID and fails the run if any are left.

## Adding a project

Add an entry to `Projects()` in `projects.go`, in the section for its repo:

- `ID` is the workload name.
- Pin `Commit` (the repo's constant).
- List the COMPAT.md anchors it `Exercises`. `Validate` checks that they
  exist.
- Write a `Probe` that does what the README says to try. Use `r.Get`,
  `r.PostJSON`, `r.WS`, `r.MCPConnect` and `r.WaitLog` (in `probe.go`).

`make e2e-compat-prepare TW_COMPAT_ONLY=<id>` checks that it installs and
builds, and `TW_COMPAT_ONLY=<id> make e2e-compat` runs it. A gap is an
entry with an `Expect` (`ExpectRefused` or `ExpectProbeFail`) and the
COMPAT.md row it belongs to. The suite promises at least 20 projects that
are expected to pass, and `Validate` enforces that.
