---
kind: feature
upgrade: [cli-json]
---
Testing twd apps (docs/testing.md). `@twd/tailnet/testing` adds
`withCaller` / `withPublic` and a fake `ctx.tailnet` for unit tests, in Node
or under `@cloudflare/vitest-plugin`, which runs twd apps' Worker and Durable
Object logic as on Cloudflare. `twd dev --caller <name>[=<tag>,…]` adds extra
dev identities, each with its own local URL, listed as `callers` in the
`ready` event and in `twd dev --once --json`; `@twd/tailnet/testing/dev`
(`startDev`) runs an app under `twd dev` for black-box tests. Every template
now has a test and an `npm test` script. `twd dev` no longer misses an edit
made right after its `ready` event.
