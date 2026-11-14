# ADR-015: Headscale profile and framework scope

**Date:** 2026-11-07
**Status:** Accepted.
**Decision:** The Headscale profile uses pre-auth keys, has no posture and no Services, and puts caps in grants only. MVP framework adapters are the Cloudflare Agents SDK and the MCP TS SDK. Claude Agent SDK, LangGraph and OpenAI Agents are tailnet *clients* of twd tools, not in-isolate.
