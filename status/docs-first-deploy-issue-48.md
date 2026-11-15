Last update: 2026-11-15 PT. Issue #48 (first-deploy docs), stacked on PR #49.

# docs-first-deploy-issue-48

Branch `docs-first-deploy-issue-48`, based on
`benchmark-dx-infra-vs-cloudflare-workerd-vercel` (PR #49, not merged yet),
with `origin/master` merged in (ADR-041 `twd upgrade`, `changes/`).

## What's built

1. **Caller grant in the quick start.** README has a new step 5, "Let people
   call it": the grant `autogroup:member` → `tag:twd-wl-support` on
   `tcp:80`. Without it the `curl` in step 6 hangs on any tailnet whose
   policy isn't allow-all. `twd deploy` prints the same grant
   (`cmd/twd/callergrant.go`, one call in `printPreflight`) when it prints
   the tagOwners snippet for new tags. It's text output only: no API or
   `--json` schema change, and twd doesn't check the grant.
2. **`docs/troubleshooting.md`**, "My first deploy doesn't answer": a
   hanging `curl` (no grant), `identity_pending` (from deploy and from a
   request), tagOwners after renaming an app, `peer_not_visible` on egress
   (facet vs plain-DO `src`), and a `failed` deployment in `twd ls` with its
   `error:` line (from PR #49).
3. **`docs/concepts.md`**: a glossary (tailnet, node, tag, tagOwners, grant,
   caller, tenant, mgmt, host, workload, object, sub-agent, process and
   microVM tiers, facet and plain-DO modes, pool nodes / `_pool`, warm keys,
   `twd link`, `twd dev`, kill). Linked from the README's first use of
   "workload", from INSTALL's intro, and from llms.txt.
4. **`docs/install-testing.md`**: the Linux support matrix, the
   unsupported-host table, Lima notes, the restart and reboot timings, the
   microVM tier's test status (amd64, the red-team tarball finding), and
   the STATUS.md / ADR pointers, all moved out of INSTALL.md.
5. **INSTALL.md**: a short intro (the path: install → first deploy →
   upgrade → uninstall, with links). "Try it without a tailnet" (rewritten
   in shorter sentences) and "Isolation" (was "Isolation, honestly", a
   blockquote before "What you need") moved below the macOS install steps.
   ADR-033 is cited by link, not as a draft. 996 → 954 lines.

## Findings and decisions

- **Semantic merge fix.** PR #49's new INSTALL "Upgrade" section described
  only the manual path (`twd down`, `sh get.sh`, `twd up`). Master now has
  `twd upgrade` (ADR-041, docs/upgrading.md). The section now leads with
  `twd upgrade` and keeps the manual path as the alternative; "Back up and
  restore" mentions the backups `twd upgrade` takes.
- **Dangling STATUS.md references.** INSTALL cited STATUS.md "Fast identity"
  and "FV", but those sections are no longer in STATUS.md (dropped in
  `5b9e955`). install-testing.md points at ADR-031 and ADR-030 instead,
  which carry the same numbers.
- The printed caller grant names the workload's `tailnet.tags` only, the
  narrowest grant that makes the workload URL answer. troubleshooting.md
  says to add the class tag to call an object on its own node.
- docs/twd-jsonc.md still says `"tier": "microvm"` needs a Linux host with
  KVM; macOS has the tier too (PR #49 fixed INSTALL). Left for a follow-up,
  outside this issue.

## Tests

- `go test ./cmd/twd -run TestCallerGrant`: ok (new test).
- Link check of README, INSTALL, concepts, troubleshooting and
  install-testing (relative files and heading anchors): no broken links.
- `make -o changes-check lint`: ok (gofmt, every vet, api-check,
  compat-check, check-secrets).
- `make lint` itself fails only in `changes-check`: diffed against master,
  this branch carries PR #49's `schema/cli.schema.json` change, which needs
  a `cli-json` fragment that #49 doesn't have yet (it predates `changes/`).
  `sh scripts/check-changes.sh origin/benchmark-dx-infra-vs-cloudflare-workerd-vercel`
  (this branch's own diff): ok.
- `make test-go`: 40 packages ok; `netd` fails `TestChain` and `TestService`
  only because this worktree has no `node_modules/.bin/workerd` (`make deps`
  not run). Not related to this change.
- `go test ./cmd/twd/... ./docs/... ./schema/... ./agent/...`: ok
  (`TestLLMsTxt` checks the three new docs are listed in llms.txt).
- Not run: `test-js` and the e2e suites (no `node_modules`, no tailnet).

## Gaps / next steps

- The caller grant is not in `twd deploy --json` (it would change
  `schema/cli.schema.json`); the `twd-policy` skill already tells agents
  about it.
