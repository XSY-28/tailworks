---
name: twd-public
description: Use when part of a twd app must be reachable from the public internet - a webhook receiver, an OAuth callback, a public page - via twd.jsonc "public" routes over Tailscale Funnel, and `twd expose on|off`.
---

# Public routes on twd

Everything on twd is tailnet-only by default. To serve chosen paths to
the internet, list them in `twd.jsonc`; twd serves them over Tailscale
Funnel at `https://<workload>.<tailnet>.ts.net/`. Unlisted paths answer
`404` and never reach the app.

```jsonc
{
  "public": {
    "routes": ["/webhooks/*", "/oauth/callback"],   // "/x" exact, "/x/*" prefix, "/*" everything
    "upgrade": []                                   // routes allowed to upgrade to WebSocket/SSE
  }
}
```

- Keep the list as small as the feature needs. There's no
  `"public": true`; `"/*"` means everything, so use it only if the user
  asks for it.
- **Authenticate public requests yourself** (webhook signatures, OAuth
  state). They carry no tailnet identity: `getCaller(req)` throws on them.
  Use `getRequester(req)` from `@twd/tailnet`, which returns
  `{kind: "public", ip, ...}` or `{kind: "tailnet", ...}`.
- Public requests can reach Durable Objects that already exist but can't
  create new ones unless the class sets `"public": {"create": true,
  "maxObjects": N}` in `tailnet.objects`.

## Policy and tenant gate

`twd deploy` preflights both and fails with the fix in `hint`:

- the policy needs `tag:twd-public` owned by `tag:twd` and a `funnel`
  node attribute for it (`funnel_required`); show the lines to the user;
- the tailnet needs HTTPS certificates turned on;
- the tenant must allow public ingress. Only an admin changes that, with
  `twd tenant set <tenant> --public-ingress allow`.

## Deploy, open and close

```sh
twd deploy --wait-public 10m --json    # result has "public": {"url", "exposed", ...}
twd expose off <workload> --json       # closes every public route now; survives redeploys
twd expose on <workload> --json        # opens them again
```

The first request to a new public name can take a few minutes while its
certificate and DNS are created.

More: `twd docs show public-ingress.md`.
