// A black-box test: `twd dev` runs the app (the same netd and workerd as a
// deployment, on a local control plane) and the test calls it as a tailnet
// identity from its own dev node. Needs twd on PATH (or TWD_BIN).
// docs/testing.md has unit tests too (@twd/tailnet/testing, vitest).
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { startDev } from "@twd/tailnet/testing/dev";

let app;
before(async () => {
  app = await startDev({
    dir: fileURLToPath(new URL("..", import.meta.url)),
    callers: ["alice"],
  });
});
after(() => app?.stop());

test("the agent's MCP calls leave from its own node, and a missing server is a typed error", async () => {
  // No MCP server runs next to this app in twd dev, so MCP_URL (http://mcp/mcp)
  // is not in the agent node's netmap: twd refuses the call with a typed
  // error, which the template returns as its catalog response.
  const r = await app.as("alice").fetch("/?prompt=where%20is%20o1");
  assert.equal(r.status, 403);
  assert.equal(r.headers.get("twd-error"), "peer_not_visible");
  const body = await r.json();
  assert.equal(body.code, "peer_not_visible");
});
