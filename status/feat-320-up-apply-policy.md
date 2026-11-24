Last update: 2026-11-24 08:10 PT. `tw up --apply-policy` on Tailscale (issue #320 parts 1 and 2); part 3 (the stopwatch) not done.

# feat-320-up-apply-policy

Issue #320: collapse the quick start's manual policy pastes, and measure
install to first fetch against ADR-021's 10-minute target.

## What's built

### `tw up --apply-policy [--yes]` on Tailscale

`cmd/tw/up_policyapply.go` (new). In oauth mode, after the OAuth client
exchanges and the policy is read and before anything is minted, `tw up`:

1. works out what the policy lacks of `operatorSnippet` (the README step 2
   snippet: the three `tagOwners` entries and the three grants);
2. merges it and has the control plane validate the result;
3. prints the lines as a `+` diff and asks `Add these lines to the tailnet
   policy? [y/N]` (`--yes` skips it; without a terminal, or with `--json`,
   `--yes` is required: `confirmation_required`);
4. writes with `policy.ApplyRetry`: read with ETag, merge, validate,
   `If-Match` write, one retry on a 412, then `policy_conflict`.

It reuses `confirmPolicyWrite` (the prompt of `tw policy --apply`) and
`internal/policy` (`Check`, `MergeWith`, `VerifyAdditive`, `ApplyRetry`),
the same code mgmt runs for `tw policy --apply` and `tw deploy
--apply-policy`. It can't go through mgmt's endpoint as those do: mgmt
isn't running yet, and its node can't be minted until the policy has the
lines.

What happens in each case:

| Case | Result |
|---|---|
| Lines missing, confirmed | written; `tw up` goes on; `policyApplied: true` |
| Nothing missing | "nothing to write"; no validate, no write, no question |
| Prompt declined | `cancelled`; nothing written, nothing minted |
| No terminal (or `--json`), no `--yes` | `confirmation_required`, after the lines were shown; the fix has `tw up --apply-policy --yes` and the lines |
| 403 on validate or on the write (no Policy File › Write) | a note says why it wasn't applied; then as without the flag: `policy_missing_tag_owners` with the snippet if `tag:tw` doesn't own what this layout mints, otherwise `tw up` goes on and prints the missing grants at the end |
| Client can't read the policy | a note and the whole snippet; `tw up` goes on (the first mint proves ownership, as before) |
| 412 on the write | read, merge, validate and write once more; a second 412 is `policy_conflict` |
| The control plane rejects the merged policy | `policy_apply_failed` with the lines to paste; nothing minted |

### Decisions

- **One flag, two spellings.** `tw up` already had `--policy-apply`
  (Headscale only); `tw deploy` has `--apply-policy`. `tw up` now takes
  `--apply-policy` on both control planes and keeps `--policy-apply` as
  the same flag (one variable, `f.BoolVar`), so existing Headscale command
  lines and `make e2e-headscale` keep working. Docs and messages for `tw
  up` use `--apply-policy`.
- **Headscale is unchanged: it still writes without asking.** Adding the
  prompt there would break unattended `tw up --control=headscale
  --policy-apply` command lines that don't pass `--yes`. So the one flag
  asks on Tailscale and doesn't on Headscale. The help text says so.
- **`tw tenant add --policy-apply` is unchanged** (no alias, no prompt, and
  it writes `baseSnippet`, without the admin grant). Giving it the
  `--apply-policy` spelling without the prompt would make that spelling
  mean "writes without asking" in one more place. Left for a follow-up.
- **Owners of an existing tag are never widened.** The existing check in
  `tw up` already accepts a `tag:tw` owned by something other than
  `autogroup:admin` ("a group, say"). A plain merge of `operatorSnippet`
  would append `autogroup:admin` to such a tag. `upPolicyWant` drops
  `tag:tw` when it is already defined, and asks only for `tag:tw` as an
  owner of an existing `tag:tw-mgmt` / `tag:tw-host`.
- **The admin grant goes through `policy.Options.AllowSrcs`.** Its source
  is `autogroup:admin`, which `policy.Check` refuses by default (RT-06).
  `upPolicyOptions` allows exactly that source. This is the first caller
  of `AllowSrcs` outside tests. The snippet is built by `tw`, not read
  from a bundle.
- **The whole snippet is written even in single-process mode**, where
  `tag:tw-host` and the host ⇄ mgmt grants matter only for `tw host add`.
  The issue asks for "the ones in README step 2", and it saves a second
  policy edit at scale-out.
- **Validate before asking.** A client without the write scope, or a
  merged policy the control plane would reject, is found out before the
  question, not after the user has said yes.

### Two manual policy steps remain (the quick start can't be fully collapsed)

1. **`"tag:tw": ["autogroup:admin"]` must be pasted before the OAuth client
   exists.** The admin console only offers existing tags when a client is
   created, and the client is what writes the policy. README step 2 is
   now that one line, with the full snippet in a `<details>` block for
   people who'd rather not grant write.
2. **The caller grant in step 5.** `tw deploy --apply-policy` writes the
   app's own tag ownership but never the grant that lets people call the
   app (`src: autogroup:member`): mgmt's RT-06 check refuses non-`tag:tw*`
   sources, and who may call an app is the admin's decision ("? callers
   not checked"). Without it the step 6 `curl` doesn't reach the app. Step
   5 says so and still asks for that one paste. Removing it would mean
   tailworks writing an `autogroup:member` grant, which is a security
   decision this branch didn't make.

### Docs

- `README.md` quick start steps 2 to 5 only, plus the "Next" bullet that
  step 5 made redundant.
- `docs/INSTALL.md` "The OAuth client" (steps 1 to 3), the Headscale
  paragraph and the `policy_missing_tag_owners` troubleshooting line.
- `docs/headscale.md`, `docs/services.md`, `docs/errors.md`: the flag's
  spelling, and `tw up --apply-policy` named where it can now raise the
  error.
- `docs/cli.md` regenerated (`make cli-docs`).
- `tw up` with no credential (`oauthConsoleSteps`) recommends Policy File
  › Write.
- `cmd/tw/docs_policy_test.go` now also accepts the README's one-line
  `tag:tw` snippet (exactly one), next to the two full ones.

## Tests

Commands run on 2026-11-24, from this worktree unless noted.

| Command | Result |
|---|---|
| `go test ./cmd/tw/... ./internal/policy/... -count=1` (after `make deps`) | ok (cmd/tw 182 s, internal/policy 0.3 s) |
| `go test ./cmd/tw/ -run 'TestCLIOAuth$' -count=1 -v` | PASS (11.5 s); not skipped, so the real-binary checks ran |
| `make lint` | exit 0 (api-check, compat-check, changes-check, gofmt, vet with every e2e tag, check-secrets) |
| `make changes-check` | ok (no upgrade-relevant paths changed); a `changes/` fragment is added anyway, for the release notes |
| `make cli-docs` | `docs/cli.md` regenerated; `TestCLIReferenceUpToDate` passes |
| `sh ~/.claude/scratch/mbp-test.sh test` (`make test` on mbp, `-race`) | exit 0: 127 Go packages ok, none failed (cmd/tw 252 s); `npm test` and the CLI package tests pass |

| `make e2e-headscale` (real Headscale v0.29.7 in Docker) | PASS, 55.6 s: `TestTwUp` passes `--policy-apply`, so the earlier spelling still writes the policy |
| after merging `origin/master` (10efb468): `make lint`; `go test ./cmd/tw/... ./docs/... -count=1` | exit 0; ok |

The `mbp-test.sh` run was before that merge, which brought in only docs,
status files and `e2e/public` test changes.

Not run: `make e2e-oauth` and the other real-tailnet suites (this branch
touched no real tailnet), and `make e2e-tw-upgrade` (`tw upgrade` restarts
with `tw up --yes`; `--yes` without `--apply-policy` is still accepted and
ignored, as every `tw up` in the hermetic CLI tests passes it).

New tests:

- `cmd/tw/up_policyapply_test.go`, against an `httptest` fake of the
  Tailscale policy API (GET with ETag, validate, `If-Match` POST) through
  the real `tsapi.Client`: applied; already present (no validate, no
  write); owners not widened; no write scope (403 on validate, 403 on the
  write, grants-only, unreadable policy); one conflict retried and two
  conflicts refused; declined, no terminal, `--json`, answered `y`; the
  flag alias.
- `cmd/tw/up_policyapply_ext_test.go`, called from `TestCLIOAuth`: the
  real `tw` binary with `--apply-policy` and with `--policy-apply`, no
  `--yes`, stops with `confirmation_required` after showing the lines; with
  `--yes` and a control plane that refuses the write, `policy_apply_failed`.
  No key is minted in either.

Not covered by a test: a successful write followed by a full `tw up`
through the real binary (the hermetic fake API answers every policy write
with a 500; the write itself is covered by the unit tests above).

## Not done: part 3 of the issue (the stopwatch)

No measurement was made, and no number here is an estimate. What a real
run needs and what this machine has:

- **A release that has the flag.** `curl … get.sh | sh` installs the
  published release (v0.4.0), where `tw up --apply-policy` doesn't exist
  on Tailscale. Timing it would time the old six-step flow. The
  measurement has to follow the release that carries this branch.
- **A fresh macOS VM.** None exists here: `tart list` shows one unrelated
  VM and no macOS one, and Lima only runs Linux guests. A fresh
  Linux VM is possible (Lima is installed).
- **A tailnet whose policy lacks tailworks' lines.** The dedicated test
  tailnet's policy (`TS_CREDS`, `tailnet1_api.env` is present) already has
  them from earlier suites, so `--apply-policy` would report "nothing to
  write" and the step being measured would be skipped. Taking the lines
  out first is a removal, which the safety rules for the real-tailnet
  suites don't allow (`docs/install-testing.md`: additions only).
- **A person at the admin console.** The issue's stopwatch includes
  creating the OAuth client and the two pastes above. `e2e-oauth` creates
  its client with the API key, which leaves out the part of the ten
  minutes most likely to be the long pole.

Nothing on a real tailnet was touched by this branch: no policy write, no
OAuth client, no device.

To finish it: after the release, on a fresh macOS VM and a fresh Linux VM,
with a new tailnet (or one reset by hand to lack the lines), time from
`curl … | sh` to the first `curl` of the deployed app, following the README
as written, and record the two numbers and the long pole here or in a
follow-up status file.

## Gaps and follow-ups

- Part 3 above.
- `tw tenant add --policy-apply`: same spelling and prompt as `tw up`.
- `tw up --apply-policy` doesn't write `autoApprovers` for `svc:tw-mgmt`
  (`docs/services.md`, already a gap in `status/fix-295-autoapprovers.md`).
- `policy.MergeWith` rewrites the bytes of the policy slice it is given
  when it formats the result (found by a test here: the caller's copy of
  the policy changed). `up_policyapply.go` passes clones. Other callers
  (`ApplyWith` keeps `res.Before` "for backups") may see a reformatted
  `Before`. Not changed here.
