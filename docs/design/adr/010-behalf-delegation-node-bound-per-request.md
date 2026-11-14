# ADR-010: On-behalf-of and delegation as node-bound, per-request, single-hop assertions; no bearer tokens

**Date:** 2026-11-07
**Status:** Accepted.
**Context:** SEC-2: A's Biscuit could be re-bound and replayed by any recipient. SEC-6/DX-H8: on-behalf-of was fixed per object. OSS-10: Biscuit has little ecosystem uptake.
**Decision:** The sending netd signs an assertion bound to the sender node, with the receiver node as its audience, a 60 s TTL, an `act` chain (RFC 8693 shape) and pairwise subjects. Sub-agent narrowing is primarily network-enforced: the child node gets tags ⊆ the parent's. Values finer than tags, such as a "$50" limit, live in the assertion until Tailscale offers per-node cap values.
