---
kind: feature
upgrade: [schema-expand, protocol, feature-string, format, cli-json]
---
You can now plan, check and undo upgrades (ADR-upgrades-and-releases).

- **Versions everywhere.**
  - Every binary carries the release version (`--version`).
  - Hosts report their version, protocols and features to mgmt, and mgmt
    replicas record their own versions.
  - `/v1/status` shows every version running in the fleet.
- **Skew policy.** A host may run one release line behind mgmt, but never
  ahead of it.
  - mgmt refuses a host outside that window with `version_skew`, and places
    nothing new on it.
  - The CLI warns you when it is too old or too new for mgmt.
- **Numbered mgmt migrations.**
  - The store records each migration it applies in `schema_migrations`.
  - A binary older than the store's minimum version refuses to start, so a
    downgrade can't corrupt the store.
  - `twd-mgmt migrate --check` lists the pending migrations.
- **Feature-gated placement.** A deployment that uses KV, R2 or D1 goes
  only to hosts that advertise that feature. If no host does, the deploy
  fails with `feature_unavailable`.
- **Host cordon and drain.** `twd host cordon`, `uncordon` and `drain`
  take a host out of placement and move its workloads off it before
  maintenance.
- **`twd upgrade`.**
  - It plans the upgrade, then backs up the store and custody together.
  - It installs and restarts the new release, then health-checks it.
  - It rolls back automatically if the health check fails.
  - `install.sh` keeps the previous install as `bin.prev` and `share.prev`,
    and `install.sh --rollback` restores it.
- **Release catalog.** twd-mgmt embeds the notes and upgrade facts of every
  release up to its own. `twd release import` adds a newer one, so
  `twd upgrade --plan` works offline.
- **Upgrade runs.**
  - mgmt drives a fleet upgrade host by host, starting with a canary and
    health-checking each host before the next.
  - The run keeps going if the CLI disconnects.

Operator action: none for a homelab. For a fleet, upgrade twd-mgmt before
any host.
