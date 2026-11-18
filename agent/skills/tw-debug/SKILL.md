---
name: tw-debug
description: Use when a tailworks app misbehaves or a tailworks command fails - read a workload's logs with `tw logs`, look up an error code, diagnose a failing deploy with `tw doctor`, or trace who called, spawned or killed an object with `tw audit`.
---

# Debug a tailworks app

## Read the logs

<!-- tw-eval: logs -->
```sh
tw logs hello-mcp -n 50 --json       # one request record per line
tw logs hello-mcp --errors --json    # only failed requests: status >= 400, exceptions
```

- `tw logs <workload>` prints the workload's request records, one per
  request: `time`, `method`, `path`, `status`, `durationMs`, `outcome`,
  the `object` and `caller`, `traceId`, and the request's `console.*`
  output (`logs`) and `exceptions` under it.
- Filters: `--errors`, `--status 5xx`, `--object <Class/name>`,
  `--caller <login>`, `--trace <id>`. `-n` counts matching records.
- `--console` prints the app's raw output instead (workerd's own errors,
  output outside a request); with `--json` one
  `{"event": "log", "line"}` per line.
- `-f` follows. With `--json` that is a stream of one JSON object per
  line, so run it in the background and stop it when you've seen enough.
- `--component mgmt|hostd|supervisor` prints tailworks' own daemons' logs
  instead. That only works on the machine that ran `tw up`.
- With replicas, each line has the `host` it came from.

## Error codes

Every tailworks error has a stable code. With `--json` it arrives on stderr as:

```json
{"error": {"code": "acl_denied", "message": "...", "hint": "...", "doc_url": "..."}}
```

Act on `code`, follow `hint`, and read the full entry offline:

```sh
tw docs show errors.md#acl_denied
tw docs search "identity_pending"
```

The ones you'll meet most:

| Code | What to do |
|---|---|
| `not_up` | tailworks isn't running or linked here; ask the user to `tw up` or `tw link` |
| `bad_request` | a wrong flag or argument; `tw <command> --help` |
| `acl_denied` | the policy is missing a `tagOwners` line or a grant; show the user the `hint` (see `tw-policy`) |
| `build_failed` | the bundler's output is in the error; fix the code |
| `deploy_failed` | the app crashed at start; `tw logs <workload>` |
| `identity_pending` | the node didn't come up in time; `tw status`, then `tw logs --component hostd` |
| `unknown_workload` | no such deployment; `tw ls` |
| `peer_not_visible` | the app called a tailnet name that isn't in its netmap (missing or not granted); check `tailnet.connects` and the grants |
| `internet_egress_unavailable` | the app called an internet name; Workers have no internet egress. Put a gateway on the tailnet (docs/recipes.md#call-an-internet-api) |
| `cap_missing` | an MCP caller lacks the tool in its grant's app capability (see `tw-policy`) |

## Check the machine

```sh
tw doctor --json      # {"ok": bool, "version", "checks": [{"name", "status", "detail", "fix"}]}
```

Every failing check comes with a `fix`. Show the user the fixes that need
them (installing something, a sudo step, a credential); apply only the
ones that are plain edits in the app.

## Who did what

```sh
tw audit <object-id> --json     # one object's history: created, called, spawned, killed
tw audit --trace <trace-id>     # every record of one request (trace_id comes with mgmt errors)
tw audit --all --json           # the whole tenant's trail
```

`tw ls --json` lists the object IDs.

## A deploy that hangs or fails

1. `tw status --json`: is every component healthy?
2. `tw logs <workload>`: did the app throw at start?
3. `tw doctor --json`: is the host able to run workerd?
4. `tw logs --component hostd -n 100` on the tailworks host, for node
   bootstrap errors (`bootstrap_failed`, `identity_pending`).
