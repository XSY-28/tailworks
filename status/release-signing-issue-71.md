Last update: 2026-11-15 16:30 PT. Release signing (#71): mechanism, verification in get.sh, install.sh and hostd self-upgrade; key not yet made.

# Release signing (issue #71)

## What's built

- **Format:** SSH signatures (`ssh-keygen -Y sign -n twd-release`, Ed25519)
  over `SHA256SUMS`. Picked over minisign (not installed anywhere by
  default) and Sigstore or attestations (need the network at verify time,
  which rules out air-gapped hosts). `ssh-keygen` ships with OpenSSH on
  macOS and on every supported Linux.
- **`internal/releasesig`:** the pinned keys (`allowed_signers`, embedded),
  a pure-Go verifier for the sshsig format (Ed25519 only, sha512/sha256),
  `VerifyTree` (signature, every listed hash, no unlisted regular file), and
  a `Sign` for tests.
- **`scripts/release.sh`:** with `TWD_RELEASE_SIGNING_KEY` it signs each
  tarball's inner `SHA256SUMS` and `dist/SHA256SUMS`, then checks both
  against the pinned keys. `TWD_RELEASE_SIGN=1` makes the key required.
- **`release.yml`:** the release job runs in the `release` environment,
  writes the `TWD_RELEASE_SIGNING_KEY` secret to a temp file (removed on
  exit), signs, verifies the outer and the darwin inner signature with
  `ssh-keygen`, and uploads `SHA256SUMS.sig`. No secret, no draft.
- **`scripts/get.sh`:** checks `SHA256SUMS.sig` before it trusts the hashes.
  A missing signature is refused for v0.3.0 and later. Earlier releases get
  the checksum only, with a warning. `TWD_ALLOW_UNSIGNED=1` opts out.
- **`scripts/install.sh`:** checks the inner `SHA256SUMS.sig`. A missing
  signature is allowed only for local versions (`x.y.z-…`), with a warning.
  `--allow-unsigned` (or `TWD_ALLOW_UNSIGNED=1`) opts out. With no
  `ssh-keygen` it warns and goes on. That covers hosts that self-upgrade
  from a pre-#71 hostd, and install.sh is never the trust anchor anyway,
  since a tampered tarball ships its own install.sh.
- **twd-hostd self-upgrade:** `hostd/upgradesig.go` runs `VerifyTree` against
  the keys built into the binary after extracting, before `install.sh`.
  `upgrader.signers` is a test-only override, never set from mgmt.
- **`scripts/release-keygen.sh`:** makes the key outside the repo, pins it,
  and `--sync` copies the keys into get.sh and install.sh.
  `TestScriptsPinTheSameKeys` fails if the copies drift.
- **Docs:** RELEASING.md "Signing" (what is signed, who checks, the key
  ceremony, rotation, a leaked key), INSTALL.md "Verify a release".

## Tests

- `go test ./internal/releasesig/`: sign and verify, tampering, unpinned
  key, allowed_signers parsing, interop both ways with the real
  `ssh-keygen`, VerifyTree (changed, unlisted, missing file, re-signed or
  edited sums, escaping path), and that the scripts pin the same keys.
- `go test -run SelfUpgrade ./hostd/`: a pushed release that is unsigned,
  signed by another key, tampered with after signing, or carries an extra
  file never reaches install.sh. A test key is not trusted without the
  override.
- `make install-test`: the old upgrade and rollback cases, now on signed
  fake releases. Also: install.sh refuses an unpinned key's signature, a
  garbled signature, a changed binary with a re-made SHA256SUMS, an
  unsigned release, and no pinned key. It accepts `--allow-unsigned` and an
  unsigned local build, each with a warning. get.sh (from a `file://`
  mirror) installs a signed release, refuses a bad signature and a missing
  one, and installs a pre-v0.3.0 release on its checksum.
  Mutation check: with `TWD_ALLOW_UNSIGNED=1` exported the test fails
  (`FAIL: install.sh installed a release signed by an unpinned key`).
- `make lint`: ok. `make test`: see the PR.
- `make e2e-upgrade`: doesn't use self-upgrade or install.sh, so this branch
  doesn't affect it. See the PR for the run.

## Open (needs the maintainer)

- **Key custody:** no key is pinned yet, so `allowed_signers` is empty. Until
  `sh scripts/release-keygen.sh <dir outside the repo>` runs and the
  `TWD_RELEASE_SIGNING_KEY` secret is set on the `release` environment, a
  tag fails at the release job, and get.sh refuses v0.3.0 and later.
  Decide who holds the key and its backup, and whether the `release`
  environment needs required reviewers.
- **Tool choice:** SSH signatures is the default here. GitHub build
  provenance (`actions/attest-build-provenance`) could be added alongside
  it later.
- **`twd upgrade` (single machine):** it relies on install.sh's check. A Go
  check like hostd's would also cover machines without ssh-keygen.
