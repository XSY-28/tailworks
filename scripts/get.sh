#!/bin/sh
# Download a published twd release from GitHub, verify it, and install it.
#
#   sh get.sh                    the latest release, into ~/.twd
#   sh get.sh v0.1.0             a given release
#   TWD_HOME=DIR sh get.sh       install into DIR
#   TWD_RELEASE_BASE=URL sh get.sh v0.1.0   download from a mirror instead of GitHub
#
# It downloads twd-<version>-<os>-<arch>.tar.gz and SHA256SUMS, checks the
# tarball's hash, extracts it into a fresh temporary directory, and runs
# its install.sh (no sudo; touches nothing outside ~/.twd, your shell
# profile included). Read it first: it's short.
set -eu
REPO=${TWD_REPO:-rosskukulinski/twd}
TAG=${1:-latest}

die() { printf 'get.sh: %s\n' "$*" >&2; exit 1; }
[ "$(id -u)" != 0 ] || die "don't run as root (twd installs per user and needs no sudo)"
command -v curl >/dev/null || die "needs curl"
command -v tar >/dev/null || die "needs tar"

case $(uname -s) in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) die "unsupported OS $(uname -s) (twd runs on macOS and Linux)" ;;
esac
case $(uname -m) in
arm64 | aarch64) arch=arm64 ;;
x86_64 | amd64) arch=amd64 ;;
*) die "unsupported CPU $(uname -m) (arm64 or amd64)" ;;
esac
[ "$os-$arch" != darwin-amd64 ] || die "twd on macOS needs Apple silicon (arm64)"

if [ "$TAG" = latest ]; then
	TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
		sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
	[ -n "$TAG" ] || die "no published release found in $REPO (build one: see docs/INSTALL.md)"
fi
VERSION=${TAG#v}
NAME=twd-$VERSION-$os-$arch.tar.gz
BASE=${TWD_RELEASE_BASE:-https://github.com/$REPO/releases/download/$TAG}

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
printf 'downloading %s (%s)…\n' "$NAME" "$TAG"
curl -fsSL -o "$TMP/$NAME" "$BASE/$NAME" || die "no $NAME in release $TAG"
curl -fsSL -o "$TMP/SHA256SUMS" "$BASE/SHA256SUMS" || die "no SHA256SUMS in release $TAG"

want=$(awk -v n="$NAME" '$2 == n || $2 == "*"n { print $1 }' "$TMP/SHA256SUMS")
[ -n "$want" ] || die "SHA256SUMS has no entry for $NAME"
if command -v sha256sum >/dev/null; then
	got=$(sha256sum "$TMP/$NAME" | awk '{print $1}')
else
	got=$(shasum -a 256 "$TMP/$NAME" | awk '{print $1}')
fi
[ "$got" = "$want" ] || die "checksum mismatch for $NAME (got $got, want $want)"

mkdir "$TMP/x"
tar -xzf "$TMP/$NAME" -C "$TMP/x"
set -- "$TMP"/x/twd-*/install.sh
[ -f "$1" ] || die "the tarball has no install.sh"
sh "$1"
