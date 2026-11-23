#!/bin/sh
# ci-lima-make.sh [-c hostdir:vmdir]… <make args…>: run `make <args>` inside
# the Lima dev VM ($TW_DEV_VM, default tw-dev) on a copy of this tree. For
# release.yml's self-hosted Mac runner: the linux-arm64 Firecracker tier
# (e2e-vm, e2e-container) needs Linux + KVM, which on Apple silicon is a
# nested-virt Lima VM.
#
#   -c hostdir:vmdir  also copy hostdir into the VM at vmdir (e.g. the
#                     shipped vm-assets, or the sandbox images); repeatable.
#   -p 'shell cmd'    run this in the tree's copy (bash) before make, e.g. to
#                     install the shipped firecracker + jailer; repeatable.
#
# The tree goes to /tmp/tw-ci-<run> in the VM (git ls-files, as
# scripts/vm-assets-lima.sh does) and is removed afterwards. The VM is
# started if it is stopped, and stopped again at the end only if this
# script started it.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
DEV=${TW_DEV_VM:-tw-dev}
cd "$ROOT"
copies=""
pre=""
while [ $# -gt 0 ]; do
	case $1 in
	-c) copies="$copies $2"; shift 2 ;;
	-p) pre="$pre $2;"; shift 2 ;;
	*) break ;;
	esac
done
[ $# -gt 0 ] || { echo "usage: ci-lima-make.sh [-c hostdir:vmdir]… <make args…>" >&2; exit 2; }
command -v limactl >/dev/null || { echo "ci-lima-make: no limactl" >&2; exit 2; }

started=0
if [ "$(limactl list -f '{{.Status}}' "$DEV" 2>/dev/null)" != Running ]; then
	echo "ci-lima-make: starting $DEV"
	limactl start --tty=false "$DEV"
	started=1
fi
dir=/tmp/tw-ci-${GITHUB_RUN_ID:-local}-$$
cleanup() {
	limactl shell "$DEV" -- sudo rm -rf "$dir" >/dev/null 2>&1 || true
	[ "$started" = 0 ] || limactl stop "$DEV" >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 130' INT TERM

limactl shell "$DEV" -- mkdir -p "$dir"
git ls-files -co --exclude-standard | tar cf - -T - | limactl shell "$DEV" -- tar xf - -C "$dir"
for c in $copies; do
	src=${c%%:*} dst=${c#*:}
	case $dst in /*) ;; *) dst=$dir/$dst ;; esac
	echo "ci-lima-make: copying $src → $DEV:$dst"
	limactl shell "$DEV" -- mkdir -p "$dst"
	tar cf - -C "$src" . | limactl shell "$DEV" -- tar xf - -C "$dst"
done
# Quote each make argument for the remote shell.
args=""
for a in "$@"; do
	args="$args '$(printf '%s' "$a" | sed "s/'/'\\\\''/g")'"
done
echo "ci-lima-make: $DEV: make$args"
limactl shell "$DEV" -- bash -lc "set -e; cd '$dir'; $pre make$args"
