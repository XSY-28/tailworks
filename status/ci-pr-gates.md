Last update: 2026-11-15 PT. PR CI, SHA-pinned actions, release fix on GitHub's macOS runner.

# CI on PRs and the merge queue

## What's built

- `.github/workflows/ci.yml` runs on `pull_request`, `merge_group`, pushes
  to master and by hand. Jobs:
  - `lint`: `make lint`, with full history so `check-changes` can find the
    merge base.
  - `test`: `make test`.
  - `ci-ok`: needs both, and is the only check branch protection
    requires.
- Every action in `.github/workflows/` is pinned by commit SHA, with a
  `# vX.Y.Z` comment. All moved to their current Node 24 majors (checkout
  v7, setup-go v7, setup-node v7, cache v6, upload-artifact v7,
  download-artifact v8). This removes the Node 20 deprecation warning.
  `.github/dependabot.yml` keeps the pins current in one weekly PR.
- `release.yml`: the workflow default is now `contents: read`. Only the
  `release` job gets `contents: write`.
- `scripts/release.sh`: `twd-vz -check` now warns instead of failing when
  the machine can't run VMs at all ("not available on this hardware",
  VZErrorDomain Code=2). That error is what failed run 43679857487 on
  GitHub's macos-15 runner, which has no nested virtualization. Any other
  failure of the check still fails the release. The entitlement check
  before it is unchanged.

## Repo settings (outside git)

- master needs PRs and the `ci-ok` check. Details are in the PR
  description.

## Gaps

- The release fix has not run on a real tag yet. The next tag, or
  `gh workflow run release.yml -f tag=<tag>`, is the test.
- Go and npm Dependabot updates, govulncheck, and secret scanning in CI are
  tracked in a separate issue.
