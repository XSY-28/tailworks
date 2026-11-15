// Platform-injected runtime. twd-hostd's generated shims import this module
// (bundled as __twd_rt.js) to install ctx.tailnet on Durable Object state
// and on the Worker ExecutionContext. Tenant code reaches it as
// `this.ctx.tailnet` / `ctx.tailnet` (RFC §6.1); it never imports it.
//
// Every call goes through `tw`, a platform TailnetApi entrypoint stub whose
// props (minted by the platform, read-only here) say which object or
// workload is calling, so tenant code cannot act as another object.

import { TailnetError, fromResponse, isTailnetError } from "./errors.js";
import { createContainer, defineContainer, dispatchIntercept, notSupportedPreview, type ContainerSpec } from "./container.js";
import { workflowClient } from "./workflows.js";

export { wrapWorkflowClass, isApprovalType } from "./workflows.js";

/** The platform's TailnetApi stub (RPC). */
export interface TailnetApiStub {
  call(op: string, args: Record<string, unknown>): Promise<{ ok: true; value: any } | { ok: false; error: Record<string, any> }>;
}

export interface SpawnOptions {
  name: string;
  /** Must be ⊆ the parent's effective caps (checked by twd-mgmt). */
  caps?: Record<string, unknown>;
  ttl?: string;
  principal?: "inherit" | "none";
  /** How long `ready` waits for the child's node (default 30 s). */
  readyTimeoutMs?: number;
}

export interface Spawned<T = unknown> {
  stub: T;
  /** Node hostname now; the MagicDNS name once ready. */
  name: string;
  objectId: string;
  state(): "pending" | "ready" | "failed";
  ready: Promise<void>;
}

export interface TailnetContext {
  fetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response>;
  forward(request: Request, to: string | URL, init?: RequestInit): Promise<Response>;
  spawn<T = any>(ns: any, opts: SpawnOptions): Promise<Spawned<T>>;
  kill(opts?: { reason?: string; cascade?: boolean; target?: { class: string; name: string } }): Promise<{ killed: string[] }>;
  audit(records: Array<Record<string, unknown>>): Promise<void>;
  self(): Promise<{ objectId: string; hostname: string; magicDNS?: string; status: string; kind: string }>;
  connect(): never;
}

export type BindingMap = Record<string, string>; // env binding name -> DO class

export interface Self {
  class: string;
  name: string;
}

export function createTailnetContext(tw: TailnetApiStub, env: Record<string, any>, bindings: BindingMap, self?: Self, ticket?: string): TailnetContext {
  if (!tw || typeof tw.call !== "function") throw new TailnetError("no_identity", "twd platform binding missing (not running on twd?)");
  const op = async (name: string, args: Record<string, unknown> = {}) => {
    // The request's create ticket admits a Worker-level spawn (hostd checks
    // it); an object's own spawns are attested by `as` instead.
    const a: Record<string, unknown> = self ? { ...args, as: self } : ticket ? { ...args, ticket } : args;
    const r = await tw.call(name, a);
    if (!r || !r.ok) throw TailnetError.from((r as any)?.error);
    return r.value;
  };
  const classOf = (ns: unknown): string => {
    for (const [k, cls] of Object.entries(bindings)) if (env[k] === ns) return cls;
    if (typeof ns === "string" && Object.values(bindings).includes(ns)) return ns;
    throw new TailnetError("bad_request", "spawn: the namespace is not a durable_objects binding of this app");
  };
  const tfetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    let r: Response;
    try {
      r = await fetch(input as any, init);
    } catch (e) {
      throw new TailnetError("dial_failed", String((e as Error)?.message ?? e));
    }
    const err = await fromResponse(r);
    if (err) throw err;
    return r;
  };
  const tn: TailnetContext = {
    fetch: tfetch,
    forward(request: Request, to: string | URL, init?: RequestInit) {
      // MVP: re-issues the request from this node. Signed act-chain
      // assertions (RFC §4.6) are not implemented yet.
      const hasBody = request.method !== "GET" && request.method !== "HEAD";
      return tfetch(new Request(String(to), { method: request.method, headers: request.headers, body: hasBody ? request.body : undefined, ...init }));
    },
    async spawn(ns: any, opts: SpawnOptions) {
      if (!opts || typeof opts.name !== "string" || !opts.name) throw new TailnetError("bad_request", "spawn: opts.name is required");
      const cls = classOf(ns);
      const v = await op("spawn", { class: cls, name: opts.name, caps: opts.caps, ttl: opts.ttl, principal: opts.principal });
      let state: "pending" | "ready" | "failed" = v.status === "ready" ? "ready" : "pending";
      // Await: inside a facet, env.X is an RPC stub and getByName returns an
      // RpcPromise that must not be stored un-awaited.
      const stub = await ns.getByName(opts.name);
      const sp: Spawned<any> = {
        stub,
        name: v.hostname ?? opts.name,
        objectId: v.objectID,
        state: () => state,
        ready: Promise.resolve(),
      };
      sp.ready = op("ready", { class: cls, name: opts.name, timeoutMs: opts.readyTimeoutMs ?? 30000 }).then(
        (r) => {
          state = "ready";
          if (r?.magicDNS) sp.name = r.magicDNS;
        },
        (e) => {
          state = "failed";
          throw e;
        },
      );
      sp.ready.catch(() => {}); // callers may ignore readiness
      return sp;
    },
    kill(opts: { reason?: string; cascade?: boolean; target?: { class: string; name: string } } = {}) {
      return op("kill", { reason: opts.reason ?? "", cascade: !!opts.cascade, target: opts.target });
    },
    async audit(records) {
      await op("audit", { records });
    },
    self() {
      return op("self", {});
    },
    connect(): never {
      throw new TailnetError("plan_feature_missing", "ctx.tailnet.connect() is not implemented yet (raw TCP egress)");
    },
  };
  return Object.freeze(tn);
}

function define(ctx: any, tn: TailnetContext) {
  try {
    Object.defineProperty(ctx, "tailnet", { value: tn, enumerable: false, configurable: true });
  } catch {
    // a frozen ctx: leave ctx.tailnet absent rather than fail the request
  }
}

/**
 * installAlarmShim: workerd facets cannot set alarms ("Facets currently
 * cannot set alarms"), so a facet's ctx.storage alarm methods are delegated
 * to its parent Host DO (a real DO, same object identity), which owns the
 * physical alarm and calls the facet's alarm() when it fires. This is what
 * makes the Agents SDK's schedule()/scheduleEvery() work in facet mode.
 */
function installAlarmShim(ctx: any, tw: TailnetApiStub) {
  const st = ctx?.storage;
  if (!st || !tw) return;
  const call = async (action: string, time?: number) => {
    const r = await tw.call("alarm", { action, time });
    if (!r || !r.ok) throw TailnetError.from((r as any)?.error);
    return r.value;
  };
  const shim: Record<string, (...a: any[]) => Promise<any>> = {
    async setAlarm(t: number | Date) {
      const ms = typeof t === "number" ? t : t instanceof Date ? t.getTime() : Number(t);
      if (!Number.isFinite(ms)) throw new TypeError("setAlarm: invalid time");
      await call("set", ms);
    },
    async getAlarm() {
      return (await call("get")) ?? null;
    },
    async deleteAlarm() {
      await call("delete");
    },
  };
  for (const [k, fn] of Object.entries(shim)) {
    try {
      Object.defineProperty(st, k, { value: fn, configurable: true, writable: true });
    } catch {
      /* non-extensible storage: alarms stay unsupported in this facet */
    }
  }
}

/** wrapFacetClass wraps a tenant DO class run as a facet (per-object egress). */
export function wrapFacetClass(C: any, name: string, bindings: BindingMap, opts: { container?: ContainerSpec; workflows?: Record<string, string> } = {}) {
  if (typeof C !== "function") throw new Error(`twd: durable_objects class ${name} is not exported by the main module`);
  const W = {
    [name]: class extends C {
      constructor(ctx: any, env: any) {
        const tw = env?.__TWD;
        // Bindings admit before they reach an object (chained creation by
        // an attested object; hostd records it), and __TWD stays platform-only.
        const e = guardEnv(env, bindings, "tailnet", admitter(tw, {}));
        // Workflow bindings: the platform's WorkflowGate, as the Workflow API.
        for (const k of Object.keys(opts.workflows ?? {})) if (e?.[k]) e[k] = workflowClient(e[k]);
        define(ctx, createTailnetContext(tw, e, bindings));
        installAlarmShim(ctx, tw);
        // ctx.container: the object is attested by the platform (props).
        if (tw && opts.container) defineContainer(ctx, createContainer(tw, ctx, name, opts.container));
        super(ctx, e);
      }
      // Called by the Host DO when the delegated alarm fires.
      async __twd_alarm(info: unknown) {
        const self = this as any;
        if (typeof self.alarm === "function") return self.alarm(info);
      }
      // Called by the Host DO with a request the container sent to an
      // intercepted destination (hostd's sandbox egress proxy).
      async __twd_intercept(key: string, req: Request) {
        return dispatchIntercept((this as any).ctx, key, req);
      }
    },
  }[name];
  if (opts.container) noPreviewURLs(W);
  return W;
}

// Sandbox SDK preview URLs (exposePort, proxyToSandbox) need public
// ingress (ADR-029): on twd they fail with a typed not_supported error
// instead of minting URLs nothing can reach.
const PREVIEW_HEADER = "x-sandbox-preview-proxy";
function noPreviewURLs(W: any) {
  const proto = Object.getPrototypeOf(W.prototype);
  if (typeof proto?.exposePort === "function") {
    W.prototype.exposePort = async function () {
      throw notSupportedPreview();
    };
  }
  const f = W.prototype.fetch;
  if (typeof f === "function") {
    W.prototype.fetch = async function (this: any, req: Request) {
      if (req?.headers?.get?.(PREVIEW_HEADER)) {
        const e = notSupportedPreview();
        return new Response(JSON.stringify({ code: "not_supported", message: e.message }), {
          status: 501,
          headers: { "content-type": "application/json", "twd-error": "not_supported" },
        });
      }
      return f.call(this, req);
    };
  }
}

/**
 * wrapPlainClass wraps a tenant DO class run as a plain DO (egress:
 * "workload"): alarms work, egress leaves as the workload node. The wrapper
 * tells hostd the object exists (so its node comes up) and enforces kill.
 */
export function wrapPlainClass(C: any, name: string, bindings: BindingMap, opts: { node: boolean; container?: ContainerSpec }) {
  if (typeof C !== "function") throw new Error(`twd: durable_objects class ${name} is not exported by the main module`);
  const proto = C.prototype;
  const killedResponse = (reason: string) =>
    new TailnetError("killed", `object ${name} was killed: ${reason}`).toResponse();
  const W: any = {
    [name]: class extends C {
      constructor(ctx: any, env: any) {
        const tw = env?.__TWD;
        const oname = ctx?.id?.name;
        const self = oname ? { class: name, name: oname } : undefined;
        const e = guardEnv(env, bindings, "tailnet", admitter(tw, self ? { as: self } : {}));
        if (tw && oname) define(ctx, createTailnetContext(tw, e, bindings, self));
        if (tw && oname && opts.container) defineContainer(ctx, createContainer(tw, ctx, name, opts.container, { class: name, name: oname }));
        super(ctx, e);
        if (tw && oname && opts.node) {
          // The object's node comes up only if its creation was admitted
          // (RT3-06/07); fetch answers the refusal below.
          const p = tw.call("ensure", { as: { class: name, name: oname } }).then((r) => {
            if (r && !r.ok) {
              const err = TailnetError.from((r as any).error);
              if (err.code === "create_not_admitted") (this as any).__twd_denied = err;
            }
          }, () => {});
          (this as any).__twd_ensure = p;
          try {
            ctx.waitUntil?.(p);
          } catch {
            /* ignore */
          }
        }
      }
      async __twd_kill(reason: string) {
        (this as any).ctx.storage.kv.put("__twd_killed", reason || "killed");
        return true;
      }
      async __twd_intercept(key: string, req: Request) {
        const self = this as any;
        const k = self.ctx.storage.kv.get("__twd_killed");
        if (k) return killedResponse(k);
        return dispatchIntercept(self.ctx, key, req);
      }
    },
  }[name];
  if (typeof proto.fetch === "function") {
    W.prototype.fetch = async function (this: any, req: Request) {
      const k = this.ctx.storage.kv.get("__twd_killed");
      if (k) return killedResponse(k);
      if (this.__twd_ensure) await this.__twd_ensure;
      if (this.__twd_denied) return (this.__twd_denied as TailnetError).toResponse();
      return proto.fetch.call(this, req);
    };
  }
  if (opts.container) noPreviewURLs(W);
  return W;
}

// ---- create admission (RT3-06/07; public requests: ADR-029 D5) ----
//
// Every new object name mints a tailnet node, so creating one is a platform
// decision: hostd "admit". Tenant code reaches Durable Objects only through
// a guarded env whose namespaces admit a name before any call reaches it:
//   - a tailnet request admits with its create ticket (cf.twd.ticket, which
//     only hostd sets and verifies);
//   - an object admits as itself (attested by the platform; chained
//     creation is the app's logic);
//   - a public request (request.cf.tailscale.requester.kind === "public",
//     set only by netd) admits only names its class opens with
//     public.create, under its cap; its ctx.tailnet cannot spawn or kill.
// A raw binding (import { env } from "cloudflare:workers", an env cached in
// a global, __TWD itself) admits nothing: hostd refuses to create for it
// (create_not_admitted), so bypassing the guard loses the ability to mint
// nodes rather than gaining it. Existing objects are reachable either way.

function isPublicReq(req: any): boolean {
  return req?.cf?.tailscale?.requester?.kind === "public";
}

type Admitter = (cls: string, name: string) => Promise<{ ok: boolean; error?: Record<string, any> } | undefined>;

/** admitter builds the admit call for one env: extra is {ticket} for a request, {as} for a plain object, {} for a facet. */
function admitter(tw: TailnetApiStub | undefined, extra: Record<string, unknown>): Admitter {
  return async (cls, name) => {
    if (!tw || typeof tw.call !== "function") return { ok: false, error: { code: "create_not_admitted", message: "twd platform binding missing; refusing to reach a Durable Object" } };
    return tw.call("admit", { class: cls, name, ...extra });
  };
}

const ADMIT_CACHE_MAX = 10_000;
// "class\nname" of objects hostd admitted in this isolate (bounded; an
// admitted object stays admitted: a later kill answers 410 on its own).
const admitted = new Map<string, true>();

function remember(key: string) {
  if (admitted.size >= ADMIT_CACHE_MAX) {
    const first = admitted.keys().next().value;
    if (first !== undefined) admitted.delete(first);
  }
  admitted.set(key, true);
}

async function admit(adm: Admitter, cls: string, name: string): Promise<void> {
  const key = cls + "\n" + name;
  if (admitted.has(key)) return;
  const r = await adm(cls, name);
  if (!r || !r.ok) throw TailnetError.from((r as any)?.error ?? { code: "create_not_admitted", message: "admit refused" });
  remember(key);
}

function denied(msg: string): never {
  throw new TailnetError("public_create_denied", msg);
}

function guardStub(adm: Admitter, cls: string, name: string, real: any): any {
  return new Proxy(
    {},
    {
      get(_t, p) {
        if (p === "then") return undefined;
        if (p === "id" || p === "name") return real[p];
        if (typeof p === "symbol") return real[p];
        return async (...args: unknown[]) => {
          await admit(adm, cls, name);
          // Inside a facet, env.X is an NsProxy stub and getByName returns an
          // RpcPromise: RPC methods pipeline on it, fetch() does not, so
          // settle it first.
          const target = real && typeof real.then === "function" ? await real : real;
          return target[p](...args);
        };
      },
    },
  );
}

type GuardMode = "tailnet" | "public";

function guardNamespace(adm: Admitter, cls: string, ns: any, mode: GuardMode): any {
  return Object.freeze({
    getByName(name: unknown) {
      const n = String(name);
      return guardStub(adm, cls, n, ns.getByName(n));
    },
    idFromName(name: unknown) {
      return ns.idFromName(name);
    },
    idFromString(s: unknown) {
      return ns.idFromString(s);
    },
    get(id: any, opts?: unknown) {
      const n = id?.name;
      if (typeof n !== "string" || !n) {
        if (mode === "public") denied(`a public request may only address ${cls} objects by name (idFromName/getByName)`);
        return ns.get(id, opts); // a nameless object gets no node: nothing to admit
      }
      return guardStub(adm, cls, n, ns.get(id, opts));
    },
    newUniqueId(opts?: unknown) {
      if (mode === "public") denied(`a public request may not create a new ${cls} object (newUniqueId)`);
      return ns.newUniqueId(opts); // no name, no node
    },
    jurisdiction(j: unknown) {
      return guardNamespace(adm, cls, ns.jurisdiction(j), mode);
    },
  });
}

/** guardEnv returns env with every DO binding guarded and the platform binding removed. */
function guardEnv(env: any, bindings: BindingMap, mode: GuardMode, adm: Admitter): any {
  if (!env || typeof env !== "object") return env;
  const out: Record<string, unknown> = { ...env };
  delete out.__TWD;
  for (const [k, cls] of Object.entries(bindings)) {
    if (env[k]) out[k] = guardNamespace(adm, cls, env[k], mode);
  }
  return out;
}

/** guardTailnetEnv guards env as for a tailnet caller without a create ticket (Workflows). */
export function guardTailnetEnv(env: any, bindings: BindingMap): any {
  return guardEnv(env, bindings, "tailnet", admitter(env?.__TWD, {}));
}

/** publicEnv returns env guarded for a public request (kept for callers of the old name). */
export function publicEnv(env: any, bindings: BindingMap): any {
  return guardEnv(env, bindings, "public", admitter(env?.__TWD, {}));
}

/** publicTailnet is ctx.tailnet for a public request: no spawn, no kill. */
function publicTailnet(tn: TailnetContext): TailnetContext {
  return Object.freeze({
    ...tn,
    spawn(): Promise<never> {
      return Promise.reject(new TailnetError("public_create_denied", "a public request may not spawn objects"));
    },
    kill(): Promise<never> {
      return Promise.reject(new TailnetError("public_request", "a public request may not kill objects"));
    },
  }) as TailnetContext;
}

// asResponse renders errors the platform throws through tenant code.
function asResponse(e: unknown): Response | null {
  const m = String((e as Error)?.message ?? e);
  const i = m.indexOf("[killed]");
  if (i >= 0) return new TailnetError("killed", m.slice(i + 8).trim()).toResponse();
  const j = m.indexOf("[create_not_admitted]");
  if (j >= 0) return new TailnetError("create_not_admitted", m.slice(j + 21).trim()).toResponse();
  if (isTailnetError(e) && (e.code === "public_create_denied" || e.code === "public_request" || e.code === "create_not_admitted")) return e.toResponse();
  return null;
}

/** requestTicket is the create ticket hostd stamped on a tailnet request or cron run. */
function requestTicket(req: any): string | undefined {
  const t = req?.cf?.twd?.ticket;
  return typeof t === "string" && t ? t : undefined;
}

/** isScheduledReq: the platform dispatching a cron run (cf.twd.kind "scheduled", set by hostd). */
function isScheduledReq(req: any): boolean {
  return req?.cf?.twd?.kind === "scheduled";
}

/**
 * runScheduled dispatches a cron run to the handler's scheduled() with a
 * controller (cron, scheduledTime, type, noRetry()) and answers the
 * platform's {outcome, noRetry, error} record.
 */
async function runScheduled(scheduledFn: ((c: any, ...rest: any[]) => unknown) | undefined, req: any, extra: unknown[]): Promise<Response> {
  const t = req.cf.twd;
  let noRetry = false;
  const controller = {
    cron: String(t.cron ?? ""),
    scheduledTime: Number(t.scheduledTime),
    type: "scheduled",
    noRetry() {
      noRetry = true;
    },
  };
  if (typeof scheduledFn !== "function") {
    return Response.json({ outcome: "exception", noRetry: false, error: "the default export has no scheduled() handler" });
  }
  try {
    await scheduledFn(controller, ...extra);
    return Response.json({ outcome: "ok", noRetry });
  } catch (e) {
    return Response.json({ outcome: "exception", noRetry, error: String((e as any)?.message ?? e).slice(0, 512) });
  }
}

/** wrapHandler installs ctx.tailnet (workload identity) on the default export. */
export function wrapHandler(H: any, bindings: BindingMap) {
  if (!H) {
    return {
      fetch: () => new TailnetError("unknown_object", "this app has no default export (Worker); address objects via their own nodes or /_twd/o/<Class>/<name>/").toResponse(),
    };
  }
  if (typeof H === "function") {
    // A WorkerEntrypoint class (one instance per request).
    const C = class extends H {
      constructor(ctx: any, env: any) {
        const tw = env?.__TWD;
        const e = guardEnv(env, bindings, "tailnet", admitter(tw, {}));
        define(ctx, createTailnetContext(tw, e, bindings));
        super(ctx, e);
        (this as any).__twd = tw;
      }
    };
    if (typeof H.prototype?.fetch === "function") {
      const f = H.prototype.fetch;
      (C.prototype as any).fetch = async function (this: any, req: Request) {
        const tw: TailnetApiStub | undefined = this.__twd;
        if (isPublicReq(req)) {
          this.env = guardEnv(this.env, bindings, "public", admitter(tw, {}));
          if (tw) define(this.ctx, publicTailnet(createTailnetContext(tw, this.env, bindings)));
        } else {
          const ticket = requestTicket(req);
          this.env = guardEnv(this.env, bindings, "tailnet", admitter(tw, { ticket }));
          if (tw) define(this.ctx, createTailnetContext(tw, this.env, bindings, undefined, ticket));
          if (isScheduledReq(req)) {
            const sf = typeof this.scheduled === "function" ? this.scheduled.bind(this) : undefined;
            return runScheduled(sf, req, []);
          }
        }
        try {
          return await f.call(this, req);
        } catch (e) {
          const r = asResponse(e);
          if (r) return r;
          throw e;
        }
      };
    }
    return { [H.name || "Default"]: C }[H.name || "Default"];
  }
  const out: Record<string, unknown> = { ...H };
  for (const k of Object.keys(H)) {
    if (typeof H[k] !== "function") continue;
    out[k] = function (this: unknown, a: unknown, env: any, ctx: any) {
      const tw: TailnetApiStub | undefined = env?.__TWD;
      const pub = k === "fetch" && isPublicReq(a);
      if (pub) {
        env = guardEnv(env, bindings, "public", admitter(tw, {}));
        if (ctx && tw) define(ctx, publicTailnet(createTailnetContext(tw, env, bindings)));
      } else {
        // The create ticket rides on the request (a cron run is dispatched
        // as a fetch with cf.twd.kind "scheduled" and its own ticket).
        const ticket = k === "fetch" ? requestTicket(a) : undefined;
        env = guardEnv(env, bindings, "tailnet", admitter(tw, { ticket }));
        if (ctx && tw) define(ctx, createTailnetContext(tw, env, bindings, undefined, ticket));
        if (k === "fetch" && isScheduledReq(a)) return runScheduled(H.scheduled, a, [env, ctx]);
      }
      let r: unknown;
      try {
        r = H[k].call(H, a, env, ctx);
      } catch (e) {
        const resp = k === "fetch" ? asResponse(e) : null;
        if (resp) return resp;
        throw e;
      }
      if (k !== "fetch") return r;
      // A call into a killed object throws "[killed] …" from the platform;
      // answer the caller with a typed 410 instead of an opaque 500. A
      // public create refusal that escapes the handler becomes its 403.
      return Promise.resolve(r).catch((e: unknown) => {
        const resp = asResponse(e);
        if (resp) return resp;
        throw e;
      });
    };
  }
  return out;
}
