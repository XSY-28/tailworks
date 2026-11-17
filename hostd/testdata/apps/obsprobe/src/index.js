// Test fixture for observability (ADR-045): console output,
// exceptions, a DO call from the Worker and egress from the DO, so the Go
// tests can check request records, metrics, OTLP export and traceparent
// propagation. Deliberately without nodejs_compat (tailworks adds nodejs_als).
import { DurableObject } from "cloudflare:workers";

async function echo(env, from) {
  // The echo node answers with the headers it received.
  const r = await fetch(env.UPSTREAM + "?from=" + from);
  return r.json();
}

export class Obj extends DurableObject {
  async fetch(req) {
    const name = this.ctx.id.name;
    console.log("in Obj", name);
    if (new URL(req.url).pathname.endsWith("/boom")) throw new Error("obj boom " + name);
    const e = await echo(this.env, "obj");
    return Response.json({ name, traceparent: req.headers.get("traceparent"), echoTraceparent: e.headers?.Traceparent?.[0] ?? null });
  }
}

export class Plain extends DurableObject {
  async fetch(req) {
    console.warn("in Plain", this.ctx.id.name);
    const e = await echo(this.env, "plain");
    return Response.json({ name: this.ctx.id.name, traceparent: req.headers.get("traceparent"), echoTraceparent: e.headers?.Traceparent?.[0] ?? null });
  }
}

export default {
  async fetch(req, env) {
    const u = new URL(req.url);
    const seen = req.headers.get("traceparent");
    if (u.pathname === "/throw") {
      console.error("about to throw");
      throw new Error("worker boom");
    }
    const m = u.pathname.match(/^\/(obj|plain)\/([^/]+)$/);
    if (m) {
      console.log("calling", m[1], m[2]);
      // A new Request: the runtime gives the DO call its own child span.
      const ns = m[1] === "obj" ? env.OBJ : env.PLAIN;
      const r = await ns.getByName(m[2]).fetch(new Request("http://do/" + m[2]));
      return Response.json({ worker: seen, do: await r.json() });
    }
    if (u.pathname === "/egress") {
      const e = await echo(env, "worker");
      return Response.json({ worker: seen, echoTraceparent: e.headers?.Traceparent?.[0] ?? null });
    }
    console.log("hello", u.pathname, "secret=" + (u.searchParams.get("token") ? "[given]" : "none"));
    return Response.json({ worker: seen });
  },
};
