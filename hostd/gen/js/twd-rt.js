// js/packages/tailnet/src/errors.ts
var DOC_BASE = "https://github.com/rosskukulinski/twd/blob/main/docs/errors.md#";
var CATALOG = Object.freeze({
  peer_not_visible: { status: 403, title: "the destination is not in this node's netmap (a grant is missing, or the name does not exist)" },
  forbidden_destination: { status: 403, title: "the destination class is refused by netd (loopback, self, sibling, host, non-tailnet)" },
  acl_denied: { status: 403, title: "the peer is visible but the port or service is not granted" },
  cap_missing: { status: 403, title: "the caller lacks the app capability (or the tool) required" },
  cap_limit_exceeded: { status: 403, title: "within the granted tool, but above a numeric limit or budget" },
  delegation_invalid: { status: 403, title: "the on-behalf-of assertion failed verification" },
  delegation_exceeds_parent: { status: 403, title: "spawn asked for caps the parent does not have" },
  spawn_budget_exceeded: { status: 429, title: "too many live descendants or too deep a spawn chain" },
  bootstrap_failed: { status: 502, title: "the object's node could not be created (mint, token exchange or join failed)" },
  identity_pending: { status: 503, title: "the object's node is not ready yet" },
  derp_unreachable: { status: 502, title: "no direct path and no reachable DERP relay" },
  plan_feature_missing: { status: 501, title: "the tailnet's plan or control server lacks a feature twd can use" },
  killed: { status: 410, title: "the object was killed" },
  object_killed: { status: 410, title: "a new request reached a killed object (its node's tombstone answered)" },
  no_identity: { status: 401, title: "the request carries no verified tailnet caller" },
  lease_lost: { status: 409, title: "this host no longer holds the namespace lease" },
  namespace_conflict: { status: 409, title: "mgmt returned another identity's object for this workload's class + object name (an invariant violation; twd fails closed rather than share a node)" },
  bad_request: { status: 400, title: "the request is malformed" },
  unknown_object: { status: 404, title: "no such object" },
  dial_failed: { status: 502, title: "the tailnet dial failed" },
  upstream_unavailable: { status: 503, title: "the platform upstream is unavailable" },
  hostd_unavailable: { status: 503, title: "the host agent is unavailable" },
  public_request: { status: 403, title: "a public (Funnel) request reached code that needs a tailnet caller; use getRequester()" },
  public_create_denied: { status: 403, title: "a public request may not create a new object (or spawn); only existing objects, or classes with public.create" },
  create_not_admitted: { status: 403, title: "the object does not exist and nothing admitted its creation: reach new objects through the env twd hands your handler (not a raw or cached binding, not from a public request)" },
  public_ingress_not_allowed: { status: 403, title: "the tenant does not allow public ingress (operator setting publicIngress)" },
  public_name_taken: { status: 409, title: "the workload's public node name is taken on the tailnet (MagicDNS would rename it)" },
  rate_limited: { status: 429, title: "too many public requests (per workload or per client IP)" },
  payload_too_large: { status: 413, title: "the public request body is above the workload's limit" },
  header_too_large: { status: 431, title: "the public request headers are above the workload's limit" },
  not_public: { status: 404, title: "this path is not one of the workload's public routes" },
  upgrade_not_allowed: { status: 403, title: "connection upgrades (WebSocket) are not allowed on this public route" }
});
var BRAND = /* @__PURE__ */ Symbol.for("twd.TailnetError");
var TailnetError = class _TailnetError extends Error {
  code;
  missing;
  snippet;
  docUrl;
  traceId;
  status;
  /** Human-readable detail (message may be JSON; see @twd/mcp). */
  detail;
  constructor(code, message, init = {}) {
    super(message ?? CATALOG[code]?.title ?? code);
    this.name = "TailnetError";
    this.code = code;
    this.detail = message ?? this.message;
    const { missing, snippet, docUrl, traceId, status, ...rest } = init;
    Object.assign(this, rest);
    this.missing = missing;
    this.snippet = snippet;
    this.docUrl = docUrl ?? DOC_BASE + code;
    this.traceId = traceId ?? "";
    this.status = status ?? CATALOG[code]?.status ?? 500;
    Object.defineProperty(this, BRAND, { value: true });
  }
  /** The wire body (DX.md shape). */
  toJSON() {
    const o = { code: this.code, message: this.detail, doc_url: this.docUrl };
    if (this.missing) o.missing = this.missing;
    if (this.snippet) o.snippet = this.snippet;
    if (this.traceId) o.trace_id = this.traceId;
    for (const k of ["have", "chain", "limit", "used", "object", "destination"]) if (this[k] !== void 0) o[k] = this[k];
    return o;
  }
  /** Render as an HTTP response with the twd-error header. */
  toResponse() {
    return new Response(JSON.stringify(this.toJSON()), {
      status: this.status,
      headers: { "content-type": "application/json", "twd-error": String(this.code), "cache-control": "no-store" }
    });
  }
  /** Build from a wire body ({code, message|detail, ...}). */
  static from(body, status) {
    const b = body ?? {};
    const code = typeof b.code === "string" && b.code ? b.code : "upstream_unavailable";
    const { code: _c, message, detail, doc_url, trace_id, missing, snippet, ...rest } = b;
    return new _TailnetError(code, detail ?? message ?? code, { ...rest, missing, snippet, docUrl: doc_url, traceId: trace_id, status });
  }
};
function isTailnetError(e) {
  return !!e && typeof e === "object" && (e[BRAND] === true || e.name === "TailnetError");
}
async function fromResponse(r) {
  const code = r.headers.get("twd-error");
  if (!code) return null;
  let body = { code };
  try {
    body = { code, ...await r.clone().json() };
  } catch {
  }
  return TailnetError.from(body, r.status);
}

// js/packages/tailnet/src/runtime.ts
function createTailnetContext(tw, env, bindings, self, ticket) {
  if (!tw || typeof tw.call !== "function") throw new TailnetError("no_identity", "twd platform binding missing (not running on twd?)");
  const op = async (name, args = {}) => {
    const a = self ? { ...args, as: self } : ticket ? { ...args, ticket } : args;
    const r = await tw.call(name, a);
    if (!r || !r.ok) throw TailnetError.from(r?.error);
    return r.value;
  };
  const classOf = (ns) => {
    for (const [k, cls] of Object.entries(bindings)) if (env[k] === ns) return cls;
    if (typeof ns === "string" && Object.values(bindings).includes(ns)) return ns;
    throw new TailnetError("bad_request", "spawn: the namespace is not a durable_objects binding of this app");
  };
  const tfetch = async (input, init) => {
    let r;
    try {
      r = await fetch(input, init);
    } catch (e) {
      throw new TailnetError("dial_failed", String(e?.message ?? e));
    }
    const err = await fromResponse(r);
    if (err) throw err;
    return r;
  };
  const tn = {
    fetch: tfetch,
    forward(request, to, init) {
      const hasBody = request.method !== "GET" && request.method !== "HEAD";
      return tfetch(new Request(String(to), { method: request.method, headers: request.headers, body: hasBody ? request.body : void 0, ...init }));
    },
    async spawn(ns, opts) {
      if (!opts || typeof opts.name !== "string" || !opts.name) throw new TailnetError("bad_request", "spawn: opts.name is required");
      const cls = classOf(ns);
      const v = await op("spawn", { class: cls, name: opts.name, caps: opts.caps, ttl: opts.ttl, principal: opts.principal });
      let state = v.status === "ready" ? "ready" : "pending";
      const stub = await ns.getByName(opts.name);
      const sp = {
        stub,
        name: v.hostname ?? opts.name,
        objectId: v.objectID,
        state: () => state,
        ready: Promise.resolve()
      };
      sp.ready = op("ready", { class: cls, name: opts.name, timeoutMs: opts.readyTimeoutMs ?? 3e4 }).then(
        (r) => {
          state = "ready";
          if (r?.magicDNS) sp.name = r.magicDNS;
        },
        (e) => {
          state = "failed";
          throw e;
        }
      );
      sp.ready.catch(() => {
      });
      return sp;
    },
    kill(opts = {}) {
      return op("kill", { reason: opts.reason ?? "", cascade: !!opts.cascade, target: opts.target });
    },
    async audit(records) {
      await op("audit", { records });
    },
    self() {
      return op("self", {});
    },
    connect() {
      throw new TailnetError("plan_feature_missing", "ctx.tailnet.connect() is not implemented yet (raw TCP egress)");
    }
  };
  return Object.freeze(tn);
}
function define(ctx, tn) {
  try {
    Object.defineProperty(ctx, "tailnet", { value: tn, enumerable: false, configurable: true });
  } catch {
  }
}
function installAlarmShim(ctx, tw) {
  const st = ctx?.storage;
  if (!st || !tw) return;
  const call = async (action, time) => {
    const r = await tw.call("alarm", { action, time });
    if (!r || !r.ok) throw TailnetError.from(r?.error);
    return r.value;
  };
  const shim = {
    async setAlarm(t) {
      const ms = typeof t === "number" ? t : t instanceof Date ? t.getTime() : Number(t);
      if (!Number.isFinite(ms)) throw new TypeError("setAlarm: invalid time");
      await call("set", ms);
    },
    async getAlarm() {
      return await call("get") ?? null;
    },
    async deleteAlarm() {
      await call("delete");
    }
  };
  for (const [k, fn] of Object.entries(shim)) {
    try {
      Object.defineProperty(st, k, { value: fn, configurable: true, writable: true });
    } catch {
    }
  }
}
function wrapFacetClass(C, name, bindings) {
  if (typeof C !== "function") throw new Error(`twd: durable_objects class ${name} is not exported by the main module`);
  const W = {
    [name]: class extends C {
      constructor(ctx, env) {
        const tw = env?.__TWD;
        const e = guardEnv(env, bindings, "tailnet", admitter(tw, {}));
        define(ctx, createTailnetContext(tw, e, bindings));
        installAlarmShim(ctx, tw);
        super(ctx, e);
      }
      // Called by the Host DO when the delegated alarm fires.
      async __twd_alarm(info) {
        const self = this;
        if (typeof self.alarm === "function") return self.alarm(info);
      }
    }
  }[name];
  return W;
}
function wrapPlainClass(C, name, bindings, opts) {
  if (typeof C !== "function") throw new Error(`twd: durable_objects class ${name} is not exported by the main module`);
  const proto = C.prototype;
  const killedResponse = (reason) => new TailnetError("killed", `object ${name} was killed: ${reason}`).toResponse();
  const W = {
    [name]: class extends C {
      constructor(ctx, env) {
        const tw = env?.__TWD;
        const oname = ctx?.id?.name;
        const self = oname ? { class: name, name: oname } : void 0;
        const e = guardEnv(env, bindings, "tailnet", admitter(tw, self ? { as: self } : {}));
        if (tw && oname) define(ctx, createTailnetContext(tw, e, bindings, self));
        super(ctx, e);
        if (tw && oname && opts.node) {
          const p = tw.call("ensure", { as: { class: name, name: oname } }).then((r) => {
            if (r && !r.ok) {
              const err = TailnetError.from(r.error);
              if (err.code === "create_not_admitted") this.__twd_denied = err;
            }
          }, () => {
          });
          this.__twd_ensure = p;
          try {
            ctx.waitUntil?.(p);
          } catch {
          }
        }
      }
      async __twd_kill(reason) {
        this.ctx.storage.kv.put("__twd_killed", reason || "killed");
        return true;
      }
    }
  }[name];
  if (typeof proto.fetch === "function") {
    W.prototype.fetch = async function(req) {
      const k = this.ctx.storage.kv.get("__twd_killed");
      if (k) return killedResponse(k);
      if (this.__twd_ensure) await this.__twd_ensure;
      if (this.__twd_denied) return this.__twd_denied.toResponse();
      return proto.fetch.call(this, req);
    };
  }
  return W;
}
function isPublicReq(req) {
  return req?.cf?.tailscale?.requester?.kind === "public";
}
function admitter(tw, extra) {
  return async (cls, name) => {
    if (!tw || typeof tw.call !== "function") return { ok: false, error: { code: "create_not_admitted", message: "twd platform binding missing; refusing to reach a Durable Object" } };
    return tw.call("admit", { class: cls, name, ...extra });
  };
}
var ADMIT_CACHE_MAX = 1e4;
var admitted = /* @__PURE__ */ new Map();
function remember(key) {
  if (admitted.size >= ADMIT_CACHE_MAX) {
    const first = admitted.keys().next().value;
    if (first !== void 0) admitted.delete(first);
  }
  admitted.set(key, true);
}
async function admit(adm, cls, name) {
  const key = cls + "\n" + name;
  if (admitted.has(key)) return;
  const r = await adm(cls, name);
  if (!r || !r.ok) throw TailnetError.from(r?.error ?? { code: "create_not_admitted", message: "admit refused" });
  remember(key);
}
function denied(msg) {
  throw new TailnetError("public_create_denied", msg);
}
function guardStub(adm, cls, name, real) {
  return new Proxy(
    {},
    {
      get(_t, p) {
        if (p === "then") return void 0;
        if (p === "id" || p === "name") return real[p];
        if (typeof p === "symbol") return real[p];
        return async (...args) => {
          await admit(adm, cls, name);
          const target = real && typeof real.then === "function" ? await real : real;
          return target[p](...args);
        };
      }
    }
  );
}
function guardNamespace(adm, cls, ns, mode) {
  return Object.freeze({
    getByName(name) {
      const n = String(name);
      return guardStub(adm, cls, n, ns.getByName(n));
    },
    idFromName(name) {
      return ns.idFromName(name);
    },
    idFromString(s) {
      return ns.idFromString(s);
    },
    get(id, opts) {
      const n = id?.name;
      if (typeof n !== "string" || !n) {
        if (mode === "public") denied(`a public request may only address ${cls} objects by name (idFromName/getByName)`);
        return ns.get(id, opts);
      }
      return guardStub(adm, cls, n, ns.get(id, opts));
    },
    newUniqueId(opts) {
      if (mode === "public") denied(`a public request may not create a new ${cls} object (newUniqueId)`);
      return ns.newUniqueId(opts);
    },
    jurisdiction(j) {
      return guardNamespace(adm, cls, ns.jurisdiction(j), mode);
    }
  });
}
function guardEnv(env, bindings, mode, adm) {
  if (!env || typeof env !== "object") return env;
  const out = { ...env };
  delete out.__TWD;
  for (const [k, cls] of Object.entries(bindings)) {
    if (env[k]) out[k] = guardNamespace(adm, cls, env[k], mode);
  }
  return out;
}
function publicEnv(env, bindings) {
  return guardEnv(env, bindings, "public", admitter(env?.__TWD, {}));
}
function publicTailnet(tn) {
  return Object.freeze({
    ...tn,
    spawn() {
      return Promise.reject(new TailnetError("public_create_denied", "a public request may not spawn objects"));
    },
    kill() {
      return Promise.reject(new TailnetError("public_request", "a public request may not kill objects"));
    }
  });
}
function asResponse(e) {
  const m = String(e?.message ?? e);
  const i = m.indexOf("[killed]");
  if (i >= 0) return new TailnetError("killed", m.slice(i + 8).trim()).toResponse();
  const j = m.indexOf("[create_not_admitted]");
  if (j >= 0) return new TailnetError("create_not_admitted", m.slice(j + 21).trim()).toResponse();
  if (isTailnetError(e) && (e.code === "public_create_denied" || e.code === "public_request" || e.code === "create_not_admitted")) return e.toResponse();
  return null;
}
function requestTicket(req) {
  const t = req?.cf?.twd?.ticket;
  return typeof t === "string" && t ? t : void 0;
}
function isScheduledReq(req) {
  return req?.cf?.twd?.kind === "scheduled";
}
async function runScheduled(scheduledFn, req, extra) {
  const t = req.cf.twd;
  let noRetry = false;
  const controller = {
    cron: String(t.cron ?? ""),
    scheduledTime: Number(t.scheduledTime),
    type: "scheduled",
    noRetry() {
      noRetry = true;
    }
  };
  if (typeof scheduledFn !== "function") {
    return Response.json({ outcome: "exception", noRetry: false, error: "the default export has no scheduled() handler" });
  }
  try {
    await scheduledFn(controller, ...extra);
    return Response.json({ outcome: "ok", noRetry });
  } catch (e) {
    return Response.json({ outcome: "exception", noRetry, error: String(e?.message ?? e).slice(0, 512) });
  }
}
function wrapHandler(H, bindings) {
  if (!H) {
    return {
      fetch: () => new TailnetError("unknown_object", "this app has no default export (Worker); address objects via their own nodes or /_twd/o/<Class>/<name>/").toResponse()
    };
  }
  if (typeof H === "function") {
    const C = class extends H {
      constructor(ctx, env) {
        const tw = env?.__TWD;
        const e = guardEnv(env, bindings, "tailnet", admitter(tw, {}));
        define(ctx, createTailnetContext(tw, e, bindings));
        super(ctx, e);
        this.__twd = tw;
      }
    };
    if (typeof H.prototype?.fetch === "function") {
      const f = H.prototype.fetch;
      C.prototype.fetch = async function(req) {
        const tw = this.__twd;
        if (isPublicReq(req)) {
          this.env = guardEnv(this.env, bindings, "public", admitter(tw, {}));
          if (tw) define(this.ctx, publicTailnet(createTailnetContext(tw, this.env, bindings)));
        } else {
          const ticket = requestTicket(req);
          this.env = guardEnv(this.env, bindings, "tailnet", admitter(tw, { ticket }));
          if (tw) define(this.ctx, createTailnetContext(tw, this.env, bindings, void 0, ticket));
          if (isScheduledReq(req)) {
            const sf = typeof this.scheduled === "function" ? this.scheduled.bind(this) : void 0;
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
  const out = { ...H };
  for (const k of Object.keys(H)) {
    if (typeof H[k] !== "function") continue;
    out[k] = function(a, env, ctx) {
      const tw = env?.__TWD;
      const pub = k === "fetch" && isPublicReq(a);
      if (pub) {
        env = guardEnv(env, bindings, "public", admitter(tw, {}));
        if (ctx && tw) define(ctx, publicTailnet(createTailnetContext(tw, env, bindings)));
      } else {
        const ticket = k === "fetch" ? requestTicket(a) : void 0;
        env = guardEnv(env, bindings, "tailnet", admitter(tw, { ticket }));
        if (ctx && tw) define(ctx, createTailnetContext(tw, env, bindings, void 0, ticket));
        if (k === "fetch" && isScheduledReq(a)) return runScheduled(H.scheduled, a, [env, ctx]);
      }
      let r;
      try {
        r = H[k].call(H, a, env, ctx);
      } catch (e) {
        const resp = k === "fetch" ? asResponse(e) : null;
        if (resp) return resp;
        throw e;
      }
      if (k !== "fetch") return r;
      return Promise.resolve(r).catch((e) => {
        const resp = asResponse(e);
        if (resp) return resp;
        throw e;
      });
    };
  }
  return out;
}
export {
  createTailnetContext,
  publicEnv,
  wrapFacetClass,
  wrapHandler,
  wrapPlainClass
};
