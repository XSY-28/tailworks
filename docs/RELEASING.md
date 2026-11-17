# Releasing tailworks

This is the maintainer process. The design behind it is
`docs/design/adr/041-upgrades-and-releases.md`.

## Versions

- **One version for everything.** Every binary, the microVM guest, the
  embedded platform runtime, the admin UI, the create-tailworks templates and the
  `@tailworks/*` npm packages share the version in `VERSION`.
- **Before 1.0** versions are `0.MINOR.PATCH`:
  - A minor may break things. A breaking change needs a migration path and
    a `breaking` fragment.
  - A patch fixes bugs only. It has no mgmt migration, no format change, no
    workerd bump (unless it is a security fix), no new feature string and
    no protocol change.
- **From 1.0** tailworks follows standard SemVer. Config and API stay stable within
  a major.
- **Skew.** A release line is a minor before 1.0 and a major after it. Hosts
  may run one line behind mgmt, never ahead (ADR §7).

## Branches

- master is always releasable.
- **A minor release** is a tag on master, after the `VERSION` bump.
- **A patch release** comes from `release-0.N`. Create that branch from the
  minor's tag when the first patch is needed, then cherry-pick fixes from
  master into it.
- **Support.** Only the latest minor gets patches. Security fixes also go to
  the minor before it.

## Release notes

Every branch adds `changes/<branch>.md` (format: `changes/README.md`).
`make lint` fails when a branch touches an upgrade-relevant path and no
fragment names the matching upgrade kind. Upgrade-relevant paths include
migrations, `api/openapi.yaml`, `schema/cli.schema.json`, the workerd pin,
guest inputs, format fixtures and protocol constants.

Preview the notes at any time:

```sh
scripts/release-notes.sh v0.1.0 v0.2.0
```

## Cutting a release

1. **Merge.** Merge everything intended for the release, then check out
   master, or `release-0.N` for a patch.
2. **Finalize the notes.**

   ```sh
   scripts/release-notes.sh --finalize v0.1.0 v0.2.0
   ```

   This moves the pending fragments to `changes/released/v0.2.0/`. It also
   writes `internal/catalog/releases/v0.2.0.json`, the catalog entry that
   tw-mgmt embeds for offline upgrade plans. The entry records the notes,
   the workerd pin, the newest migration, contract migrations and protocol
   versions. Last, it re-renders the user-facing
   [CHANGELOG.md](../CHANGELOG.md) from the catalog
   (`go run ./internal/catalog/changes changelog`; a test fails when it is
   stale).
3. **Bump `VERSION`**, and the `@tailworks/*` and `create-tailworks` package versions,
   to `0.2.0`. Commit all of it as `Release v0.2.0`.

   ```sh
   echo 0.2.0 >VERSION && node scripts/npm-versions.mjs --set 0.2.0
   ```

   `npm-versions.mjs --set` moves every workspace package, their
   dependencies on each other and the templates' `^0.2.0` ranges together.
   `make test` (`TestNPMVersions`) fails if any of them differs from
   `VERSION`.
4. **Check, tag and push.** After the release commit is on master (or
   `release-0.N`), from a checkout of it:

   ```sh
   sh scripts/release-check.sh v0.2.0
   git tag -a v0.2.0 -m "tw v0.2.0" && git push origin v0.2.0
   ```

   `release-check.sh` fails if the tag doesn't match `VERSION` or the npm
   versions, if the notes aren't finalized for the tag or a fragment is
   still pending in `changes/`, or if no signing key is pinned. The
   `gates` job runs it again on the tag.
5. **Watch `.github/workflows/release.yml`.** It runs:
   - `gates`: `scripts/release-check.sh`, `make lint test`, the npm
     publish dry run, the install.sh rollback test, and
     `make e2e-upgrade`, which upgrades from the newest published release
     to this commit and checks data, rejoin and old bundles. While no
     release is published it upgrades from this tree stamped an older
     version (`scripts/upgrade-from.sh`);
   - `headscale`: `make e2e-headscale` against each supported Headscale
     minor;
   - `vm-assets` and `vz-assets`: the microVM guests for each arch;
   - `release`: the three tarballs with `SHA256SUMS`, `SHA256SUMS.sig`
     (see [Signing](#signing)) and the catalog, and a **draft** GitHub
     release whose body comes from `release-notes.sh`.

   A failed gate means no draft.
6. **Review the draft.**
   - Check that the notes read well, and that the Breaking and Upgrade
     actions sections are complete.
   - Check the asset list: `tailworks-0.2.0-{darwin-arm64,linux-arm64,linux-amd64}.tar.gz`,
     `SHA256SUMS` and `SHA256SUMS.sig`. `get.sh` refuses a release without
     the signature.
   - Check that private vulnerability reporting is still on, since
     [SECURITY.md](../SECURITY.md) sends reporters there:
     `gh api repos/rosskukulinski/tailworks/private-vulnerability-reporting`
     must print `{"enabled":true}`.
   - Install from the draft's assets (a draft's download URLs aren't
     public, so fetch them first):

     ```sh
     gh release download v0.2.0 -D /tmp/tw-v0.2.0
     TW_HOME=/tmp/tw-home TW_RELEASE_BASE=file:///tmp/tw-v0.2.0 sh scripts/get.sh v0.2.0
     ```

7. **Publish** the draft. `scripts/get.sh` and `tw upgrade` install the
   latest published release.

   ```sh
   gh release edit v0.2.0 --draft=false --latest
   gh api repos/rosskukulinski/tailworks/releases/latest --jq .tag_name   # v0.2.0
   ```

   Then smoke it as a newcomer would, on a clean Mac (Apple silicon) and a
   clean Linux machine:
   `curl -fsSLO https://raw.githubusercontent.com/rosskukulinski/tailworks/master/scripts/get.sh && sh get.sh && ~/.tailworks/bin/tw doctor`.
8. **Publish npm.** Publishing the GitHub release starts
   `.github/workflows/npm-publish.yml`. It runs `scripts/npm-publish.sh`
   from the tag, which publishes `@tailworks/tailnet`, `@tailworks/node`, `@tailworks/mcp`
   and `create-tailworks` at the release's version (a prerelease goes to the
   `next` dist-tag), with provenance.
   - It needs the repository secret `NPM_TOKEN`, a token that may publish
     the `@tailworks` scope and `create-tailworks`. Without it the job only logs a
     notice.
   - Packages already on npm at that version are skipped, so after a partial
     failure re-run it: `gh workflow run npm-publish.yml -f tag=v0.2.0`.
   - By hand, from a checkout of the tag after `npm ci` and `npm login`:
     `sh scripts/npm-publish.sh 0.2.0` (`--dry-run` to check first;
     release.yml's gates already ran one).
   - Before the first publish, check the templates against the packed
     packages: `make templates-typecheck` (every template's
     `tw init && npm install && npx tsc --noEmit`, with `@tailworks/*` from
     `npm pack` tarballs).
   - One-time setup, before the first publish:
     1. On npmjs.com, create the `tailworks` organization (that owns the
        `@tailworks` scope) and check that the unscoped name
        `create-tailworks` is still free.
     2. Create a granular access token that can publish `@tailworks/*` and
        `create-tailworks` (an automation token also works), and add it to
        the repository as the `NPM_TOKEN` secret:
        `gh secret set NPM_TOKEN`.
     3. Cut the next release as above; publishing it runs the workflow.
        Only tags made after the rename to tailworks carry the
        `@tailworks/*` names, so don't publish an older tag. For a release
        already published before the secret existed:
        `gh workflow run npm-publish.yml -f tag=v<VERSION>`. Then check
        `npm view @tailworks/tailnet version` and that
        `npm create tailworks@latest x && cd x && npm install && npx tsc --noEmit`
        passes on a machine without this checkout.
     4. Remove the "until the first release that publishes them" notes from
        `docs/wrangler.md` and `docs/testing.md`.

## Signing

Every release is signed (issue #71). A checksum only proves a download is
intact; the signature proves the release came from this repository's
release workflow.

- **What is signed.** `release.yml` signs the release's `SHA256SUMS` and
  each tarball's inner `SHA256SUMS`. Each gets a `SHA256SUMS.sig` next to
  it. The signature is an SSH signature (`ssh-keygen -Y sign -n
  tw-release`) by an Ed25519 key.
- **Who checks it.**
  - `scripts/get.sh` checks the release's `SHA256SUMS.sig` before it trusts
    the hashes in `SHA256SUMS`. It refuses an unsigned release whatever its
    version: an exception for "old" releases would let whoever can publish
    a release pick a tag that skips the check. (No unsigned release was
    ever published.)
  - `install.sh` checks the tarball's `SHA256SUMS.sig` before it installs.
    It is only a second check: a tampered tarball ships its own
    `install.sh`, which is why `get.sh` and hostd check first.
    A build whose version has a `-` suffix (what `make release` stamps on a
    local build) may be unsigned.
  - tw-hostd's self-upgrade checks the tarball's signature in Go, against
    the keys built into the running binary, before it runs `install.sh`.
    The keys are never taken from tw-mgmt or the release.
- **Where the keys are pinned.** `internal/releasesig/allowed_signers` is
  the source of truth. `scripts/get.sh` and `scripts/install.sh` carry
  copies, and `make test` fails if they differ.
- **Opting out.** `install.sh --allow-unsigned`, or `TW_ALLOW_UNSIGNED=1`
  for `get.sh` and `install.sh`. Use it for builds you made yourself.

### The key ceremony

Do this once, and again to rotate the key.

1. Make the key outside the repository, on an encrypted volume:

   ```sh
   sh scripts/release-keygen.sh /Volumes/keys/tw-release
   ```

   This writes the key pair, adds the public key to
   `internal/releasesig/allowed_signers`, and copies it into `get.sh` and
   `install.sh` (`--sync`).
2. Store the private key as the `release` environment's secret:
   `gh secret set TW_RELEASE_SIGNING_KEY --env release <key-file>`.
   Keep an offline backup.
3. Commit the three changed files and merge them before you tag.
   `release.yml` refuses to draft a release without the secret, or with a
   key that isn't pinned.

**Rotating.** Add the new key first and keep the old line, so binaries
already installed still trust the next release. Remove the old line a
release later. **If the key leaks:** remove its line, add a new key and
ship a patch. Hosts on older releases still trust the leaked key until they
upgrade, so tell operators to upgrade with `get.sh` from master.

## If a release is bad

- **Before publishing:** delete the draft and the tag, fix the problem on
  master, then release again. If more changes landed meanwhile, re-finalize
  the notes (below) so they are in this release.
- **After publishing:** ship a patch. Never move a published tag.
- **For users:** `tw upgrade` rolls back on a failed health check. To roll
  back by hand, run `~/.tailworks/install.sh --rollback` and restore the mgmt
  backup that `tw upgrade` took. Contract migrations can't be undone by a
  rollback, which is why they ship a release after their expand step.

## Re-finalizing an unpublished release

Finalized notes are for one commit. If changes land after you finalize a
version but before it is published (or its draft is deleted), those
fragments are pending again: `release-check.sh` fails, and if you tagged
anyway they would be missing from this release's notes and catalog entry
and turn up in the next release's. Fold them in, for a version that was
never published only:

```sh
git mv changes/released/v0.2.0/*.md changes/ && rmdir changes/released/v0.2.0
git rm internal/catalog/releases/v0.2.0.json
scripts/release-notes.sh --finalize v0.1.0 v0.2.0
git commit -m "Release v0.2.0 (re-finalized)"
```

`--finalize` writes the entry again with today's date and re-renders
CHANGELOG.md. A published release's notes never change: ship the
changes in the next version.
