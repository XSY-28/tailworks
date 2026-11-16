#!/bin/sh
# npm-publish.sh <version> [--dry-run]: publish twd's npm packages
# (@twd/tailnet, @twd/node, @twd/mcp, create-twd) at <version>, from a
# checkout of the release tag (ADR-041 §9; docs/RELEASING.md "Publish npm").
#
#   - Checks every package and template is at <version> first
#     (scripts/npm-versions.mjs), so a release never publishes a mix.
#   - Builds each package (its prepack runs tsc for dist/ and the .d.ts).
#   - Publishes in dependency order (@twd/mcp depends on @twd/tailnet), and
#     skips a package already on the registry at <version>, so a re-run after
#     a partial failure finishes the job.
#   - A prerelease (0.3.0-rc.1) goes to the "next" dist-tag, not "latest".
#
# Auth is npm's own: NODE_AUTH_TOKEN with the .npmrc that
# actions/setup-node writes (.github/workflows/npm-publish.yml), or your
# `npm login`. --dry-run packs and validates without publishing (no auth).
# Set NPM_CONFIG_PROVENANCE=true in CI for provenance attestations.
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
for ws in tailnet node mcp create-twd; do
	name=$(node -p "require('./js/packages/$ws/package.json').name")
	if [ -z "$DRY" ] && [ "$(npm view "$name@$V" version 2>/dev/null || true)" = "$V" ]; then
		echo "npm-publish: $name@$V is already published; skipping"
		continue
	fi
	echo "npm-publish: $name@$V (dist-tag $TAG)${DRY:+ (dry run)}"
	npm publish -w "js/packages/$ws" --access public --tag "$TAG" $DRY
done
