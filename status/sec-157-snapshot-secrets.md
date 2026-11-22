Last update: 2026-11-21 PT. H24 (#157): what snapshot-restored microVMs still share after the reseed, measured and disclosed.

## What changed

- **Analysis:** ADR-030, section "(d) analysed". Copies of one base share the
  kernel secrets created before the snapshot: `net_secret` (the TCP ISN and the
  ephemeral-port offset), `ts_secret`, the syncookie and hash-table secrets, the
  boot-time hash seeds, and guestd's Go runtime seeds. They don't share anything
  drawn from the CRNG after the load. That includes the tenant's workerd and
  every per-socket random value.
- **Verdict: not exploitable across tenants, so disclosed.** A guest's TCP ends
  at its own VM's gVisor gateway stack (`hostd/vm/vnet.go`). No guest segment
  leaves the host, and guests have no path to each other, so nobody can observe
  or inject into another copy's ISNs, ports or timestamps. Tenant input never
  chooses keys in another guest's kernel hash tables. On amd64, Firecracker's
  direct `vmlinux` boot gives no KASLR slide at all ("Kernel Offset: disabled").
- **No rotation on load:** Linux can't re-key `net_secret` or `ts_secret` from
  userspace. The TCP Fast Open key is writable, but guests run no TFO server.
- **Docs:** docs/security-model.md Known gaps, the LEDGER.md H24 row
  (**disclosed**), and the THREAT-MODEL.md microVM row.
- **Test:** `vm.TestSnapshotKernelState` (`hostd/vm/snapshot_kstate_test.go`,
  linux + KVM). It builds a static probe and runs it in 3 restored copies and in
  a cold-booted control, through guestd's debug `run`. It reads the TCP ISN with
  the clock term removed (the `net_secret` hash), the TCP timestamp offset
  (`ts_secret`), the first ephemeral port, `_text` and the TFO key.
  - Asserted: a rerun in the same guest matches itself, and the cold control
    differs. That shows the method can tell secrets apart.
  - Recorded, not asserted: what the copies share.
  - `snapBackend` now passes `TW_VM_VCPUS` through.

## Tests

- `make lint`: ok.
- `make test` (mbp runner, `mbp-test.sh test`): ok.
- `GOOS=linux go vet ./hostd/vm/`: ok. The probe builds for linux/amd64 (`go build`, `go vet`).
- **Probe method, validated on Linux (the twd-amd64 host kernel, 6.8, not a guest):**
  two root runs gave ISN keys `0x2a8d0774` and `0x2a8d0761` (19 units apart; the
  tolerance is 2^16) and the same timestamp key. Run unprivileged, it fails as
  expected: TCP_REPAIR needs CAP_NET_ADMIN, and guestd's `run` is root. The
  ephemeral port moved by 12 between runs (the perturbation table advances), so
  the port is recorded, not compared.
- **`vm.TestSnapshotKernelState` in a real guest: not run (harness broken).** On
  `twd-amd64` (nested KVM on nas-1, 2026-11-22) every Firecracker boot stalls
  after `PCI: Fatal: No config space access function found`, with a guest soft
  lockup at ~28 s. That includes master's own `TestSmoke` and
  `TestSnapshotUniqueness`, with and without hugepages, at 1 or 2 vCPUs, with
  the VM idle and under load. So the copies' measured sharing is still to be
  recorded. The run that would do it:
  `sudo env PATH=$PATH TW_VM_HUGEPAGES=on TW_VM_KERNEL=… TW_VM_ROOTFS=… bin/vmsmoke.test -test.run TestSnapshotKernelState -test.v`
  (or `make e2e-vm`) on a working KVM host (Lima tw-dev).
- `make e2e-vm`: skipped (it needs a working KVM host; see above).
