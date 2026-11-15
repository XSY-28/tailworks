---
name: twd-debug
description: Use when a twd app misbehaves or a twd command fails - read a workload's logs with `twd logs`, look up an error code, diagnose a failing deploy with `twd doctor`, or trace who called, spawned or killed an object with `twd audit`.
---

# Debug a twd app

## Read the logs

<!-- twd-eval: logs -->
```sh
twd logs hello-mcp -n 50 --json       # one {"event": "log", "line", "workload"} per line
```

- `twd logs <workload>` prints the workload's own log: the app's
  `console.log` output and workerd's errors.
- `-f` follows it. With `--json` that is a stream of one JSON object per
  line, so run it in the background and stop it when you've seen enough.
- `--component mgmt|hostd|supervisor` prints twd's own daemons' logs
  instead. That only works on the machine that ran `twd up`.
- With replicas, each line has the `host` it came from.

## Error codes

Every twd error has a stable code. With `--json` it arrives on stderr as:

```json
{"error": {"code": "acl_denied", "message": "...", "hint": "...", "doc_url": "..."}}
```

Act on `code`, follow `hint`, and read the full entry offline:

```sh
twd docs show errors.md#acl_denied
twd docs search "identity_pending"
```

The ones you'll meet most:

| Code | What to do |
|---|---|
| `not_up` | twd isn't running or linked here; ask the user to `twd up` or `twd link` |
| `bad_request` | a wrong flag or argument; `twd <command> --help` |
| `acl_denied` | the policy is missing a `tagOwners` line or a grant; show the user the `hint` (see `twd-policy`) |
| `build_failed` | the bundler's output is in the error; fix the code |
| `deploy_failed` | the app crashed at start; `twd logs <workload>` |
| `identity_pending` | the node didn't come up in time; `twd status`, then `twd logs --component hostd` |
| `unknown_workload` | no such deployment; `twd ls` |
| `peer_not_visible` | the app called a name that isn't on the tailnet or isn't granted; check `tailnet.connects` and the grants |
| `cap_missing` | an MCP caller lacks the tool in its grant's app capability (see `twd-policy`) |

## Check the machine

```sh
twd doctor --json      # {"ok": bool, "version", "checks": [{"name", "status", "detail", "fix"}]}
```

Every failing check comes with a `fix`. Show the user the fixes that need
them (installing something, a sudo step, a credential); apply only the
ones that are plain edits in the app.

## Who did what

```sh
twd audit <object-id> --json     # one object's history: created, called, spawned, killed
twd audit --trace <trace-id>     # every record of one request (trace_id comes with mgmt errors)
twd audit --all --json           # the whole tenant's trail
```

`twd ls --json` lists the object IDs.

## A deploy that hangs or fails

1. `twd status --json`: is every component healthy?
2. `twd logs <workload>`: did the app throw at start?
3. `twd doctor --json`: is the host able to run workerd?
4. `twd logs --component hostd -n 100` on the twd host, for node
   bootstrap errors (`bootstrap_failed`, `identity_pending`).
