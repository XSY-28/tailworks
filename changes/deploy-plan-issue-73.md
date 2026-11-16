---
kind: feature
upgrade: [cli-json]
---
`twd policy [dir]` prints everything an app needs in the tailnet policy, as
one snippet, before it is deployed. It runs the same check as `twd deploy`'s
preflight and deploys nothing. The snippet holds the missing `tagOwners`, a
grant for each `tailnet.connects`, the Tailscale Service auto-approval and
the public-ingress `nodeAttrs`, plus the grant that lets people call the app
(which twd can't check). It exits `acl_denied` while a checked part is
missing, so CI can run it before a deploy. `twd init` and the README's first
deploy now use it, so a first deploy takes one policy edit. With `--json`
the result is `$defs/policy`.
