# ADR-004: Node core: Go (tsnet) now, keep it swappable for tailscale-rs

**Date:** 2026-11-07
**Status:** Proposed.
**Context:** tailscale-rs (v0.7.1, preview, BSD-3) lacks app caps, MagicDNS, Serve and Services. tsnet has workload identity federation, WhoIs+CapMap, and Services. Ross suggested JS support for tailscale-rs as a separate DX project.
**Decision:** The host's tailnet layer goes behind an internal interface. Go tsnet is the reference implementation, and tailscale-rs is tracked as a future core and a separate JS-bindings project.
