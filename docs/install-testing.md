# Where the install is tested, and what was measured

[INSTALL.md](INSTALL.md) is the install guide. This page is the evidence
behind it: which machines the install steps ran on, the harness that ran
them, and the timings measured there. The full history is in
[STATUS.md](../STATUS.md) (frozen at 2026-11-14) and [status/](../status/)
(one file per workstream since).

## macOS

The [macOS steps](INSTALL.md#macos) were run as written, on a clean
Apple-silicon Mac with nothing installed (`mbp`, M2 Pro, macOS 26.6), by
`make e2e-mbp`. The measured numbers are in STATUS.md, "M6: release
packaging + E2E-MAC on a different, clean Mac".

## Linux requirements

Linux is a host platform on a par with macOS. These are the requirements
the Linux work was built and tested against; code comments cite them by
number.

1. **Releases:** `linux-arm64` and `linux-amd64` tarballs, both built and
   tested. `install.sh` installs into `~/.tailworks` without root, on any
   supported distro.
2. **E2E-LINUX:** the full e2e flow (checks a–f, plus multi-tenant
   isolation) passes from the installed release on fresh VMs: Ubuntu 24.04
   arm64, Debian 12 arm64, Ubuntu 22.04 arm64, Ubuntu 24.04 amd64 (process
   tier only) and Fedora arm64. The test client and echo run on another
   machine over the test tailnet (`make e2e-linux`; results in the
   [support matrix](#linux-support-matrix)).
3. **Process-tier sandbox:** each tenant's workerd and Node app runs under
   Landlock (unprivileged), plus seccomp where cheap, with the same
   guarantees as the macOS Seatbelt profile and the same cross-tenant
   attack tests. Without Landlock, `tw doctor` warns and multi-tenant use
   is refused unless `--insecure-shared-host` is passed.
4. **microVM tier:** Durable Object storage persists across VM restart and
   redeploy; Firecracker's jailer is used; one-time
   `sudo tw host setup --microvm` is the only privileged step, and without
   it tailworks falls back to the process tier and `tw doctor` explains.
5. **Service management:** `tw up --service` installs a systemd *user* unit
   so tailworks survives logout (with `loginctl enable-linger`) and reboot;
   `tw down --service` removes it.
6. **`tw doctor` on Linux** checks glibc, the kernel and Landlock ABI,
   `/dev/kvm`, the jailer, cgroups v2, systemd, and that the system
   tailscaled is not used, with clear errors for unsupported hosts
   (musl/Alpine, glibc < 2.35, no Landlock).
7. **[INSTALL.md](INSTALL.md#linux)** covers Linux as a peer of macOS, with
   per-distro notes and the measured support matrix below.

## Linux support matrix

`make e2e-linux`, 2026-11-08, release `0.1.0-bb3a3296`. Every VM was **fresh**:
a Lima official template, 4 CPU / 6 GiB / 30 GiB, `--plain --mount-none`, and
nothing mounted from the Mac. The tarball and the API key file (0600) were
copied in with `limactl copy`. Tailworks ran in the VM under `tw up --service`; the
tag:ttest client and echo ran on the Mac (M5 Pro) over tailnet 1. Every row
passed checks (a)–(f), `tw doctor` before and while running,
`systemctl --user restart tw` (healthy again, the same workload IP answering
the Mac), `tw down --purge --service`, and cleanup: zero leftover devices or
keys. **Sandbox: none on every row in L1's run (made before the Landlock sandbox landed).** **microVM
tier: unavailable on every row** (no `/dev/kvm` in these VMs, no
firecracker, not root). Raw results: `e2e/real/results/l1-linux/`.
With the sandbox on, the Ubuntu 24.04 arm64 row (reboot + ephemeral phases),
Debian 12 and Ubuntu 24.04 amd64 were re-run during integration, and Ubuntu
24.04 arm64 again on the final tree (`sandbox ok`, both workloads
`process/landlock`, 410 after kill in ≤ 1 ms, reboot → answering 7.7 s;
STATUS.md "Integration (Linux: L2+L3+L1)" and "Final verification"). The
microVM tier's own rows (`make e2e-linux-vm`, `make e2e-linux-reboot` on a
nested-virt VM, from the shipped tarball) are there too.

| | Ubuntu 24.04 arm64 | Debian 12 arm64 | Ubuntu 22.04 arm64 | Fedora 45 arm64 | Ubuntu 24.04 **amd64** (qemu, emulated) |
|---|---|---|---|---|---|
| result | **PASS** | **PASS** | **PASS** | **PASS** | **PASS** |
| glibc · kernel · Landlock ABI | 2.39 · 6.8 · 4 | 2.36 · 6.1 · 2 | 2.35 · 5.15 · 1 | 2.44 · 7.2 · 10 | 2.39 · 6.8 · 4 |
| `install.sh` | 0.41 s | 0.81 s | 0.84 s | 0.47 s | 9.1 s |
| `tw up --service` → ready | 0.70 s | 0.65 s | 0.64 s | 0.68 s | 3.4 s |
| `tw deploy` → ready (agent / mcp) | 2.78 / 1.57 s | 1.57 / 1.45 s | 1.50 / 1.46 s | 1.59 / 1.56 s | 4.70 / 5.55 s |
| first request → object node ready | 1.64 s | 1.67 s | 1.62 s | 1.59 s | 3.95 s |
| `tw kill --cascade` (CLI / push) | 66 / 56 ms | 72 / 63 ms | 65 / 59 ms | 69 / 56 ms | 319 / 133 ms |
| request to a killed object → 410 | 1.7 ms | 1.3 ms | 0.9 ms | 0.9 ms | 10.9 ms |
| Mac → VM path | **direct** after 0.19 s | **direct** after 0.18 s | **direct** after 0.18 s | **direct** after 0.20 s | **direct** after 0.47 s |
| MCP `tools/list` direct · DERP-only (median) | 1.9 · 10.7 ms | 2.5 · 11.2 ms | 3.5 · 11.2 ms | 2.0 · 16.6 ms | 33.1 · 41.8 ms |
| disco ping direct · DERP (median) | 0.41 · 7.8 ms | 0.57 · 9.2 ms | 0.67 · 8.5 ms | 0.42 · 14.1 ms | 1.47 · 13.4 ms |
| `systemctl --user restart tw` → healthy / workload answers the Mac | 3.6 / 6.6 s | 3.6 / 6.7 s | 3.6 / 6.7 s | 0.7 / 3.8 s | 5.6 / 13.1 s |
| `tw down --purge --service` | 0.62 s | 0.56 s | 0.60 s | 0.63 s | 4.0 s |
| reboot (enable-linger, `limactl stop`/`start`) → service active / healthy / workload answers | 6.6 / 6.6 / 8.6–17.1 s | — | — | — | — |
| `--ephemeral` sub-agent across `systemctl --user restart` → re-minted, ready / answers | 4.1 / 9.5 s | — | — | — | — |

The direct path is the Mac ↔ Lima VM NAT (a private address); DERP-only
numbers come from a second client with `TS_DEBUG_ALWAYS_USE_DERP=1`
(region `sea`). The `systemctl --user restart` command itself returned in
about 3.1 s on Ubuntu and Debian (the supervisor drains hostd, then mgmt,
before systemd starts the new one), 0.17 s on Fedora 45, and 4.6 s on amd64.
In every row, the first request after the restart that succeeded had waited
about 3 s for the workload node to rejoin. The amd64 column ran under qemu
x86-64 emulation on an Apple-silicon Mac, so its timings are slower than a
real x86 host's.

Unsupported hosts (bonus rows; `TestUnsupportedHost`):

| | Debian 11 arm64 (glibc 2.31, kernel 5.10) | Alpine 3.24 arm64 (musl) |
|---|---|---|
| `install.sh` | refused: `glibc 2.31 is too old: tw's workerd needs glibc >= 2.35 (Ubuntu 22.04+, Debian 12+, Fedora 36+, RHEL 10)` | refused: `this Linux uses musl libc (Alpine?); tw's workerd needs glibc >= 2.35 …` |
| `bin/tw doctor` | `✗ glibc` (names the distro and the fix); `! kernel` (< 5.13); `! landlock` (ENOSYS) | `✗ glibc`: `this system uses musl libc (/lib/ld-musl-aarch64.so.1) …` |
| would workerd start anyway? | yes (`workerd --version`; untested beyond that) | no (no glibc loader) |

RHEL/Alma/Rocky 8–9 and Amazon Linux 2 were not run in the matrix; they are
refused for their glibc version.

**Lima notes.** Debian 11's genericcloud image doesn't boot under Lima's
`vmType: vz`; use qemu.

## Restarts and reboots

Measured on Ubuntu 24.04 arm64 (the matrix's first column):

- **`--ephemeral` nodes across a restart:** a spawned sub-agent was ready
  again 4.1 s after `systemctl --user restart tw`, and answering a client on
  another machine after 9.5 s.
- **Reboot, lingering on:** from the start of boot, ssh up at 6.6 s,
  `tw.service` active and tailworks healthy at 6.6 s, every object rejoined from
  saved state (same node IDs, nothing re-minted), and the workload answered
  a client on another machine at 8.6–17.1 s (two runs).

The spread is on the **client's** side, not tailworks': a persistent node comes
back with the same node key but fresh WireGuard state, so a client that
still holds a recent session to it keeps sending on that dead session, and
WireGuard only re-handshakes after ~15 s of silence. A client whose session
had already gone idle, or a client that has never talked to the node,
reaches it within 2–3 s of `tw up`. The node is reachable over DERP the
moment it is up. The diagnosis is in STATUS.md, "Diagnosis: the 12–17 s
"client re-finds the node after reboot" delay".

## Fast identity

How fast a new object's own node comes up with the
[tuning](INSTALL.md#tuning-how-fast-a-new-objects-own-node-comes-up)
defaults (warm keys, pool nodes, no port mapping), measured on a production
tailnet: the first request on the object's own node arrives ~0.23 s after
it is created (p95 ~0.3 s), down from ~7 s. The breakdown, and the numbers
before each step, are in
[ADR-031](design/adr/031-pre-joined-node-pool.md).

## The microVM tier

- **Tested end to end** on Ubuntu 24.04 arm64 (nested virtualization).
  `make e2e-linux-vm` and `make e2e-linux-reboot` ran from the shipped
  tarball; the rows are in STATUS.md, "Final verification".
- **amd64:** the assets are built and the guest boots to workerd, but the
  only x86 test host so far (nested KVM in a QEMU VM on an AMD Zen 1 machine,
  whose TSC it reports unstable) stalls its guests in an interrupt storm,
  so the amd64 tier is not yet verified end to end.
- **Fast starts** (the warm pool and snapshot restore): the measured times
  are in [ADR-030](design/adr/030-microvm-snapshot-restore.md).
- **A tarball without the guest.** The red team's second pass found a
  Mac-built tarball without the microVM guest. `make release` now builds
  the guest through Lima on a Mac, or warns, and the GitHub release
  workflow refuses to publish without it (see
  [INSTALL.md](INSTALL.md#linux-the-microvm-tier-firecracker-kvm)).

## Safety rules for the real-tailnet suites

The suites that join real tailnets (`e2e-real`, `e2e-mac`, `e2e-mbp`,
`e2e-multi`, `e2e-linux`, `e2e-oauth`, `e2e-public` and the like) follow
these rules, and new ones must too:

1. **Dedicated test tailnets only.** Each suite allow-lists the tailnets it
   may touch and refuses any other. Never point one at a personal or
   production tailnet, and never use the machine's system `tailscaled` or
   Tailscale app: tailworks' own nodes are tsnet nodes.
2. **Policy changes are additions only,** validated before they are
   applied, applied with `If-Match`, and only after a copy of the previous
   policy has been saved next to the credentials. Record each change in
   your `status/<branch>.md`.
3. **Never print keys** (auth keys, OAuth secrets, node keys) in test
   output, logs or status files.
4. **Clean up what you created:** delete your own devices and revoke your
   own keys by ID, even when the test fails.
