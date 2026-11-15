#!/bin/sh
# install_test.sh: install.sh upgrade keeps .prev, --rollback swaps it back,
# a same-version repair keeps the older .prev, --uninstall removes it all.
# Hermetic (fake releases in a temp dir); run: sh scripts/install_test.sh
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
[ "$arch" = aarch64 ] && arch=arm64
[ "$arch" = x86_64 ] && arch=amd64
sha() { if command -v sha256sum >/dev/null; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }

mkrel() { # version
	d=$T/rel-$1
	mkdir -p "$d/bin" "$d/share/twd"
	printf '#!/bin/sh\necho twd %s\n' "$1" >"$d/bin/twd"
	printf '#!/bin/sh\necho workerd\n' >"$d/bin/workerd"
	chmod 755 "$d/bin/"*
	printf '%s\n' "$1" >"$d/VERSION"
	printf '{"version":"%s","os":"%s","arch":"%s"}\n' "$1" "$os" "$arch" >"$d/share/twd/release.json"
	cp "$ROOT/scripts/install.sh" "$d/install.sh"
	(cd "$d" && find . -type f ! -name SHA256SUMS | sed 's|^\./||' | while read -r f; do sha "$f"; done >SHA256SUMS)
}
fail() { echo "FAIL: $*" >&2; exit 1; }
P=$T/prefix
export TWD_HOME=$P

mkrel 0.1.0
mkrel 0.2.0
sh "$T/rel-0.1.0/install.sh" >/dev/null
[ "$(cat "$P/VERSION")" = 0.1.0 ] || fail "first install"
[ ! -e "$P/bin.prev" ] || fail "first install made a .prev"
[ -x "$P/install.sh" ] || fail "install.sh not copied to the prefix"

sh "$T/rel-0.2.0/install.sh" >/dev/null
[ "$(cat "$P/VERSION")" = 0.2.0 ] || fail "upgrade"
[ "$(cat "$P/VERSION.prev")" = 0.1.0 ] || fail "VERSION.prev after upgrade"
[ "$("$P/bin.prev/twd")" = "twd 0.1.0" ] || fail "bin.prev is not 0.1.0"

sh "$T/rel-0.2.0/install.sh" >/dev/null # repair: keeps 0.1.0 as .prev
[ "$(cat "$P/VERSION.prev")" = 0.1.0 ] || fail "repair replaced .prev"

sh "$P/install.sh" --rollback >/dev/null
[ "$(cat "$P/VERSION")" = 0.1.0 ] || fail "rollback VERSION"
[ "$("$P/bin/twd")" = "twd 0.1.0" ] || fail "rollback bin"
[ "$(cat "$P/VERSION.prev")" = 0.2.0 ] || fail "rollback did not keep 0.2.0 as .prev"
sh "$P/install.sh" --rollback >/dev/null
[ "$("$P/bin/twd")" = "twd 0.2.0" ] || fail "second rollback did not return"

# Refuses while running (a live supervisor.pid), unless --force.
sleep 30 &
pid=$!
echo $pid >"$P/supervisor.pid"
if sh "$P/install.sh" --rollback >/dev/null 2>&1; then fail "rollback while running"; fi
sh "$P/install.sh" --rollback --force >/dev/null
kill $pid 2>/dev/null || true
wait $pid 2>/dev/null || true
rm -f "$P/supervisor.pid"

sh "$P/install.sh" --uninstall >/dev/null
[ ! -e "$P" ] || fail "uninstall left: $(ls -A "$P")"
if sh "$T/rel-0.1.0/install.sh" --rollback >/dev/null 2>&1; then fail "rollback with no .prev"; fi
echo "install_test: ok"
