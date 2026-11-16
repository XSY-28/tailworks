---
name: twd-teardown
description: Use when stopping or removing something on twd - undeploy a workload, kill a Durable Object instance or agent and its sub-agents, or stop twd itself - and to know which of these commands ask for confirmation.
---

# Stop and remove things on twd

These commands act immediately and can't be undone. **Run one only after
the user has agreed to that specific action** (name the workload or
object you're about to stop). An earlier "yes" to something else doesn't
count.

| Command | What it does | Asks first? |
|---|---|---|
| `twd undeploy <workload>` | stops the workload: kills all its objects, releases its host | no |
| `twd kill <object>` | cuts one object (Durable Object instance or workload node) off the tailnet in under a second; its node is deleted | no |
| `twd kill <object> --cascade` | the object and every object it spawned | no |
| `twd secret rm <NAME>` | deletes a secret (the app loses it on the next deploy) | no |
| `twd down` | stops twd on this machine (`--purge` also kills every object) | no |
| `twd tenant rm`, `twd host rm` | remove a tenant or a host (operator commands) | yes, unless `--yes` |
| `twd d1 delete`, `twd kv namespace delete`, `twd r2 bucket rm` | delete a database, a KV namespace or an R2 bucket, with all its data | yes, unless `--yes` |
| `twd queues rm`, `twd queues purge` | delete a queue with its messages, or just its messages | yes, unless `--yes` |
| `twd up` | starts twd | yes, unless `--yes` |
| `twd agent setup`, `twd agent remove` | write or delete these skill files | yes, unless `--yes` |

`--yes` only skips a confirmation prompt. Without a terminal (as you run
commands) a command that asks first refuses with `confirmation_required`
and does nothing. Tell the user what will be lost (or run `--dry-run` where
there is one and show them), and add `--yes` only after they agree to that
action.

## Undeploy a workload

<!-- twd-eval: teardown -->
```sh
twd undeploy hello-mcp --reason "done testing" --json   # {"workload", "deploymentID", "result": {"killed": [...]}}
```

`--reason` is recorded in the audit trail; give one. Undeploying an
already stopped workload fails with `already_stopped`.

## Kill one object

```sh
twd ls --json                                    # find the objectID
twd kill <object-id> --cascade --reason "<why>" --json
twd audit <object-id> --json                     # confirms who killed it and when
```

A killed object stays killed: calls to its name fail with
`object_killed`. Redeploying the workload doesn't bring it back; a new
object name does.

## Afterwards

`twd ls --all --json` also lists killed objects and stopped deployments.
