#!/bin/sh
# npm-publish.sh <version> [--dry-run]: publish tailworks' npm packages
# (@tailworks/tailnet, @tailworks/node, @tailworks/mcp, create-tailworks) at <version>, from a
# checkout of the release tag (ADR-041 §9; docs/RELEASING.md "Publish npm").
#
#   - Checks every package and template is at <version> first
#     (scripts/npm-versions.mjs), so a release never publishes a mix.
#   - Builds each package (its prepack runs tsc for dist/ and the .d.ts).
#   - Publishes in dependency order (@tailworks/mcp depends on @tailworks/tailnet), and
#     skips a package already on the registry at <version>, so a re-run after
#     a partial failure finishes the job.
#   - A prerelease (0.3.0-rc.1) goes to the "next" dist-tag, not "latest".
#
# Auth is npm's own. In CI it is trusted publishing: npm (>= 11.5.1)
# exchanges the GitHub Actions OIDC token for a publish credential and
# attaches provenance itself (.github/workflows/npm-publish.yml). By hand it
# is your `npm login`. --dry-run packs and validates without publishing
# (no auth).
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

V=${1:-}
[ -n "$V" ] || { echo "usage: scripts/npm-publish.sh <version> [--dry-run]" >&2; exit 2; }
V=${V#v}
shift
DRY=""
[ "${1:-}" = "--dry-run" ] && DRY="--dry-run"

node scripts/npm-versions.mjs "$V"

TAG=latest
case "$V" in *-*) TAG=next ;; esac

# Workspace directory → package name, in publish order.
for ws in tailnet node mcp create-tailworks; do
	name=$(node -p "require('./js/packages/$ws/package.json').name")
	if [ -z "$DRY" ] && [ "$(npm view "$name@$V" version 2>/dev/null || true)" = "$V" ]; then
		echo "npm-publish: $name@$V is already published; skipping"
		continue
	fi
	echo "npm-publish: $name@$V (dist-tag $TAG)${DRY:+ (dry run)}"
	npm publish -w "js/packages/$ws" --access public --tag "$TAG" $DRY
done
