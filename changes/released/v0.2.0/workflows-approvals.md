---
kind: feature
upgrade: [workerd]
---
Cloudflare **Workflows** (wrangler `workflows`) run on workerd's built-in engine, and `step.waitForApproval` is decided by tailnet identities holding `twd.dev/cap/approve` (`twd approvals`, `twd approve`; ADR-040).

Operator action: workerd moves to 1.20261114.1.
