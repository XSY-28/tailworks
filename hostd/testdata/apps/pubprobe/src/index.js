// Test fixture for ADR-029 public ingress: reports what a handler sees for
// public vs tailnet requests, and tries the things public requests must not do.
import { DurableObject, env as rawEnv } from "cloudflare:workers";
import { getCaller, getRequester, isTailnetError } from "@twd/tailnet";

// A handler that caches the env it was given (a pattern the docs warn
// about): a later public request would use a tailnet request's guard.
let CACHED_ENV = null;

const SEEN = (req) =>
  Object.fromEntries([...req.headers].filter(([k]) => k.startsWith("x-twd") || k.startsWith("cf-") || k.startsWith("x-forwarded") || k.startsWith("tailscale-")));

function who(req) {
  let callerError = null;
  try {
    getCaller(req);
  } catch (e) {
    callerError = isTailnetError(e) ? e.code : String(e);
  }
  const r = getRequester(req);
  return { requester: { kind: r.kind, ip: r.ip, proto: r.proto, host: r.host, nodeId: r.nodeId }, callerError };
}

class Base extends DurableObject {
  async fetch(req) {
    return Response.json({ object: this.ctx.id.name, ...who(req) });
  }
  async ping() {
    return this.ctx.id.name;
  }
}
export class Room extends Base {}
export class Open extends Base {}

function stream() {
  const enc = new TextEncoder();
  let n = 0;
  return new Response(new ReadableStream({
    async pull(c) {
      await new Promise((r) => setTimeout(r, 50));
      c.enqueue(enc.encode("tick " + n++ + "\n"));
    },
  }));
}

export default {
  async fetch(req, env, ctx) {
    const url = new URL(req.url);
    const parts = url.pathname.split("/").filter(Boolean);
    if (parts[0] === "hook") return Response.json({ ...who(req), seen: SEEN(req), path: url.pathname });
    if (parts[0] === "agents" && parts.length >= 3) {
      const ns = parts[1] === "room" ? env.ROOM : parts[1] === "open" ? env.OPEN : null;
      if (!ns) return new Response("no such class", { status: 404 });
      return ns.getByName(decodeURIComponent(parts[2])).fetch(req);
    }
    // RT3-06/07: bypasses of the guarded env. None may create an object.
    if (parts[0] === "raw" && parts[1]) {
      // import { env } from "cloudflare:workers": the raw binding, no guard.
      return rawEnv.ROOM.getByName(decodeURIComponent(parts[1])).fetch(req);
    }
    if (parts[0] === "rawrpc" && parts[1]) {
      try {
        return Response.json({ pinged: await rawEnv.ROOM.getByName(decodeURIComponent(parts[1])).ping() });
      } catch (e) {
        return Response.json({ error: String(e?.message ?? e) }, { status: 299 });
      }
    }
    if (parts[0] === "twd") {
      // The platform binding is not in the handler's env; the raw binding
      // still has it, and a spawn through it must be refused by hostd.
      const out = { handlerHas: "__TWD" in env, rawHas: !!rawEnv.__TWD };
      if (rawEnv.__TWD) {
        try {
          out.rawSpawn = await rawEnv.__TWD.call("spawn", { class: "Room", name: "via-twd" });
        } catch (e) {
          out.rawSpawn = { error: String(e) };
        }
      }
      return Response.json(out);
    }
    if (parts[0] === "cache") {
      CACHED_ENV = env;
      return Response.json({ cached: true });
    }
    if (parts[0] === "cached" && parts[1]) {
      if (!CACHED_ENV) return Response.json({ error: "nothing cached" }, { status: 299 });
      return CACHED_ENV.ROOM.getByName(decodeURIComponent(parts[1])).fetch(req);
    }
    if (parts[0] === "spawn") {
      try {
        await ctx.tailnet.spawn(env.ROOM, { name: "spawned-by-public" });
        return Response.json({ spawned: true });
      } catch (e) {
        return Response.json({ code: e.code ?? String(e) }, { status: 299 });
      }
    }
    if (parts[0] === "slow") return stream();
    if (parts[0] === "big") return Response.json({ len: (await req.arrayBuffer()).byteLength });
    if (parts[0] === "private") return Response.json({ private: true, ...who(req) });
    return new Response("not found", { status: 404 });
  },
};
