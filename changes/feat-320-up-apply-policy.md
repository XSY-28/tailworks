---
kind: feature
upgrade: []
---
`tw up --apply-policy` now works on Tailscale too: when the OAuth client
has the **Policy File › Write** scope, `tw up` adds the `tagOwners` and
grants tailworks needs to the tailnet policy instead of printing them for
you to paste. It shows the lines and asks first (`--yes` skips the
question, and is required without a terminal or with `--json`), only ever
adds, and writes with `If-Match`, retrying once if the policy changed in
the meantime. A tag that is already in `tagOwners` keeps its owners.
Without the write scope nothing is written: `tw up` says why and goes on
as before. One line still has to be pasted first: `"tag:tw":
["autogroup:admin"]`, because the admin console only lets you tag an
OAuth client with a tag that exists.

`--policy-apply`, the flag's earlier name on `tw up` (Headscale only),
still works and means the same thing. On Headscale the policy is written
without asking, as before.
