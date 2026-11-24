---
kind: fix
upgrade: [protocol]
---
`tw policy --apply` and `tw deploy --apply-policy` now handle a
`tailnet.connects` target that is another tailworks app's tag before that
app is deployed (say `tag:tw-wl-mcp`). Before, the tag had no `tagOwners`
entry, so tailworks left the grants to it for you to add by hand, and the
snippet `tw policy` printed for pasting was rejected by the control plane
because it granted to an undefined tag. Now tailworks adds the tag's
`tagOwners` entry, the same one deploying that app would add, and writes the
grants. The printed snippet includes the entry too. Tenants with their own
owner tag (teams) still define such tags themselves, because the tag may
belong to another team's app. The preflight result has a new
`undefinedConnects` field.
