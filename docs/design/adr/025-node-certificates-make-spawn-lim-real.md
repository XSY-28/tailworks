# ADR-025: Node certificates make spawn `lim` real; audit provenance; host token

**Date:** 2026-11-08
**Status:** Accepted (implemented, twd rt-fixes), from red-team findings RT-01/02/03. **Verified live** by red-team pass 2 (2026-11-08) against the installed Linux release: RT-01/02/03/05/06/08 hold; the attack suite is cross-compiled and runs in CI (`security/attacks/`). See twd `security/RED-TEAM.md` §Pass 2.
**Context:** Spawn `lim` was validated but never bound to the child's identity, so receivers saw full policy caps: least-privilege delegation was cosmetic, and e2e check (d) only proved a child existed. Tenant code could write privileged audit kinds into the valid hash chain. mgmt's socket trusted same-uid peers.
**Decision:** mgmt signs an ADR-010 node certificate `{tenant, objectID, stableNodeID, tags, lim, parent, root, exp}` with a per-tenant Ed25519 root at `ready` (and again after re-mint). The receiving netd fetches and verifies the sender's cert and computes `effectiveCaps = CapMap ∩ lim`, failing closed. Tenant `audit()` yields only `tenant_note` with netd-assigned fields; privileged kinds require the host token. A per-host bearer token (0600, rotated on `twd up`) is required on every mgmt socket request. e2e check (d) now asserts the child's denial.
**Lesson:** acceptance tests must assert the *security outcome*, not the existence of the mechanism.
