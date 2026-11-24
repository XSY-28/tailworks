# Using wrangler with tailworks

An existing Workers project runs on tailworks without changes. `tw deploy` and
`tw dev` read wrangler's own config file, bundle the project the way
wrangler does (with tailworks' built-in esbuild or with wrangler itself), and say
which settings they ignore.

```sh
cd my-worker            # wrangler.toml, src/index.ts, no tw files
tw dev                 # local run: tw-hostd + workerd, no tailnet
tw deploy              # to your tw stack
tw deploy --env staging
```

What tailworks does not use is `wrangler deploy` itself, or the other wrangler
commands that call Cloudflare's API. See [below](#wrangler-commands-that-call-cloudflare) for why.

## What's different from Cloudflare, in short

- **No Cloudflare account, no edge.** Your code runs on your own machines.
  A workload's address is a MagicDNS name on your tailnet, not
  `*.workers.dev` or a route on your zone.
- **Private by default.** Only devices on your tailnet can call a workload,
  and only if your tailnet policy lets them. Public paths are opt-in, over
  Funnel ([public-ingress.md](public-ingress.md)).
- **Callers are tailnet identities.** `getCaller` gives the caller's login
  or tags from WireGuard. There are no API tokens or Access JWTs to check.
- **Outbound `fetch()` reaches tailnet peers and the internet.** Tailnet
  calls leave from the workload's (or object's) own node. Internet calls
  work as on Cloudflare, but leave from the host's network, so the API
  sees the host's address. Private and metadata addresses are refused.
  `tailnet.egress.internet: false` in tailworks.jsonc turns internet
  egress off, and a list limits it
  ([recipes.md](recipes.md#call-an-internet-api)).
- **Each Durable Object is a tailnet node** with its own name, tags and
  grants. Its storage lives on one host's disk, with no replication.
- **Same CLI shapes, different binary.** `tw dev`, `tw deploy`,
  `tw secret`, `tw kv`, `tw d1`, `tw queues`, `tw r2` and `tw logs`
  stand in for their wrangler commands. Secrets apply on the next deploy.
- **Not there:** Workers AI, Vectorize, Hyperdrive, Browser Rendering,
  Email, service bindings, TCP `connect()`. Static assets work, with
  `_redirects` and `_headers` ([below](#assets)), and so do Cloudflare
  Pages projects with `functions/` ([below](#pages)). The full list,
  with tests: [COMPAT.md](COMPAT.md).

## Config files

tailworks reads the same files as wrangler 4, in the same order. The first one
found in the app directory wins:

1. `wrangler.json`
2. `wrangler.jsonc`
3. `wrangler.toml`

If there are others, `tw deploy` prints one line for each, saying it's
ignored. Unlike wrangler, tailworks only looks in the app directory, not its
parents, because `tailworks.jsonc` sits there too.

Whatever the format, the bundle carries the resolved config as JSON
(`wrangler.jsonc`), so the host never sees TOML or environments.

## Environments (`--env`)

`tw deploy --env staging` and `tw dev --env staging` use `[env.staging]`
the way `wrangler --env staging` does:

- These keys are **inherited** from the top level unless the environment
  sets them: `main`, `compatibility_date`, `compatibility_flags`,
  `migrations`, `triggers`, `placement`, the build settings, and the rest.
- These keys are **not inherited**: `vars`, `define`, `durable_objects`,
  `containers` and every other binding. An environment without its own copy
  has none. Tailworks prints a line for each one, as wrangler does:

  ```
  › wrangler.toml: env.staging: define is set at the top level but not in env.staging; environments don't inherit it (as in wrangler), so env.staging has none
  ```

- The **workload name** is `<name>-<env>` (for example `shop-staging`),
  unless the environment sets its own `name`. `<name>` is `tailworks.jsonc`
  `name` if there is one, then wrangler's `name`, then the directory name.
  `--name` overrides all of these.

So `staging` and production are two workloads with two tailnet nodes. If
`tailworks.jsonc` exists, it is shared by every environment. It has no `env`
section, so both get the same tags and grants.

Secrets belong to a workload, so give an environment's secrets its name:
`tw secret put API_KEY --workload shop-staging`. Run in the app
directory without `--workload`, `tw secret` uses the top-level name.

An unknown environment is an error that lists the ones that exist.

## Without a tailworks.jsonc

A project that has only a wrangler config gets these defaults:

```jsonc
{
  "name": "<workload>",
  "tailnet": {
    "tags": ["tag:tw-wl-<workload>"],
    // every durable_objects class:
    "objects": { "<Class>": { "node": false } }
  }
}
```

The workload node is tagged `tag:tw-wl-<workload>`. As with any new tag,
`tw policy` prints the `tagOwners` line to add (and the first `tw deploy`
stops at preflight until it's there).

Durable Object classes are **internal**: you reach them through their
binding, as on Cloudflare, and they have no tailnet node of their own (see
[egress-modes.md](egress-modes.md)).

To give the workload `connects`, a Service or public routes, or to give a
class its own node and tags, add a `tailworks.jsonc` ([tailworks-jsonc.md](tailworks-jsonc.md)).
`tw deploy` says when it is using the defaults.

## Bundling

There are three ways to bundle. All three produce the same bundle shape:
the entry module plus additional modules named `<sha1>-<file>`, exactly as
wrangler names them.

| | built-in esbuild (default) | `--wrangler` | `--from-wrangler-out <dir>` |
|---|---|---|---|
| what runs | esbuild 0.28.2, linked into `tw` | `wrangler deploy --dry-run --outdir <tmp> --config <file> --env=<env>` in the app directory. Nothing is sent to Cloudflare | nothing: tailworks packs a directory you built with `wrangler deploy --dry-run --outdir <dir>` |
| needs Node.js | no | yes, and wrangler (`node_modules/.bin/wrangler`, then `PATH`; `$TW_WRANGLER` overrides) | no (at deploy time) |
| use it for | most projects | custom builds, or to get exactly wrangler's output | CI that already runs wrangler, or a pinned wrangler version |

For `--from-wrangler-out`, tailworks finds the entry by `main`'s name
(`src/index.ts` becomes `index.js`). It leaves out `README.md` and source
maps. Each other module gets its type from your `rules` and wrangler's
default rules, matched on its file name, and otherwise from its extension.
Tailworks doesn't check that the directory is up to date with your sources.

### What each bundler honours

Measured with `e2e/wrangler/testdata/toml-app` and wrangler 4.60.0
(`make e2e-wrangler`, `TestBuildWranglerTOMLApp`):

| wrangler setting | built-in esbuild | wrangler | notes |
|---|---|---|---|
| `main` (TS or JS) | ✅ | ✅ | |
| `define` | ✅ | ✅ | |
| `alias` | ✅ | ✅ | |
| default `rules`: `.txt` / `.html` / `.sql` as Text, `.bin` as Data, `.wasm` / `.wasm?module` as CompiledWasm | ✅ | ✅ | same `<sha1>-<file>` module names |
| custom `rules` of type Text, Data or CompiledWasm (with `fallthrough`) | ✅ | ✅ | e.g. `**/*.md` as Text. The host types each module from `tw-bundle.json` `modules` |
| `rules` of type ESModule / CommonJS, `no_bundle`, `find_additional_modules`, `base_dir`, `preserve_file_names` | ❌ warned | ✅ ES modules | tailworks ships ES modules only; a `.cjs` module in wrangler's output is an error |
| `build` (custom build command) | ❌ warned | ✅ | |
| `minify`, `keep_names`, `jsx_factory`, `jsx_fragment`, `tsconfig` | ✅ mapped to esbuild options | ✅ | mapped, but not measured end to end |
| `nodejs_compat` | ✅ bare built-ins become `node:*`; workerd provides them | ✅ plus wrangler's unenv polyfills | toml-app's entry: 1.0 KB built-in, 21 KB wrangler. Both pass the same checks (`node:buffer`) |
| `node_compat` (legacy) | ❌ warned | ❌ wrangler 4 rejects it | use `compatibility_flags = ["nodejs_compat"]` |
| `upload_source_maps` | ✅ on unless `false` | ❌ warned | the built-in esbuild ships `dist/index.js.map` (no sources); hosts map exception stacks to `src/…` with it ([dev-debugging.md](dev-debugging.md)) |
| `assets` (static assets) | ✅ | ✅ | read by `tw` itself, whichever bundler builds the Worker ([Static assets](#assets)) |
| `site` (Workers Sites) | ❌ warned | ❌ | deprecated by Cloudflare; use `assets` |

`create-tailworks`'s `build.mjs` (the npm bundler) still reads only
`wrangler.jsonc` and has none of these settings. `tw deploy` does not
use it.

## Settings tailworks ignores

`tw deploy` and `tw dev` print one line per top-level key they don't use,
before anything else. Nothing is ignored silently:

```
› wrangler.toml: vectorize: no such binding on tw: env.CACHE will be undefined
› wrangler.toml: routes: ignored: there is no Cloudflare zone or workers.dev; the workload answers at its MagicDNS name (tailworks.jsonc "public" for the internet)
› wrangler.toml: workers_dev: ignored: there is no Cloudflare zone or workers.dev; …
› no tailworks.jsonc: using defaults (workload node tag:tw-wl-wtoml; Durable Object classes internal, no node of their own); add a tailworks.jsonc to change them
```

`tw deploy --json` returns the same lines in `warnings`.

| keys | what tailworks says |
|---|---|
| `routes`, `route`, `workers_dev`, `preview_urls`, `account_id`, `zone_id` | ignored: the workload is reached at its MagicDNS name. Public routes are `tailworks.jsonc` `public` ([public-ingress.md](public-ingress.md)) |
| `services`, `ai`, `browser`, `vectorize`, `hyperdrive`, `analytics_engine_datasets`, `send_email`, `dispatch_namespaces`, `mtls_certificates`, `images`, `pipelines`, `ratelimits`, `secrets_store_secrets`, `version_metadata`, `worker_loaders`, `vpc_services`, `media`, `tail_consumers`, `logfwdr`, `unsafe` | no such binding on tailworks; the warning names each `env.X` that will be undefined. A section tailworks supports as a [platform store](design/adr/044-platform-stores.md) (`kv_namespaces`, [Workers KV](kv.md); `r2_buckets`, [R2](r2.md); `d1_databases`, [D1](d1.md)) is not warned about |
| `site` | not supported (Workers Sites; use `assets`) |
| `observability`, `logpush`, `limits`, `usage_model`, `keep_vars`, `send_metrics`, `dev`, … | ignored by tailworks |
| a `durable_objects` binding with `script_name` | not supported (the deploy fails) |
| anything else | unknown key, ignored |

These keys are used without a warning: `name`, `main`, `compatibility_date`,
`compatibility_flags`, `vars`, `durable_objects`, `kv_namespaces` ([kv.md](kv.md)), `r2_buckets` ([r2.md](r2.md)), `migrations`, `triggers`
([cron-triggers.md](cron-triggers.md)), `containers`
([containers.md](containers.md)), `placement`, `assets` ([below](#assets)),
`pages_build_output_dir` ([below](#pages)), `secrets` (its `required` names, for `tw types`), and the build settings
above.

<a id="assets"></a>

## Static assets (`assets`)

A folder of files served next to the Worker, as on Cloudflare: a plain site,
or the build output of a Vite, React or docs project. The `static-site`
template is a working example (`tw init my-site --template static-site`).

```jsonc
"assets": {
  "directory": "./public",                 // required: the folder to serve
  "binding": "ASSETS",                     // optional: env.ASSETS.fetch(req) in the Worker
  "html_handling": "auto-trailing-slash",  // the default; or force-trailing-slash, drop-trailing-slash, none
  "not_found_handling": "404-page",        // or single-page-application, none (the default)
  "run_worker_first": ["/api/*"]           // or true: these paths run the Worker before any file
}
```

How a request is answered:

1. A path matching `run_worker_first` (`true` is every path; `*` matches
   any characters, `/` too; a `!` pattern excludes) goes to the Worker.
   It can answer, or check `getCaller(req)` and then serve a file with
   `env.ASSETS.fetch(req)`.
2. A `_redirects` rule that matches the path (first match wins) answers
   with its redirect, or, for status `200`, with its destination's file
   at the requested URL.
3. A `GET` or `HEAD` for a file is answered with the file: `Content-Type`
   from its extension, a strong `ETag`, `Cache-Control: public, max-age=0,
   must-revalidate`, and `304 Not Modified` for a matching `If-None-Match`.
   `html_handling` maps `/about` to `about.html`, `/docs/` to
   `docs/index.html`, and redirects (307) the other spellings, the way
   Cloudflare does.
4. Anything else goes to the Worker. In `single-page-application` mode a
   browser navigation (`Sec-Fetch-Mode: navigate`) that matches no file
   gets `index.html` instead.

`env.ASSETS.fetch(req)` applies `not_found_handling` to a path with no file:
`404-page` serves the nearest `404.html` (status 404),
`single-page-application` serves `index.html`, `none` answers an empty 404.

What gets uploaded: every file under `directory`, except the patterns in
its `.assetsignore` (gitignore-style: `*.map`, `private/`, `/top.txt`;
no `!` or `**`), `.git`, `node_modules` and `.DS_Store`.

### `_redirects` and `_headers`

Put them at the top of `directory`. They are read by the host, never
served as files, and `tw deploy` / `tw dev` reject a bad line (with its
line number) before uploading.

```
# _redirects: <from> <to> [status]
/old             /new                     # 302 by default
/moved           /new             301     # 301, 302, 303, 307 or 308
/blog/*          /posts/:splat    308     # * is a splat (any characters, / too)
/u/:id/profile   /people/:id              # :name is one path segment
/docs/*          https://docs.example.com/:splat
/app/*           /index.html      200     # a rewrite: that file, at this URL
```

```
# _headers: a path pattern, then indented headers
/*
  X-Frame-Options: DENY
/assets/*
  Cache-Control: public, max-age=31536000, immutable
  ! X-Frame-Options
```

- Rules match the URL path as sent (percent-encoded). `_redirects` keeps
  the request's query string unless the destination has its own. A `200`
  rewrite only names a path on the site; a destination with no file is a
  miss.
- `_headers` apply to file responses (including 304s and the 404 page),
  not to redirects or the Worker's own responses. A rule's header replaces
  tailworks' default (`Cache-Control`, `Content-Type`); the same header
  from several matching rules is joined with `, `; `! Name` removes it.
  `:splat` and `:name` fill in values too.
- Limits, as on Cloudflare: 2,000 static and 100 dynamic redirects (1,000
  characters a line); 100 header rules (2,000 characters a line). Only
  paths: domain-level rules (`https://host/…` as the pattern) are refused.

### Assets-only projects

A config with `assets` and no `main` is a site with no Worker:

```jsonc
{ "name": "docs", "assets": { "directory": "./dist", "not_found_handling": "single-page-application" } }
```

Every `GET` and `HEAD` is answered from the files, and a miss gets
`not_found_handling` (a single-page app gets `index.html` for any path,
navigation or not). Other methods get `405`. `run_worker_first` needs a
Worker, so it is an error here. `tw` bundles no code; the host supplies
the stand-in Worker.

`tw dev` serves the same way and reloads when a file in `directory`
changes, even when it is `dist/`. Build your client (for example
`npm run build`) before `tw deploy`; tailworks doesn't run the build.

Differences from Cloudflare:

- The files ride in the deploy bundle, so they count against its 64 MiB
  (about 48 MiB in one deploy request, which carries the bundle as JSON).
  Larger sites need a separate upload path, which doesn't exist yet.
- Who can read: anyone who can reach the workload, over the tailnet or a
  [public](public-ingress.md) path. Put files that need an identity check
  under a `run_worker_first` path.
- `env.ASSETS` is in the Worker and in plain-mode Durable Objects
  (`"egress": "workload"`), not in facet-mode objects.
- The host must run a tailworks release with the `assets` feature; mgmt
  doesn't place an assets deployment on an older host.

<a id="pages"></a>
## Cloudflare Pages projects (`functions/`, `_worker.js`)

An existing Pages site deploys as it is: a folder of built files plus
`functions/`, or an advanced-mode `_worker.js`. `tw` compiles the
Functions into the Worker itself (no wrangler needed), so Pages becomes a
Worker with [static assets](#assets). For a new project, use a Worker
with `assets` directly, as Cloudflare also recommends.

```jsonc
// wrangler.jsonc: Pages sites often have none; this is all it needs
{
  "name": "arcade",
  "compatibility_date": "2026-04-01",
  "pages_build_output_dir": "./public",   // the folder your build writes
  "d1_databases": [{ "binding": "DB", "database_name": "arcade", "database_id": "…" }]
}
```

```sh
npm run build    # if the site has a build step: tailworks doesn't run it
tw dev           # reloads on edits under functions/ and public/
tw deploy
```

A project is treated as Pages when the config has
`pages_build_output_dir`, or when it has no `main` and has `functions/` or
`<assets.directory>/_worker.js`. `tw deploy` prints
`Cloudflare Pages project: …` when it does.

What carries over, with Cloudflare's semantics:

- **Routing** from file names: `functions/index.js` is `/`,
  `api/scores.js` is `/api/scores`, `api/scores/[id].js` is
  `/api/scores/:id` (`params.id`), `files/[[path]].js` is `/files` and
  everything under it (`params.path`, an array). The more specific route
  wins (a literal beats `[id]`, which beats `[[path]]`).
- **Handlers:** `onRequest`, and `onRequestGet`, `onRequestPost`, `…Put`,
  `…Patch`, `…Delete`, `…Head`, `…Options`; each a function or an array.
- **Middleware:** `_middleware.js` at any level, for that directory and
  below, outermost first; `export const onRequest = [a, b]` chains.
- **The context:** `request`, `env`, `params`, `data`, `next()`,
  `next(url)`, `waitUntil`, `passThroughOnException`, `functionPath`.
- **Assets:** `env.ASSETS.fetch()`; `next()` with no handler left, and any
  path no function matches, is served from the files. `_redirects` and
  `_headers` work as [above](#_redirects-and-_headers) (headers apply to
  files, not to Function responses, as on Pages). A site with no
  `404.html` is served as a single-page app, as on Pages.
- **`_routes.json`** (in the output directory) limits which paths run the
  Functions; an excluded path serves files for every method. Without one,
  only the paths of your function routes reach the Worker; other files
  never run code.
- **Advanced mode:** `<output dir>/_worker.js`, a file or a directory with
  `index.js`, is the Worker for every request (or the `_routes.json`
  paths). `functions/` is then ignored, as on Cloudflare. Neither
  `_worker.js` nor `_routes.json` is served.

Differences from Cloudflare:

- **Bindings must be in the config.** Bindings and secrets set in the
  Pages dashboard aren't in git, so declare them in the wrangler config
  (`d1_databases`, `kv_namespaces`, `vars`, …) and set secrets with
  `tw secret put` (`.dev.vars` for `tw dev`). When `functions/` reads an
  `env.X` that nothing declares, `tw` warns, naming the file:
  `functions/ reads env.ADMIN_KEY (api/scores.js), but wrangler.jsonc doesn't declare it`.
  Listing a secret in `secrets.required` silences it.
- **`CF-Connecting-IP`** is the client address on a
  [public](public-ingress.md) route, and absent for a tailnet caller. Key a
  rate limit on `getRequester(req)` (a login, tags, or the public IP)
  rather than on the header.
- **Not supported:** branch preview aliases, dashboard config, the Pages
  build system (run your build first), the `CF_PAGES*` variables, Pages
  Plugins (`context.pluginArgs`), and `wrangler pages` commands.
  `--wrangler` doesn't build a Pages project; the built-in compiler does.

## Types: `tw types`

`tw types` is tailworks' `wrangler types`. It writes `worker-configuration.d.ts`
in the app directory: a global `Env` interface built from the wrangler
config and `tailworks.jsonc`.

| source | in `Env` as |
|---|---|
| `vars` | `string`, `number`, `boolean` or an object type (widened, not the literal value) |
| `durable_objects` bindings | `DurableObjectNamespace<import("./src/index").Class>` (plain `DurableObjectNamespace` for a `script_name` binding) |
| `kv_namespaces`, `r2_buckets`, `d1_databases` | `KVNamespace`, `R2Bucket`, `D1Database` |
| `queues.producers` | `Queue` |
| `workflows` | `Workflow` |
| secret names: `.dev.vars`, `.dev.vars.example`, `secrets.required`, and with `--remote` the workload's secrets in mgmt (`tw secret ls`) | `string` |

Bindings tailworks doesn't support are left out, because on tailworks they are
undefined. `--env <name>` types that environment. A `"runtime": "node"` app
gets `NodeJS.ProcessEnv` instead: the variables tailworks sets, `node.env` and the
secret names.

Every `tw init` template ships the file along with a `tsconfig.json`
(`@cloudflare/workers-types` plus `@tailworks/tailnet`, which declares
`ctx.tailnet`), so `npm install && npx tsc --noEmit` passes. The templates
use that global `Env` and don't declare their own: in an ES module a local
`interface Env` would hide the generated one. Run
`tw types` again after you change the config. In CI, run
`tw types --check`: it writes nothing and fails with
[`types_stale`](errors.md#types_stale) if the file is missing or out of date.
`.dev.vars` isn't committed, so for `--check` to see the same secrets on
every machine, list their names in `.dev.vars.example` or
`secrets.required`.

`npm install` fetches `@tailworks/tailnet` (and `@tailworks/mcp`,
`@tailworks/node`) from npm, where each tailworks release publishes them at
its version (since v0.2.0). `tw dev` and `tw deploy` don't need it: for
workerd apps, tailworks resolves those imports from the copies in its own
install.

## `wrangler dev` and Miniflare

`wrangler dev` runs the same workerd. So for code that doesn't depend on
the tailnet, it is a fine inner loop. Use `tw dev` when you need anything
tailworks adds. Here is what changes under `wrangler dev`:

| | `wrangler dev` / Miniflare | `tw dev` |
|---|---|---|
| `getCaller(req)`, `getRequester(req)` | throw `TailnetError` `no_identity` ("request has no verified tailnet caller"), a 500 if the handler doesn't catch it. This is deliberate: tailworks never invents an identity | a real tailnet caller (a local dev node) |
| `fetch()` egress | leaves from your machine's own network. With tailscaled running, tailnet names resolve as **you**, not as the workload | through netd, as the workload node |
| `tailnet.objects` (a node per DO) | ignored: plain Durable Objects, no facets, no per-object egress | as deployed (facets, per-object nodes) |
| `ctx.tailnet` (`spawn`, `kill`), `@tailworks/mcp` caps | no `ctx.tailnet`: `no_identity` | ✅ |
| secrets | `.dev.vars` | `.dev.vars` ([secrets.md](secrets.md)) |
| cron triggers | `/__scheduled` (`--test-scheduled`) | `/__scheduled` with `tw dev --test-scheduled` |
| R2 (`r2_buckets`) | simulated locally | local files and an in-memory index, for one run ([r2.md](r2.md)) |
| KV (`kv_namespaces`) | simulated locally | kept between runs in `<app>/.tailworks/dev-stores.json` ([kv.md](kv.md)) |
| D1 (`d1_databases`) | simulated locally | in memory for one run; pending migrations applied at start ([d1.md](d1.md)) |
| other Cloudflare bindings | simulated locally | absent (warned) |

Code that should run under both can catch the error:
`isTailnetError(e) && e.code === "no_identity"`. Treat that as "not on
tw", never as an authenticated user. The check behind this table:
`wrangler dev` on the `hello` template answered `GET /` with that 500,
because `getCaller` threw.

## Wrangler commands that call Cloudflare

`wrangler deploy`, `wrangler secret put` and `wrangler tail` talk to
Cloudflare's API. wrangler lets you change the API's base URL
(`CLOUDFLARE_API_BASE_URL`), so in principle tailworks could serve a
Cloudflare-compatible API for them. We decided not to:

- **Surface.** wrangler 4.60 calls about 50 distinct
  `/accounts/:id/workers/…` endpoints: script upload (multipart with
  binding metadata), versions and deployments, settings, schedules,
  subdomains, routes, secrets, tails. A deploy chains several of them, in an
  order that isn't documented.
- **Churn.** wrangler ships often (4.60 to 4.162 while this was written),
  and the API it calls changes with it. Pinning a wrangler version for tailworks
  defeats the point. Not pinning means tailworks would chase every release.
- **Identity.** Cloudflare's API authenticates with bearer tokens or
  OAuth. Tailworks authenticates the caller's tailnet identity. A shim would
  either ignore the token (confusing) or bring tokens back into tailworks.
- **The tailworks parts have no place in it.** Preflighting grants and printing
  the `tagOwners` snippet, placement, building and pinning container
  images, and tailnet identity per class can't be expressed through
  wrangler's deploy flow.
- **`wrangler tail`** expects a WebSocket URL from Cloudflare's tail
  service, in Cloudflare's event format. `tw logs -f` already covers this.

Use `tw deploy`, `tw secret put` and `tw logs` instead. They take the
same project.
