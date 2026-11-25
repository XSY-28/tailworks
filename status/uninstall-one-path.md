Last update: 2026-11-24. `tw uninstall` (#487); install.sh/get.sh refuse another version over a running tw (#485).

## What's built

- **`tw uninstall [--keep-state] [--dry-run] [--yes] [--force]`** (Set up group;
  `cmd/tw/uninstall.go`). Guards first, before any change: the microVM tier's
  `/etc/tailworks/setup.json` (fix: `sudo <prefix>/bin/tw host setup --undo`),
  a configured state dir whose mgmt isn't healthy (fix: `tw up`, or `--force`),
  other hosts registered with this mgmt, and a state dir that is `$HOME` or
  contains it (all `uninstall_blocked`, new in docs/errors.md). Then it prints
  the plan (steps, every path it deletes, what it keeps, what's left for you)
  and asks (`confirmDestroy`, type `uninstall`; `--yes` without a terminal).
  Steps: `downPurge` (tw down --purge), `DELETE /v1/hosts/<id>?drain=true` in
  the separate-host layout, `downStop` with service removal, mgmt's node
  deleted with the tenant's OAuth client or Headscale key (403 → admin-console
  line), `tw agent remove` for detected agents (user scope), the install
  prefix's files (or the npm command printed), and the state dir. Only
  entries from INSTALL.md's "What is in ~/.tailworks" table are deleted;
  anything else is listed as kept and the dir stays. `--keep-state` keeps the
  state entries and clears `mgmtNodeID` in tw.json so the next `tw up`
  rejoins. Link-only machines: link.json, cli/, the prefix.
- `cmdDown` split into `downPurge` and `downStop` (up.go), no behaviour change.
- `uninstall` skips the once-a-day update notice (it would recreate
  update-check.json).
- **install.sh**: another version over a live `$PREFIX/supervisor.pid` is
  refused with the `tw upgrade --from <dir>` command; same-version repair and
  `--force` go ahead. `--uninstall --purge` now `rm -rf`s the whole prefix
  (refuses `/` and `$HOME`).
- **get.sh**: the same check before downloading; `TW_INSTALL_FORCE=1` passes
  `--force` to install.sh.
- Docs: INSTALL.md (npm removal, macOS Day 2, the ~/.tailworks table, the
  Uninstall section, host setup undo), upgrading.md (get.sh/install.sh refuse
  over a running install), errors.md, cli.md, cli.schema.json
  (`$defs/uninstall`), `changes/uninstall-one-path.md`.

## Decisions

- Channel detection (prefix / npm / neither) is self-contained in
  uninstall.go: `installChannel()` (#498) had not landed on master.
- An extracted release directory (it has SHA256SUMS) is never treated as the
  install prefix to remove.
- Added `--dry-run` (not in the issue) so the plan can be read without a
  prompt, as `tw agent remove --dry-run` does.
- Other registered hosts block even with `--force` (conservative).

## Tests

- `go test ./cmd/tw -run TestUninstall` (8 tests, temp HOME/state, seams for
  the setup record, prefix, node deletion, agents): ok.
- `make install-test`: ok (new cases: refusal over a running tw, repair,
  `--force`, get.sh refusal before download, `TW_INSTALL_FORCE=1`,
  `--uninstall --purge` removes everything).
- `make lint`: ok (before merging master; the merge brought only help-flag
  changes, `make cli-docs` rerun after it).
- `make test` (after merging origin/master): failures in cmd/tw
  (TestCLIEndToEnd, TestCLIOAuth, TestCLIMultiTenant, TestDevPython), mgmt
  (TestWorkloadReplicas, TestRT3_H23_PlacementPoison, TestTenantAdminScope)
  and hostd (8 tests), all heartbeat or client timeouts on a heavily loaded
  machine. Rerun alone: the cmd/tw and mgmt ones pass; hostd ran out of
  disk (`No space left on device` while linking, 1.1 GiB free on the shared
  machine). CI's `make test` decides.
- Skipped: `make e2e-tw-upgrade` (the disk is full; tw upgrade passes
  `--force` to install.sh, and `tw down`'s stop path is only split into
  functions), `make npm-cli-test` (npm packaging untouched).
