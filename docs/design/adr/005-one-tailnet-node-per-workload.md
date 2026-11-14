# ADR-005: One tailnet node per workload; multi-tailnet hosts; management plane + tenant data planes

**Date:** 2026-11-07
**Status:** Accepted (Ross).
**Context:** Workloads may belong to different tailnets, even on one host and even when self-hosted.
**Decision:** Each workload joins its owner's tailnet as its own node. A host runs nodes in N tailnets. A management plane (its own node in the operator tailnet) supervises the workloads, holds placement and leases, and never shares a node identity with them. The same design must work Tailscale-hosted and self-hosted.
**Consequences:** This supersedes the "one node per host" option (A in the earlier plan). Tenant isolation becomes a first-class requirement, at the process level or stronger. Credential bootstrap is per-tenant. Control-plane load is per workload node in each tenant tailnet, so it's modest per tailnet. Open: how granular a "workload" is (deployment vs DO instance).
