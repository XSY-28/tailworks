---
kind: security
upgrade: []
---
Security fixes from review. `twd up` runs as a single process by default (mgmt with the host agent embedded; `--separate-host` keeps the old layout), and twd-mgmt serves an admin UI at `/ui/`.
