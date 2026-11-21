---
kind: fix
upgrade: [config-default]
---
`tw up` on a state dir that is already set up now keeps the settings it
was set up with (host ID, hostname prefix, control and API URLs,
`--ephemeral`, `--allow-sibling-egress`, `--local-dev`, warm keys, pool
nodes, heartbeat, …) unless you pass the flag again. Each flag left out
used to fall back to its default and was saved. `tw upgrade` restarts a
`tw up` that doesn't run as a service with `tw up --yes`, so after an
upgrade, or the rollback of a failed one, tailworks came back with those
defaults instead (another host ID, for example), and on a `--local-dev`
state the restart failed. `tw upgrade --plan` on a machine where tw isn't
running no longer lists a rollback step: there is no health check then.
