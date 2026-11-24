Last update: 2026-11-23 PT. tw policy: defines undefined tag:tw* connects targets; one report for --apply; merged grants; hello without tag:db.

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

## Also (tw policy UX review)

- `--apply` prints the header and checks once (`printPolicyHeader`), then the
  plan, then one result: "now has everything … run tw deploy" or "Still
  missing" with only what's left. No ETags in text output (still in
  `--json` and the audit record).
- mgmt's `manual` lines read `grant <src> → <dst> <ip>: not written: <why>`
  (`grantLabel`, `checkReason`); the CLI lists them once and they stay in
  the snippet.
- `PreflightResult.MissingGrants()` (mgmt/preflightgrants.go) merges missing
  grants by (dst, ip): one grant with every src. Used by mgmt's apply
  snippet, `tw policy`, `tw deploy`'s preflight output and its fix.
- `tw policy` names what carries each tag when the app has object classes.
- `hello` template: `connects: []` with a commented example; cliflow adds
  `tag:db` itself before its preflight-refusal check.

## Not done

- `--callers <src>` to write the caller grant: RT-06 (`policy.Check`)
  refuses grants with user, group or `autogroup:*` sources by design.
  Writing one is a security-model change, so it needs a decision first.
