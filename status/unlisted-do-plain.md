# unlisted-do-plain (#492)

Unlisted Durable Object classes are internal (`"node": false`, plain DO)
whether or not tailworks.jsonc exists.

- `cmd/tw/defaultobjects.go`: `defaultInternalClasses` writes
  `{"node": false}` into the packed tailworks.jsonc (comments kept, as
  `defaultWorkloadTags` does) for every wrangler `durable_objects` class
  (no `script_name`) missing from `tailnet.objects`. `defaultTwConfig`
  now carries only the tags; the classes come from the same default.
  `estimateAppNodes` needs no change: it reads the defaulted config, so
  `tw deploy` lists them as sharing the workload node.
- hostd unchanged (bundle.go's facet default and nodeless.go stay for
  bundles packed by an older tw, as the issue asks). Follow-up for a later
  release: delete `facetEgressObject`'s nodeless branch and `nodelessEnsure`.
- mcp template: `objects` block dropped. Docs: egress-modes (also the stale
  "facets have no alarms" row), create-admission, security-model,
  tailworks-jsonc, wrangler, ADR-006 amendment paragraph.
- e2e/upgrade: an unlisted class `Tally` (packed old-style) whose storage
  must survive the hostd upgrade.
- `changes/unlisted-do-plain.md`: kind breaking, `config-default`.

## Tests

See the PR body.
