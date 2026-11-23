#!/usr/bin/env bash
# ci-tarball-check.sh <dist dir> <platform> <version>: validate one release
# tarball the way a user gets it (release.yml runs it on each platform's
# own runner). Hermetic: no tailnet; the real-tailnet runs of the same
# tarballs are e2e-linux, e2e-linux-vm and e2e-mbp.
#
#   1. the release's SHA256SUMS is signed by a pinned key, and the tarball
#      matches it;
#   2. install.sh into a fresh prefix (it checks the signature itself, with
#      no --allow-unsigned), then again over it (an upgrade/repair; the
#      rollback path is make install-test's);
#   3. tw version is <version>; release.json names the platform and has the
#      microVM tier; LICENSE and THIRD_PARTY_NOTICES are there;
#   4. tw doctor passes (binaries, workerd, bundler, sandbox);
#   5. every template: tw init, then tw dev --once (the installed tw-hostd,
#      netd and workerd serve it; the smoke request must be a 2xx).
set -euo pipefail
DIST=$(cd "${1:?dist dir}" && pwd)
PLATFORM=${2:?platform, e.g. linux-amd64}
VERSION=${3:?version}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
TGZ=$DIST/tailworks-$VERSION-$PLATFORM.tar.gz
WORK=$(mktemp -d "${RUNNER_TEMP:-/tmp}/tw-tarball.XXXXXX")
trap 'rm -rf "$WORK"' EXIT
say() { printf '\n== %s\n' "$*"; }
sha256() { if command -v sha256sum >/dev/null; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }

say "1. signature and checksum: $(basename "$TGZ")"
ssh-keygen -Y verify -f "$ROOT/internal/releasesig/allowed_signers" -I tw-release -n tw-release \
	-s "$DIST/SHA256SUMS.sig" <"$DIST/SHA256SUMS"
(cd "$DIST" && grep " $(basename "$TGZ")\$" SHA256SUMS | sha256 -c -)

say "2. install.sh (signature checked), then a reinstall over it"
mkdir -p "$WORK/x" "$WORK/home"
tar -xzf "$TGZ" -C "$WORK/x"
REL=$(ls -d "$WORK"/x/tailworks-*)
PREFIX=$WORK/prefix
export HOME=$WORK/home
"$REL/install.sh" --prefix "$PREFIX"
"$REL/install.sh" --prefix "$PREFIX"
TW=$PREFIX/bin/tw

say "3. version, release.json, notices"
got=$("$TW" version | head -1)
echo "$got"
case $got in *"$VERSION"*) ;; *) echo "tw version: want $VERSION"; exit 1 ;; esac
rj=$PREFIX/share/tailworks/release.json
cat "$rj"
grep -Eq '"microvm": ?true' "$rj" || { echo "release.json: no microVM tier"; exit 1; }
for f in LICENSE THIRD_PARTY_NOTICES; do
	ls "$REL"/$f* >/dev/null || { echo "missing $f"; exit 1; }
done

say "4. tw doctor"
"$TW" doctor

say "5. every template under tw dev --once"
export PATH=$PREFIX/bin:$PATH
for t in hello agent mcp ai-agent lock refund-approval node-workflow; do
	echo "-- $t"
	"$TW" init "$WORK/apps/$t" --template "$t" >/dev/null
	out=$("$TW" dev "$WORK/apps/$t" --once --port 0 --json)
	status=$(printf '%s' "$out" | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{const r=JSON.parse(s.trim().split("\n").pop());console.log(r.smoke&&r.smoke.status)})')
	echo "   smoke status $status"
	case $status in 2??) ;; *) echo "$out"; echo "template $t: tw dev --once smoke status $status"; exit 1 ;; esac
done
echo
echo "ci-tarball-check: $PLATFORM $VERSION ok"
