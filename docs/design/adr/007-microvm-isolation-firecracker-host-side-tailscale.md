# ADR-007: microVM isolation (Firecracker) with a host-side "Tailscale NIC", tailvisor-style

**Date:** 2026-11-07
**Status:** Accepted in direction (Ross). Mechanism details are in Phase 2.
**Context:** workerd is not a hardened sandbox, and tenants from different tailnets share hosts. tailscale/tailvisor (BSD-3, experimental) already gives a VM a tailnet identity from a host-side tsnet node via TAP (Linux) or a vmnet socket (macOS).
**Decision:** Tenant code runs inside microVMs: Firecracker on Linux, Virtualization.framework for local dev on macOS. The VM's network is a host-side tsnet-backed NIC, derived from or shared with tailvisor's netd. The guest never holds tailnet keys.
**Consequences:**
- Strong isolation, and the identity can't be stolen from inside the guest.
- Firecracker needs Linux and KVM, so hosted and Linux self-hosted are first-class, and macOS is a dev tier.
- How many objects share a VM (one VM per object vs per tenant with multiple NICs) is a Phase 2 decision.
