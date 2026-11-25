# Data-plane loop ledger: containers, pooled nodes and snapshot restore

Issue #423. Process: `../LOOP.md` ("Containers, pooled nodes and snapshot
restore"). Findings are filed as GitHub issues labelled `dataplane-chaos`.
Run it with `make e2e-dataplane-vm` (needs a Mac's virtualization or
Linux + KVM as root).

## Status

| Scenario | Backend | Clean seeds (consecutive) | Notes |
|---|---|---|---|
| containers | vz | | |
| pool | vz | | |
| restore | Firecracker | | |
| mixed | vz | | |

## Findings

| ID | Verdict | Invariant | Issue | Summary |
|---|---|---|---|---|

## Harness notes (not product bugs)

## Run log

| Run (results dir) | Backend | Scenario / seed | Verdict |
|---|---|---|---|
