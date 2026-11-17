# @tailworks/mcp

Stateless MCP servers on [tailworks](https://github.com/rosskukulinski/tailworks),
authorized per caller from the tailnet: no OAuth and no sessions, because
WireGuard and WhoIs authenticate every request.

```ts
import { withTailnet, caller, authorizeTool, budget } from "@tailworks/mcp";
export { TwBudget } from "@tailworks/mcp/budget-do";
```

- `withTailnet(handler)`: wraps an MCP handler (for example
  `createMcpHandler` from `agents/mcp/server`) so `caller()` works inside
  tools.
- `authorizeTool(caller, cap, facts)`: checks the caller's app capability
  grant (tool name, limits such as `maxUsd`) and throws `cap_missing`.
- `budget(ns, caller, tool).reserve(n)`: a per-caller daily budget kept in
  the `TwBudget` Durable Object.
- `mcpFetch(ctx)`: a `fetch` for MCP clients that leaves from this node's
  tailnet identity.

`tw init my-mcp --template mcp` creates a working server.
