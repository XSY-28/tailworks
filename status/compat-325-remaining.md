Last update: 2026-11-24 PT. Issue #325, second pass: the `examples/next/*` Agents SDK examples and the AI SDK's `fastify`, `nest` and `mcp` added to the compat suite; COMPAT.md tables regenerated from the run.

# Upstream examples, second pass (#325)

Continues `status/compat-325-upstream-examples.md`.

## What's built

- `e2e/compat/upstream_more.go`: 13 new projects (published in the COMPAT.md
  tables), hooked in with one line in `Projects()`:
  - cloudflare/agents `examples/next/`: `lifecycle`, `mcp-client`, `queue`,
    `routing`, `schedules`, `sessions`, `streams`, `tasks`, `websockets`
    (`ag-next-*`), with probes for what each README does.
  - vercel/ai: `fastify`, `nest`, `mcp` `http` server, `mcp` `sse` server
    (`ai-fastify`, `ai-nest`, `ai-mcp-http`, `ai-mcp-sse`), through a
    generalised `nodeServerEx` (module, port, whether to script OpenAI).
    The MCP SSE probe is a small SSE-transport MCP client.
- `upstream.go`: the not-run list loses the 9 `next/` examples, `fastify`,
  `nest` and `mcp`; the Workers AI row says why a scripted `env.AI` isn't
  possible without changing each example. `openAIStub` is now plain
  JavaScript, so Nest's `tsc` build (which compiles every .ts file in the
  directory) can take it as a .js file.
- `report.go`: a count note for the AI SDK table (`mcp` is counted twice).
- `docs/COMPAT.md` and `upstream-results.json` regenerated with
  `make compat-upstream` from the full `make e2e-compat-dev` run below. The
  deployed results are the previous ones (the new rows say "not run"
  deployed).

## Results (`make e2e-compat-dev`, 2026-11-24, darwin/arm64)

| Upstream | Examples | pass | pass with change | fail | not run |
|---|---|---|---|---|---|
| cloudflare/agents | 69 | 17 | 0 | 6 | 46 |
| vercel/ai | 26 | 0 | 4 | 3 | 19 |
| vercel/mcp-handler | 3 | 0 | 3 | 0 | 0 |

New failures:

- `next/lifecycle`: after `ctx.abort()` the object is never recreated. Filed
  [#395](https://github.com/rosskukulinski/tailworks/issues/395); stock
  `wrangler dev` 4.168.0 recovers.
- `next/routing`: `Could not serialize object of type "RpcPromise"` in the
  hub's `createChat()`. Same as the playground, tied to the existing
  [#391](https://github.com/rosskukulinski/tailworks/issues/391) (repro
  added there as a comment); stock `wrangler dev` passes.
- `fastify`, `nest`, `mcp` `sse`: not tailworks gaps, each checked against
  stock `wrangler dev`, which fails too (pino at startup; unresolvable
  optional `require()`s; a cross-request write that the runtime cancels).
  Expected failures with the reason, no issue.

## Tests

- `make e2e-compat-dev` (all 33 published projects):
  `ok github.com/rosskukulinski/tailworks/e2e/compat 55.289s`,
  counts `PASS:25 XFAIL:8`.
- Earlier subset runs (`TW_COMPAT_ONLY=...`) while writing the probes.
- `make compat-upstream`, then `make lint`, `make test`: see the PR.
- Not run: `make e2e-compat` (deployed on tailnet 1: needs credentials not
  available here).

## Left

- The nightly publishes each night's tables in its job summary but doesn't
  commit them; committing them automatically needs write permission for the
  workflow, a maintainer decision.
- The README's "run unmodified" sentence is unchanged (maintainer's call).
- The new rows have no deployed result until `make e2e-compat` runs.
