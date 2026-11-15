---
kind: feature
upgrade: [cli-json]
---
**Typed templates and `twd types`.** Every `twd init` template has a `tsconfig.json` and `@cloudflare/workers-types` and `typescript` dev dependencies, so `npm install && npx tsc --noEmit` passes and editors know `getCaller`, `Request`, `DurableObjectState` and `ctx.tailnet` (`@twd/tailnet` now declares `ctx.tailnet` on `DurableObjectState` and `ExecutionContext`). New `twd types`, like `wrangler types`, writes `worker-configuration.d.ts` with the app's `Env` (vars, Durable Object, KV, R2, D1, Queue and Workflow bindings, secret names from `.dev.vars`, `.dev.vars.example`, `secrets.required` or `--remote`); `twd types --check` fails with `types_stale` in CI. The `@twd/*` packages and `create-twd` are ready to publish to npm, and `npm create twd` runs `twd init` when twd is installed.
