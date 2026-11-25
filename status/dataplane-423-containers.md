# dataplane-423-containers

Issue #423: the data-plane chaos loop for containers, pooled nodes and
snapshot restore. Builds on the `e2e/dataplane` harness (#415).

## What it adds

- `e2e/dataplane/containers_test.go`, `check_containers_test.go` (build tags
  `e2edataplane e2edataplanevm`), workloads in
  `e2e/dataplane/testdata/containers/` (`ctr`: one container + one node per
  Box object; `vm`: a microVM-tier workload for restore bursts).
- Faults `api-stall`, `burst-crash`, `crash-hostd-vmm`, `kill-vmm`,
  `redeploy`, `vm-burst`, plus the shared ones. Invariants K1–K4
  (LOOP.md "Containers, pooled nodes and snapshot restore").
- `make e2e-dataplane-vm` (macOS vz or Linux KVM as root; not in the
  nightly). `make lint` vets the new tags for darwin and linux.
- Harness: two fields on `cluster` (`workloadName`, `hostArgs`), so the
  shared faults (`kill-host`, `move`, takeovers) restart hosts with the
  microVM flags and move this loop's workload.
- Ledger: `e2e/dataplane/ledger/containers.md`.

## Results

See the ledger's status table and run log.
