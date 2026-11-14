# ADR-006: Every object gets its own tailnet identity

**Date:** 2026-11-07
**Status:** Accepted (Ross).
**Decision:** Each DO-like object instance, not just each deployment, is a distinct node in its owner's tailnet, with its own MagicDNS name, its own tags and grants, and its own entries in flow logs.
**Consequences:**
- Node count scales with the object count, so churn, netmap size, ephemeral cleanup and per-node host memory become first-order design problems.
- Hibernation must keep the identity reachable: the host-side node stays up while compute sleeps.
- The ADR-005 open question on workload granularity is resolved: "per object".
