---
name: twd-policy
description: Use when a twd workload needs tailnet policy changes - tagOwners for a new app's tags, a grant so people or other workloads can call it, a grant so it can call something else (tailnet.connects), or MCP tool permissions via an app capability.
---

# Tailnet policy for a twd workload

On twd, access control is the user's Tailscale tailnet policy, not code.
Each workload node carries the tags from `twd.jsonc` `tailnet.tags`; each
Durable Object class with its own nodes carries its class's tags. Grants
between tags decide who can call whom.

**You draft policy; the user applies it.** Show the user the exact lines
and ask them to add them in the Tailscale admin console (Access
controls). twd never changes the policy during a deploy, and you
shouldn't either.

## 1. Let twd own the app's tags

Every tag the app uses needs a `tagOwners` entry that includes `tag:twd`,
or twd can't create nodes with it. `twd deploy` checks this first and, if
a line is missing, fails with `acl_denied` and puts the lines in the
error's `hint`. They look like this:

```jsonc
"tagOwners": {
  "tag:twd-wl-hello-mcp": ["autogroup:admin", "tag:twd"],
}
```

A class whose objects spawn children (`ctx.tailnet.spawn`) needs the
child's tag owned by the parent's tag too:

```jsonc
"tag:twd-myapp-child": ["autogroup:admin", "tag:twd", "tag:twd-myapp-parent"],
```

## 2. Let callers reach it

Nothing on the tailnet can call a new workload until a grant allows it.
Workloads listen on port 80:

```jsonc
"grants": [
  // people in the tailnet may call the app
  {"src": ["autogroup:member"], "dst": ["tag:twd-wl-hello-mcp"], "ip": ["tcp:80"]},
]
```

Use the narrowest `src` that works: a specific user, a group, or another
workload's tag.

## 3. Let it call something

List what the app calls in `twd.jsonc` `tailnet.connects` (tags or
hosts). `twd deploy` preflights a grant for each one and prints the grant
to add if it's missing:

```jsonc
{"src": ["tag:twd-wl-hello-mcp"], "dst": ["tag:db"], "ip": ["tcp:5432"]},
```

## 4. MCP tool permissions

The `mcp` template authorizes each tool call from the caller's grant: the
tools a caller may use are an app capability on the grant to the server
(`example.com/cap/mcp` unless `MCP_CAP` in `wrangler.jsonc` says
otherwise). A caller without the tool gets `cap_missing`.

```jsonc
{
  "src": ["tag:twd-wl-my-agent"],
  "dst": ["tag:twd-wl-hello-mcp"],
  "ip": ["tcp:80"],
  "app": {
    "example.com/cap/mcp": [{
      "tools": ["lookup_order", "refund"],
      "limits": {"refund": {"maxUsd": 50, "perDayUsd": 200}}
    }]
  }
}
```

## Checking it

- `twd deploy --json` again: the `preflight` in its result lists what it
  checked, and `acl_denied` lists what's still missing.
- A grant applies within seconds of saving the policy; nodes don't need
  to restart.
- `twd docs show twd-jsonc.md#tags-and-twd-init` explains how
  `twd init` names tags and how to reuse tags across apps.
