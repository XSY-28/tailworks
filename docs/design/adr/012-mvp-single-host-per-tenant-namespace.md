# ADR-012: MVP is single-host per tenant namespace; v1 adopts upstream workerd cluster via public hooks

**Date:** 2026-11-07
**Status:** Accepted.
**Context:** OPS-1: workerd's cluster exits on bucket stalls of 8–12 s. OPS-6: no re-homing. OSS-3: its private layout is about to change, because celld is merging in.
**Decision:** The MVP uses stock single-process DO storage with async encrypted backup. v1 is gated on upstream hooks: an ownership-change event, degrade-instead-of-exit, and re-homing.
