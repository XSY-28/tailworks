---
name: twd
description: Use when the user wants to build, run, deploy, inspect, debug or stop a Worker, Durable Object, Agent or MCP server on twd (a self-hosted, tailnet-native Workers runtime driven by the `twd` CLI). Start here; it maps each task to the right twd skill and command.
---

# twd: Workers and Durable Objects on a tailnet

twd runs Cloudflare-style Workers and Durable Objects on the user's own
machines, on stock workerd. Every workload, and every Durable Object
instance, is its own node on the user's Tailscale tailnet. Callers are
identified by their WireGuard identity (`getCaller(req)` from
`@twd/tailnet`), never by tokens or API keys. Tailnet policy (tags,
`tagOwners`, `grants`) decides who can call what.

You drive twd only through the `twd` CLI.

## Before anything else

```sh
twd version --json               # is twd installed? which version?
twd status --json                # is a twd control plane up (or linked)?
```

- If `twd` is not found, point the user at the install steps in the twd
  README; don't install it yourself.
- If `twd status` fails with `not_up`, ask the user to start twd (`twd up`,
  run once by a tailnet admin) or link this machine to one
  (`twd link twd-mgmt.<tailnet>.ts.net`). Both need the user's tailnet
  credentials, so the user runs them, not you.
- `twd doctor --json` checks this machine and prints a fix for each failing
  check.

## Which skill for which task

| Task | Skill | Main commands |
|---|---|---|
| New Worker, Durable Object, Agent or MCP server | `twd-scaffold` | `twd init` |
| Run it locally, no tailnet | `twd-dev` | `twd dev` |
| Deploy it to the tailnet | `twd-deploy` | `twd deploy`, `twd ls`, `twd status` |
| Logs, a failing deploy, an error code | `twd-debug` | `twd logs`, `twd doctor`, `twd audit`, `twd docs` |
| Let something call it (or let it call something) | `twd-policy` | tailnet policy `tagOwners` and `grants` |
| API keys and other secrets | `twd-secrets` | `twd secret` |
| Cron triggers | `twd-cron` | `triggers.crons`, `twd dev --test-scheduled` |
| Public URLs (webhooks, OAuth callbacks) | `twd-public` | `twd.jsonc` `public`, `twd expose` |
| Stop, kill or remove something | `twd-teardown` | `twd undeploy`, `twd kill` |

If your agent can't load skills by name, run `twd agent show <skill>` to
print one, and `twd agent ls` to list them.

## twd's MCP server

twd also serves its operations as MCP tools (`whoami`, `status`, `ls`,
`logs`, `audit`, and when granted `deploy`, `kill`, `undeploy`). If you
have them, prefer them for inspecting and operating: they are typed,
authorized per tool by the user's tailnet policy, and audited. A
`cap_missing` error means the user hasn't granted that tool; show them its
`snippet` instead of working around it with the CLI. To set it up, the
user runs `twd mcp --config`.

## Rules for agents

1. **Use `--json` and parse it.** Every command takes `--json`. A result is
   one JSON document on stdout. An error is one line on stderr:
   `{"error": {"code", "message", "hint", "doc_url", ...}}`. `twd deploy
   --json` also writes progress events on stderr, one JSON object per line
   (`{"event": "build", ...}`). `twd dev --json` and `twd logs -f --json`
   stream one JSON object per line on stdout. The schemas are in
   `schema/cli.schema.json` in the twd repo.
2. **Act on the error code**, not the message text. `hint` says what to do
   next. `twd docs show errors.md#<code>` prints the full entry offline.
3. **Check flags before you guess them.** `twd <command> --help` lists every
   flag. Don't invent flags; twd refuses unknown ones with `bad_request`.
4. **Ask before anything destructive.** `twd kill`, `twd undeploy`,
   `twd down`, `twd tenant rm`, `twd host rm` and `twd secret rm` act
   immediately; they have no undo. Run them only after the user has said
   yes to that specific action. Never pass `--yes` to `twd up` or
   `twd agent setup` on the user's behalf; those prompts exist so the user
   sees what will change.
5. **Never handle credentials.** Don't write API keys, OAuth client
   secrets or auth keys into files, commands or `twd.jsonc`. Secrets go
   in with `twd secret put <NAME>`, which reads the value from stdin; let
   the user type or pipe it (see `twd-secrets`).
6. **The tailnet policy belongs to the user.** When twd prints a policy
   snippet (`tagOwners`, `grants`), show it to the user and ask them to add
   it in the Tailscale admin console. Don't edit the policy yourself.

## More

- `twd help` lists every command; `twd <command> --help` shows its flags.
- `twd docs search <words>` searches twd's docs offline;
  `twd docs show <doc>[#<anchor>]` prints a doc or one section.
- What works and what doesn't, compared with Cloudflare Workers:
  `twd docs show COMPAT.md`.
