---
kind: breaking
upgrade: [config-default]
---
Every command that deletes data or a tenant's resources now asks first:
`twd d1 delete`, `twd kv namespace delete`, `twd queues rm`,
`twd queues purge`, `twd r2 bucket rm`, `twd tenant rm` and `twd host rm`.
On a terminal it says what will be lost and asks you to type the name.
Without a terminal (a script, a coding agent, `--json`) it refuses with
`confirmation_required` and does nothing; add `--yes` to go ahead.
`twd d1 delete` without `--yes` now prompts on a terminal, and its refusal
code is `confirmation_required` (it was `bad_request`).

Operator action: scripts that run these commands need `--yes`.
