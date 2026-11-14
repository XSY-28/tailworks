# ADR-001: Pivot: Tailscale-native Workers-like runtime + Durable-Objects layer

**Date:** 2026-11-07
**Status:** Accepted (Ross).
**Context:** Phase 1 showed that putting a full node in every foreign runtime is constrained: the Go wasm client is 27–37 MB and uses 120–145 MB of RSS against a 128 MB DO cap; there is no UDP, so traffic is DERP-only; keep-alive defeats hibernation (~$4/mo per always-on DO node); and tsnet does not compile for js/wasm.
**Decision:** Build a self-hosted, OSS, workerd-based runtime that runs on tailnet nodes. The host is a native tailnet node, and scripts and objects run in isolates behind it. Then build a Durable-Objects-equivalent on top. Agents are the flagship use case of a general platform.
**Consequences:** Native transport (UDP, direct paths) on hosts. Identity granularity (per host / per script / per object) becomes the central design question. Existing Workers code ports to it. Foreign-runtime agents become clients of the platform.
