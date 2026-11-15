---
name: twd-scaffold
description: Use when creating a new twd app - a Worker, a Durable Object, a Cloudflare Agents SDK Agent, an MCP server, an AI SDK agent or a Node.js workflow - with `twd init`, or when editing an app's twd.jsonc or wrangler.jsonc.
---

# Scaffold a twd app

`twd init <dir> --template <template>` creates an app from a template. Pick
the template closest to what the user asked for:

| Template | What it is | Serves |
|---|---|---|
| `hello` | a plain Worker that returns its verified caller | `/` |
| `agent` | a Cloudflare Agents SDK `Agent`: each instance is a Durable Object with its own SQLite storage and its own tailnet node | `/agents/support-agent/<name>` |
| `mcp` | a stateless MCP server; each tool is allowed or denied by the caller's tailnet grant | `/mcp` |
| `ai-agent` | a Vercel AI SDK agent (one per caller) that calls MCP tools over the tailnet; ships a scripted stand-in model, no LLM key | `/` |
| `node-workflow` | a Vercel Workflow on twd's Node.js process tier (needs Node.js 24+ and `npm install`) | `/` |

<!-- twd-eval: scaffold -->
```sh
twd init hello-mcp --template mcp --json      # {"dir", "template", "workload"}
cd hello-mcp
```

- The directory must not exist or must be empty.
- The workload is named after the directory (`--name <workload>` to pick
  another). Names are lowercase letters, digits and `-`.
- `twd init` renames the template's tags after the app:
  `tag:twd-wl-<workload>` for the workload node and
  `tag:twd-<workload>-<class>` for each Durable Object class. Each new tag
  needs a `tagOwners` line in the user's tailnet policy before the first
  deploy (`twd deploy` prints it; see `twd-policy`). `--keep-tags` keeps
  the template's tag names instead, for tags the user already owns.

## The files

- `src/index.ts`: the Worker. `getCaller(req)` (from `@twd/tailnet`)
  returns the verified caller: `nodeId`, `node`, `tags`, `user`. Use it
  instead of any token or API-key check.
- `wrangler.jsonc`: the Cloudflare config twd reads (`name`, `main`,
  `compatibility_date`, `compatibility_flags`, `vars`,
  `durable_objects.bindings`, `triggers.crons`, `placement`,
  `containers`). Other keys are ignored. `wrangler.toml` is not read.
- `twd.jsonc`: twd's own settings. twd refuses keys it doesn't know, so
  check `twd docs show twd-jsonc.md` before you add one. The ones you'll
  use most:

```jsonc
{
  "name": "hello-mcp",
  "tailnet": {
    "tags": ["tag:twd-wl-hello-mcp"],        // the workload node's tags
    "connects": ["tag:db"],                   // what it calls; deploy preflights these grants
    "objects": {
      // a Durable Object class whose instances each get their own node
      "Room": { "tags": ["tag:twd-hello-mcp-room"] }
    }
  }
}
```

## Things that don't work on twd

Check before you reach for a Cloudflare binding: KV, R2, D1, Queues,
Workflows, Workers AI, Vectorize, Hyperdrive, Browser Rendering, service
bindings and static assets have no binding on twd. Durable Object storage
(KV API and SQLite) works. Call other services over the tailnet with
`fetch("http://<magicdns-name>/...")`; the call leaves from the workload's
own node. The full table: `twd docs show COMPAT.md`.

Outbound `fetch()` reaches tailnet peers only, not the internet.

## Next

Run it locally with `twd dev` (`twd-dev`), then `twd deploy`
(`twd-deploy`).
