# Data-plane loop ledger: containers, pooled nodes and snapshot restore

Issue #423. Process: `../LOOP.md` ("Containers, pooled nodes and snapshot
restore"). Findings are filed as GitHub issues labelled `dataplane-chaos`.
Run it with `make e2e-dataplane-vm` (needs a Mac's virtualization or
Linux + KVM as root). Backends used so far:

- **vz**: this Mac (Apple silicon, Virtualization.framework, no sudo), 3
  hosts, `-pool-nodes 2`, the arm64 `tw.local/debian-trixie-python:1`.
- **Firecracker**: an amd64 Ubuntu 24.04 KVM guest (2 vCPU, 6 GiB),
  hostd as root with the in-process helper, 2 hosts
  (`TW_DPK_HOSTS=2 TW_DPK_MAX_OBJECTS=4 TW_DPK_BURST=2`), warm pool 1,
  snapshots on. The only backend with snapshot restore, so `restore` runs
  here.

## Status

STATUS_TABLE

## Findings

| ID | Verdict | Invariant | Issue | Summary |
|---|---|---|---|---|
| K-F1 | FIXED (#583) | K4 (every ready pool node runs on a host) | #567 | When the lease moved, the old host's pool nodes stayed `ready` in mgmt with no host and their devices were never deleted: 4 per run in containers seeds 1 and 2. mgmt now kills another host's pool nodes when a deploy places the workload |
| K-F2 | FIXED (#590) | K4 (no VMM outlives its hostd) | #587 | Linux, in-process helper: a crashed hostd's Firecracker VMs (and their netns, jails, cgroups) were never reaped, since the helper names them after hostd's pid and its successor only cleans its own. 18 of 18 VMMs outlived 4 crashes (containers seed 11, `crash-hostd-vmm`), 0 of 26 with the fix. 118 leaked netns on the test host |
| K-F3 | FIX OPEN (#602) | K2 (final MagicDNS name read back before ready) | #599 | An object went ready, and reported ready to mgmt, with an empty MagicDNS name: netd's join took `Self.DNSName` from the first status after `Up` without checking it. 18 times in pool seed 1 (vz), 0 in pool seed 2 (intermittent) |

## Harness notes (not product bugs)

- Images: tw-hostd pulls container images only from mgmt's registry
  (ADR-032 D3), so the test pushes `TW_DPK_IMAGE` from `TW_CTR_IMAGES` as
  a public base first (`pushPublicImage`), as `tw image push` does.
- The keyscan reads the guest's disks whole (minutes per container on vz):
  one container per run (`TW_DPK_KEYSCANS`).
- Killing an object from inside it (`ctx.tailnet.kill`) cut its own
  answer off (a plain 500). Retired objects are killed through mgmt's
  `DELETE /v1/objects/{id}`, as `tw kill` does, so the result is known.
- A host that lost the lease keeps its objects' records as `pending` (no
  node) to rejoin if the lease comes back; K4 counts only `ready` ones.
- The vm workload's first draft named a field `at`, which broke decoding
  its `/id` answers into `vmRep.At`: K3 compared no microVM copies in
  restore seeds 1–3. Fixed (`drawnAt`); restore seeds 4–6 compared 6, 18
  and 15 copies.
- Once, right after a hostd restart, an `up` got a 200 that wasn't the
  object's answer (no boot_id, no epoch) and was read as epoch 0 (a false
  stale answer, containers seed 11 on Firecracker). Such a 200 is now an
  error, and K1 notes it with its body; not seen since.
- `crash-hostd-vmm` on vz: the VMMs (tw-vz) die with hostd (0 survivors
  in every run), so K-F2 is Linux only.
- K1's fence bound is 45 s (FenceAfter 30 s + a heartbeat + slack) from
  the later of the lease leaving the host and the end of the last fault on
  it. No container of a non-holder ran past it in any run.

## Run log

| Run (results dir or log) | Backend | Scenario / seed | Verdict |
|---|---|---|---|
RUN_LOG
