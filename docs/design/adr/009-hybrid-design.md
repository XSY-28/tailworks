# ADR-009: Hybrid design: B's control/security model, A's upstream alignment

**Date:** 2026-11-07
**Status:** Accepted (lead, after Phase 3 review; see review/resolutions.md).
**Context:** Security review rated B stronger: platform-set egress, node-bound assertions, key release tied to the lease, and hosts with no minting credential. OSS and DX reviews rated A stronger: stock DO code, upstream workerd alignment, less lock-in.
**Decision:** The controller owns each object's node lifecycle and key custody, as in B. DO execution is stock workerd namespaces (no facets). Distribution follows upstream workerd cluster mode through public hooks, as in A.
**Rejected:** B's facets, which lose alarms and cross-host RPC. B's per-region Raft (ADR-011). A's Biscuit tokens (ADR-010). A's reading of workerd's private bucket layout.
