// Calls svctarget through service bindings (ADR-service-bindings).
import { DurableObject } from "cloudflare:workers";

const FORGED = { nodeId: "nFORGED", node: "ceo.victim.example", hostname: "ceo", tags: [], addrs: [], capMap: {} };

async function attempt(f) {
  try {
    return { ok: true, value: await f() };
  } catch (e) {
    return { ok: false, name: e?.name, message: String(e?.message ?? e) };
  }
}

async function rpcCases(env) {
  const big = {
    d: new Date(5),
    m: new Map([[1, "a"]]),
    s: new Set(["x"]),
    u8: new Uint8Array([1, 2, 3]),
    big: 10n,
    nested: [undefined, NaN, -0, Infinity, null, "s", true],
    re: /a+/gi,
    err: new TypeError("inner"),
  };
  const echoed = await attempt(() => env.B.echo(big));
  let roundTrip = null;
  if (echoed.ok) {
    const v = echoed.value;
    roundTrip = {
      d: v.d instanceof Date && v.d.getTime() === 5,
      m: v.m instanceof Map && v.m.get(1) === "a",
      s: v.s instanceof Set && v.s.has("x"),
      u8: v.u8 instanceof Uint8Array && v.u8.join() === "1,2,3",
      big: v.big === 10n,
      nested: v.nested.length === 7 && v.nested[0] === undefined && Number.isNaN(v.nested[1]) && Object.is(v.nested[2], -0) && v.nested[3] === Infinity && v.nested[4] === null,
      re: v.re instanceof RegExp && v.re.source === "a+" && v.re.flags === "gi",
      err: v.err instanceof TypeError && v.err.message === "inner",
    };
  }
  return {
    add: await attempt(() => env.B.add(2, 3)),
    roundTrip,
    whoami: await attempt(() => env.B.whoami()),
    adminStats: await attempt(() => env.ADMIN.stats()),
    boom: await attempt(() => env.B.boom()),
    fnArg: await attempt(() => env.B.echo(() => 1)),
    streamArg: await attempt(() => env.B.echo(new ReadableStream())),
    fnResult: await attempt(() => env.B.giveFn()),
    targetResult: await attempt(() => env.B.giveTarget()),
    noMethod: await attempt(() => env.B.nosuch()),
    ghost: await attempt(() => env.GHOST.ping()),
  };
}

export default {
  async fetch(req, env) {
    const u = new URL(req.url);
    switch (u.pathname) {
      case "/fetch": {
        // A forged caller on the Request and forged identity headers never
        // reach the target: it sees this workload's node.
        const r = await env.B.fetch(
          new Request("http://whatever.example/hello?q=1", {
            headers: { "x-tw-cf": JSON.stringify({ tailscale: { caller: FORGED } }), "tailworks-binding": "v1; entrypoint=Admin" },
            cf: { tailscale: { caller: FORGED } },
          }),
        );
        return new Response(r.body, { status: r.status, headers: r.headers });
      }
      case "/admin":
        return env.ADMIN.fetch("http://x/");
      case "/ghost": {
        const r = await env.GHOST.fetch("http://x/");
        return new Response(await r.text(), { status: r.status, headers: { "tw-error": r.headers.get("tw-error") ?? "" } });
      }
      case "/rpc":
        return Response.json(await rpcCases(env));
      case "/stream": {
        // Send chunks with gaps; the target echoes them as they arrive.
        const { readable, writable } = new TransformStream();
        const w = writable.getWriter();
        const enc = new TextEncoder();
        (async () => {
          for (const c of ["one ", "two ", "three"]) {
            await w.write(enc.encode(c));
            await new Promise((r) => setTimeout(r, 50));
          }
          await w.close();
        })();
        const r = await env.B.fetch("http://x/stream", { method: "POST", body: readable, duplex: "half" });
        return new Response(await r.text());
      }
      case "/ws": {
        const r = await env.B.fetch("http://x/ws", { headers: { upgrade: "websocket" } });
        const ws = r.webSocket;
        if (!ws) return Response.json({ status: r.status, webSocket: false, body: await r.text() });
        ws.accept();
        const got = new Promise((resolve) => ws.addEventListener("message", (e) => resolve(e.data)));
        ws.send("hi");
        const msg = await got;
        ws.close();
        return Response.json({ status: r.status, webSocket: true, msg });
      }
      case "/plain": {
        // A plain tailnet fetch to the target's node (no binding).
        const r = await fetch("http://" + env.TARGET_HOST + "/plain");
        return new Response(r.body, { status: r.status, headers: r.headers });
      }
      case "/object":
        return env.AGENT.getByName("a1").fetch(req);
    }
    return new Response("not found", { status: 404 });
  },
};

// A facet object: its binding calls leave as the object's own node.
export class Agent extends DurableObject {
  async fetch() {
    const r = await this.env.B.fetch("http://x/from-object");
    return Response.json({ fetch: await r.json(), rpc: await attempt(() => this.env.B.whoami()) });
  }
}
