# ADR-013: Credentials: per-tenant WIF trust, scoped to auth keys for twd tags only; issuer key in KMS

**Date:** 2026-11-07
**Status:** Accepted. Tag-scoped token exchange is an open question for Ross.
**Context:** SEC-1: `devices:core` on a single platform signing key amounts to device-admin over every tenant tailnet.
**Decision:** Each tenant has its own trust and subject. The scope is `auth_keys` limited to `tag:twd-*`. Device deletion uses a separate opt-in credential. Minted keys are single-use, preauthorized, ephemeral, and expire in ≤5 minutes.
