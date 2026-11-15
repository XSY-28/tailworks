---
kind: breaking
upgrade: [config-default]
---
`twd logs <workload>` now prints one structured line per request, with filters (`--errors`, `--status`, `--trace`, …) and `--json` request records. Pass `--console` for the app's raw output, which used to be the default. twd also gains Prometheus `/metrics` and OTLP export of logs, metrics and traces (`twd telemetry`).

Operator action: scripts that read `twd logs` output need `--console`. twd-hostd and twd-mgmt now serve `/metrics` on port 9464 of their tailnet node by default (`-metrics-port 0` turns it off); scraping it needs a policy grant.
