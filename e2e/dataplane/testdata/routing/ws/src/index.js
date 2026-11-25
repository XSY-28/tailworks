// e2e/dataplane routing loop (#422): GET /ws/<room>?c=<conn> upgrades to a
// WebSocket the Echo object accepts with the hibernation API
// (ctx.acceptWebSocket). Each message {s} is answered with {c, s, host,
// epoch, ver, boot}: the connection ID (kept in the socket's attachment, so
// it survives hibernation), the object's host and lease epoch, and its boot
// ID (new after an eviction). Without an upgrade: {host, epoch, ver, boot,
// conns}.
import { DurableObject } from "cloudflare:workers";

async function ident(ctx, env) {
  let s = {};
  try {
    s = await ctx.tailnet.self();
  } catch (e) {
    s = { error: String(e?.message ?? e) };
  }
  const hostname = s.hostname ?? "";
  return { host: hostname.split("-")[0], epoch: s.epoch ?? 0, ver: env.VER };
}

export class Echo extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    this.boot = crypto.randomUUID();
  }
  async fetch(req) {
    const id = await ident(this.ctx, this.env);
    if (req.headers.get("upgrade") !== "websocket") return Response.json({ ...id, boot: this.boot, conns: this.ctx.getWebSockets().length });
    const [client, server] = Object.values(new WebSocketPair());
    this.ctx.acceptWebSocket(server);
    server.serializeAttachment({ c: new URL(req.url).searchParams.get("c") });
    return new Response(null, { status: 101, webSocket: client });
  }
  async webSocketMessage(ws, msg) {
    const { c } = ws.deserializeAttachment();
    let s = -1;
    try {
      s = JSON.parse(msg).s;
    } catch {}
    ws.send(JSON.stringify({ c, s, ...(await ident(this.ctx, this.env)), boot: this.boot }));
  }
  async webSocketClose(ws, code) {
    try {
      ws.close(code, "bye");
    } catch {}
  }
}

export default {
  async fetch(req, env) {
    const [, ws, room] = new URL(req.url).pathname.split("/");
    if (ws !== "ws" || !room) return Response.json({ error: "want /ws/<room>" }, { status: 404 });
    return env.ECHO.getByName(room).fetch(req);
  },
};
