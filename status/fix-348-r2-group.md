Last update: 2026-11-24. A workload that is not a replica can't be deployed under a replica's name (#348, ledger H-r2group); mgmt and hostd read replica names with one parser.

# fix-348-r2-group: one definition of a replica's name

## What

- The red team's fresh-eyes pass (2026-11-23, master `27fd3de4`) found two
  resolvers for a workload's replica group that disagreed for one kind of
  name. hostd files R2 blob bytes under `r2Group(name)`, which stripped a
  trailing `--r<n>` (n ≥ 2) from the workload's name. mgmt scopes the
  bucket's index, every other store and the secrets by the deployment
  record (`replicaGroupOf`). A workload of its own named `web--r2` kept its
  index to itself and wrote its bytes under `web`'s prefix. Latent: blobs
  are reached only by random IDs from the workload's own index, and nothing
  lists or sweeps a prefix.
- New package `internal/replicaname`: `Name` (what mgmt names a replica)
  and `Group` (is this a replica's name, and whose). mgmt's `ReplicaName`
  and hostd's `r2Group` both use it; hostd's own regular expression is
  gone.
- mgmt refuses a name `Group` recognises to anything but a replica:
  `checkReplicaName` (`mgmt/workload_replicas.go`), called from `Deploy`
  (the one place a deployment record is written) and from the deploy's dry
  run. `409 replica_conflict`. A preview alias that makes such a name
  (`pr7--r2`) is refused the same way.
- Existing names are not backfilled: a workload deployed under such a name
  before the rule keeps deploying while it is live, and keeps the shared
  blob prefix until it is renamed.

## Why this option

The issue offered two. Passing the mgmt-resolved group to hostd (option 1)
would need a new assignment field: `Assignments` folds the `_workload`
namespace into `workloadTags` and `service`, so a replica's `replicaOf`
never reaches its host. hostd would also need a fallback for an older mgmt,
and the blobs of any workload already deployed under such a name would
move. Reserving the name (option 2) changes no wire format and no blob
path. Sharing the parser keeps the two sides from drifting apart again,
which is the part of option 1 that costs nothing.

## Tests

- `mgmt.TestRT_Hr2group_ReplicaNameReserved` (new, through the `/v1` API):
  new names of the shape are refused (deploy and dry run) and nothing is
  written; near-miss names (`web--r1`, `web--r02`, `web-r2`, …) deploy;
  replicas deploy, and a replica's name stays refused to a workload of its
  own after the replica is gone; a preview alias that makes such a name is
  refused; a workload seeded as an older mgmt would have accepted it keeps
  deploying until it is undeployed. For every accepted name it checks that
  mgmt's store scope is hostd's blob group. With the mgmt change reverted:
  FAIL (`POST /v1/deployments: got 202, want 409 replica_conflict`).
- `mgmt.TestWorkloadReplicas/NameTakenBySeparateWorkload` now seeds the
  separate workload in the store (the API refuses to make one).
- `hostd.TestR2BlobPrefixIsTheReplicaGroup` (new): the `r2API` handler with
  a directory backend files a replica's blob under its workload's prefix
  and reads its workload's blob.
- `internal/replicaname`: `TestGroup`, `TestName`,
  `TestGroupMatchesHostdBlobPrefix` (200 000 random names against the
  expression hostd used) and `FuzzGroup` (9.7 million executions, no
  difference).
