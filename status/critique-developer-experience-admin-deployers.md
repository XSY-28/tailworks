Last update: 2026-11-14 PT. Merged remote `twd logs` (status/remote-logs.md); hostd's GET /v1/logs now admits only tag:twd-mgmt over the tailnet.

Earlier: DX pass: strict twd.jsonc + schema, release workflow + get.sh, app-named tags, policy-doc fix.

# Developer-experience pass (branch `critique-developer-experience-admin-deployers`)

Started from a DX review of the admin path (install, `twd up`) and the
worker-developer path (`twd init`, `twd dev`, `twd deploy`). This branch has
the fixes that don't need new API surface. Remote `twd logs` and Worker
secrets are separate workstreams, each with its own status file.

## What's built

- **twd.jsonc is validated (`schema/`).** `schema/twd.schema.json` is the one
  list of twd.jsonc keys. The Go structs that read twd.jsonc (mgmt,
  hostd/bundle, placement, containers) each decode only part of it, so
  before this a typo was silently ignored. `schema.CheckTwd` implements the
  subset of JSON Schema the file uses and suggests the nearest key.
  `loadApp` (`twd dev`, `twd deploy`) refuses unknown keys or bad values:
  ```
  error [bad_request]: twd.jsonc has 2 problems
    tailnet.objcts: unknown key (did you mean "objects"?)
    tailnett_typo_ok: unknown key
  ```
  Before, `"objcts"` deployed every class as a plain DO with no node. Only
  the CLI enforces this; mgmt and hostd stay lenient, so bundles already
  stored keep loading.
- **`docs/twd-jsonc.md`:** a reference for every key. Templates now carry a
  `"$schema"` line for editor completion.
- **`twd init` names tags after the app.** `tag:twd-wl-<workload>` and
  `tag:twd-<workload>-<class>`; only the template's `tag:twd-…` tags are
  renamed. Before, two apps made from one template shared tags, and so
  shared grants. `--keep-tags` keeps the old names. The e2e suites and CLI
  tests pass it, because their fixed test policies own the template tags.
- **Bug fix:** `mgmt.ParseTwdConfig` blanked out the caller's bytes
  (`hujson.Standardize` works in place), so the `twd.jsonc` shipped in a
  bundle lost its comments. It now parses a copy.
- **Policy docs fixed.** INSTALL.md had `tag:twd-issuer` in the OAuth
  snippet; it belongs in the WIF one (`keylessSnippet`). The new
  `TestDocPolicySnippetsMatchCode` compares the three snippets in README.md
  and INSTALL.md with `operatorSnippet` / `keylessSnippet`.
- **Releases.**
  - `.github/workflows/release.yml`: on a `v*` tag (which must match
    `VERSION`), runs `make release` on macOS, smoke-tests the darwin
    tarball, and uploads the tarballs plus `SHA256SUMS` to a **draft**
    release.
  - The linux tarballs are built with `RELEASE_VM_ASSETS=0`, so they have
    no microVM guest. INSTALL.md says so.
  - `scripts/get.sh` downloads a release, verifies its hash and runs
    `install.sh`.
  - README step 1 now downloads a release; building from source is the
    fallback. README recommends all four OAuth scopes up front.

## Test results

- `make lint`: ok.
- `make test`: ok after the `--keep-tags` test fix. That includes
  `go test ./schema/`, which checks every `twd.jsonc` in the repo against
  the schema (12 files), and `TestAppTags` / `TestClassLabel` /
  `TestDocPolicySnippetsMatchCode` in `cmd/twd`.
- `make test-js`: ok.
- `scripts/get.sh` against a local HTTP mirror (`TWD_RELEASE_BASE`) and a
  real darwin tarball: installs into `TWD_HOME`; a wrong `SHA256SUMS`
  entry is refused with `checksum mismatch`.
- Not run: the release workflow itself (it needs a pushed tag on GitHub),
  and the e2e suites against a real tailnet.

## Remote logs: caller check on hostd

The push API (tcp:7474 on the host's node) relied only on the policy grant
`tag:twd-mgmt → tag:twd-host`. Logs are tenant data: with a broad grant
(for example the default allow-all policy), any node could read any
workload's log and skip mgmt's admin check. `GET /v1/logs` now asks WhoIs
(`hostd.Config.PushCallerTags`, `cmd/twd-hostd/pushcaller.go`) and answers
TCP callers only if they carry `tag:twd-mgmt`. A failed WhoIs is refused,
and the unix socket is unchanged. Test: `TestLogsHandlerCallerMustBeMgmt`.
After the merge, `make lint test` passes; `TestCLILink` ran here (not
skipped) and covers `twd logs` from a linked machine.

Kills, `/v1/status` and the other push routes still have no such check.
Extending it to the whole push API needs the e2e suites run against a
real tailnet first.

## Gaps and next steps

- No release is published yet, so `get.sh` reports "no published
  release" until a maintainer pushes a `v*` tag and publishes the draft.
- The schema has no `maximum`. mgmt still enforces limits such as
  `replicas` ≤ 16.
