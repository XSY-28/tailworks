Last update: 2026-11-23 PT. tw policy defines undefined tag:tw* connects targets.

# policy-define-connects-tags

## What changed

`tw policy --apply` left grants to an undefined `tailnet.connects` tag as
manual (b37478f6), including `tag:tw*` tags of tailworks apps not deployed
yet (e.g. `tag:tw-wl-mcp`). The snippet it printed for pasting omitted the
target's `tagOwners`, so the control plane rejected it.

- `mgmt/connectstags.go`: `undefinedConnectsTags`: `tag:tw*` connects
  targets with no `tagOwners` entry (not the app's own tags; none for a team
  tenant, whose target may be another team's tag).
- `mgmt.Preflight`: new `UndefinedConnects`; their entries go in
  `TagOwnersSnippet` (owners `tagOwnersFor(t)`); they make `ok` false.
- `preflightSnippet` includes the snippet when only `UndefinedConnects` is
  set, so `undefinedDst` sees the tag defined and the grants are written.
- CLI: `tw policy` and `tw deploy` print a `✗ tagOwners` line for them; the
  snippet and the `acl_denied` fix include the entry (`needTagOwnersSnippet`).
- Spec: `PreflightResult.undefinedConnects`; docs/tailworks-jsonc.md.

## Tests

See the PR. New: `TestPolicyApplyDefinesConnectsTag`,
`TestUndefinedConnectsTags` (mgmt), `TestPolicySnippetDefinesConnectsTag`
(cmd/tw).

## Not done (from the tw policy UX review)

- `--apply` reprints the whole report after applying and exits
  `acl_denied` even when only unchecked callers remain.
- Manual lines are printed twice in two formats; ETags in text output.
- No `--callers <src>` to write the caller grant.
- Grants from several src tags to one dst aren't merged.
- The `hello` template's placeholder `connects: ["tag:db"]` fails a fresh
  deploy's preflight.
