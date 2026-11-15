// make test-vitest: twd templates' plain Worker and Durable Object logic under
// @cloudflare/vitest-plugin (tests run inside workerd, per test file
// storage), with @twd/tailnet/testing for callers and ctx.tailnet.
// One project per template: each has its own wrangler.jsonc.
import { cloudflareTest } from "@cloudflare/vitest-plugin";
import { defineConfig } from "vitest/config";

const template = (name: string) => ({
  plugins: [cloudflareTest({ wrangler: { configPath: `../../js/packages/create-twd/templates/${name}/wrangler.jsonc` } })],
  test: { name, include: [`test/${name}.test.ts`] },
});

export default defineConfig({
  test: {
    projects: [
      template("lock"),
      template("agent"),
      template("mcp"),
      template("refund-approval"),
    ],
  },
});
