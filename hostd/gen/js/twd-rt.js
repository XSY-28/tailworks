// js/packages/tailnet/src/errors.ts
var DOC_BASE = "https://github.com/rosskukulinski/twd/blob/master/docs/errors.md#";
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

// js/packages/tailnet/src/container.ts
var NOT_LISTENING = "the container is not listening";
var NOT_RUNNING = "the container is not running, consider calling start()";
var UNEXPECTED_EXIT = "Container exited with unexpected exit code: ";
var RUNTIME_SIGNALLED = "Runtime signalled the container to exit: ";
var STATE_KEY = "__twd_container";
var handlers = /* @__PURE__ */ new WeakMap();
async function dispatchIntercept(ctx, key, req) {
  const h = handlers.get(ctx)?.get(key);
  const fetcher = typeof h === "function" ? { fetch: h } : h;
  if (!fetcher || typeof fetcher.fetch !== "function") {
    return new Response(
      JSON.stringify({ code: "intercept_unavailable", message: "no handler for " + key + " in this object (it was registered before the object restarted; register it again)" }),
      { status: 502, headers: { "content-type": "application/json", "twd-error": "intercept_unavailable" } }
    );
  }
  return await fetcher.fetch(req);
}
function notSupportedPreview() {
  return Object.assign(
    new Error("exposePort()/preview URLs are not supported on twd yet: they need public ingress (ADR-029); reach a container port from its object with ctx.container.getTcpPort(port).fetch()"),
    { code: "not_supported" }
  );
}
function err(body, fallback) {
  const code = body?.code ?? "container_error";
  let msg = String(body?.message ?? body?.detail ?? fallback);
  if (code === "not_listening") msg = NOT_LISTENING + " (" + msg + ")";
  else if (code === "container_not_running") msg = NOT_RUNNING;
  const e = new Error(msg);
  e.code = code;
  return e;
}
async function unwrap(p) {
  const r = await p;
  if (!r || !r.ok) throw err(r?.error, "container op failed");
  return r.value;
}
async function errorOf(r) {
  let body = { code: r.headers.get("twd-error") ?? "container_error", message: "HTTP " + r.status };
  try {
    body = await r.json();
  } catch {
  }
  return err(body, "HTTP " + r.status);
}
function toBytes(chunks) {
  let n = 0;
  for (const c of chunks) n += c.byteLength;
  const out = new Uint8Array(n);
  let o = 0;
  for (const c of chunks) {
    out.set(c, o);
    o += c.byteLength;
  }
  return out.buffer;
}
async function readAll(s) {
  if (!s) return new ArrayBuffer(0);
  const chunks = [];
  const rd = s.getReader();
  for (; ; ) {
    const { done, value } = await rd.read();
    if (done) break;
    chunks.push(value);
  }
  return toBytes(chunks);
}
function demux(body, wantOut, wantErr) {
  const ctl = { out: null, err: null };
  const stdout = wantOut ? new ReadableStream({ start: (c) => void (ctl.out = c) }) : null;
  const stderr = wantErr ? new ReadableStream({ start: (c) => void (ctl.err = c) }) : null;
  const outC = () => ctl.out;
  const errC = () => ctl.err;
  const exitCode = (async () => {
    const rd = body.getReader();
    let buf = new Uint8Array(0);
    const fail = (e) => {
      for (const c of [outC(), errC()]) {
        try {
          c?.error(e);
        } catch {
        }
      }
    };
    try {
      for (; ; ) {
        while (buf.byteLength >= 5) {
          const len = new DataView(buf.buffer, buf.byteOffset + 1, 4).getUint32(0);
          if (buf.byteLength < 5 + len) break;
          const kind = buf[0];
          const p = buf.slice(5, 5 + len);
          buf = buf.subarray(5 + len);
          if (kind === 1) outC()?.enqueue(p);
          else if (kind === 2) errC()?.enqueue(p);
          else if (kind === 3) {
            const code = p.byteLength === 4 ? new DataView(p.buffer, p.byteOffset, 4).getInt32(0) : -1;
            for (const c of [outC(), errC()]) {
              try {
                c?.close();
              } catch {
              }
            }
            rd.cancel().catch(() => {
            });
            return code;
          }
        }
        const { done, value } = await rd.read();
        if (done) throw new Error("exec stream ended without an exit status");
        const nb = new Uint8Array(buf.byteLength + value.byteLength);
        nb.set(buf);
        nb.set(value, buf.byteLength);
        buf = nb;
      }
    } catch (e) {
      fail(e);
      throw e;
    }
  })();
  exitCode.catch(() => {
  });
  return { stdout, stderr, exitCode };
}
function createContainer(tw, ctx, cls, spec, self) {
  const as = self ? { as: self } : {};
  const op = (name, args = {}) => unwrap(tw.ctr(name, { ...as, ...args })).catch((e) => {
    if (e?.code === "container_not_running" && !starting) {
      running = false;
      save();
    }
    throw e;
  });
  const kv = ctx?.storage?.kv;
  const load = () => {
    try {
      const v = kv?.get(STATE_KEY);
      if (v && typeof v === "object") return { running: !!v.running, gen: Number(v.gen) || 0 };
    } catch {
    }
    return { running: false, gen: 0 };
  };
  const st = load();
  let running = st.running;
  let gen = st.gen;
  let starting = null;
  let startErr = null;
  const intercepts = /* @__PURE__ */ new Map();
  if (ctx && typeof ctx === "object") handlers.set(ctx, intercepts);
  const save = () => {
    try {
      kv?.put(STATE_KEY, { running, gen });
    } catch {
    }
  };
  const keep = (p) => {
    try {
      ctx.waitUntil?.(p);
    } catch {
    }
  };
  let watching = 0;
  const watch = () => {
    const g = gen;
    if (!running || !g || watching === g) return;
    watching = g;
    keep((async () => {
      try {
        for (; ; ) {
          const v = await op("monitor", { gen: g });
          if (v.done) {
            if (gen === g) {
              running = false;
              save();
            }
            return;
          }
        }
      } catch {
      } finally {
        if (watching === g) watching = 0;
      }
    })());
  };
  const intercept = async (kind, addr, binding) => {
    const b = binding;
    if (!b || typeof b !== "function" && typeof b.fetch !== "function") throw new TypeError("intercept: the handler must be a Fetcher (an object with fetch())");
    await settle();
    const key = kind + " " + addr;
    const v = await op("intercept", { kind, addr });
    intercepts.set(String(v?.key ?? key), binding);
  };
  const settle = async () => {
    if (starting) await starting.catch(() => {
    });
    watch();
  };
  const reconcile = async () => {
    try {
      const v = await op("status");
      running = !!v.running;
      gen = Number(v.gen) || 0;
      save();
      watch();
    } catch {
    }
  };
  try {
    ctx.blockConcurrencyWhile?.(reconcile);
  } catch {
  }
  const c = {
    get running() {
      return running;
    },
    get images() {
      return { ...spec.images ?? {} };
    },
    start(options = {}) {
      if (running) throw new Error("start() cannot be called on a container that is already running.");
      if (options?.containerSnapshot) throw Object.assign(new Error("containerSnapshot is not supported on twd yet (ADR-032 D6: v1.1)"), { code: "not_supported" });
      if (options?.directorySnapshots?.length) throw Object.assign(new Error("directorySnapshots are not supported on twd"), { code: "not_supported" });
      running = true;
      startErr = null;
      save();
      const req = {
        image: options.image,
        entrypoint: options.entrypoint,
        env: options.env,
        enableInternet: !!options.enableInternet,
        instance: options.instance,
        labels: options.labels
      };
      const p = op("start", { start: req }).then(
        (v) => {
          gen = Number(v.gen) || gen + 1;
          save();
          watch();
        },
        (e) => {
          running = false;
          startErr = e;
          save();
          throw e;
        }
      );
      starting = p;
      p.catch(() => {
      }).finally(() => {
        if (starting === p) starting = null;
      });
      keep(p.catch(() => {
      }));
    },
    async monitor() {
      if (starting) await starting;
      else if (!running) {
        if (startErr) throw startErr;
        throw new Error(NOT_RUNNING);
      }
      const g = gen;
      for (; ; ) {
        const v = await op("monitor", { gen: g });
        if (!v.done) continue;
        if (gen === g) {
          running = false;
          save();
        }
        const code = Number(v.exitCode);
        const reason = String(v.reason ?? "");
        if (code === 0 || reason.startsWith("destroy")) return;
        if (reason === "exited") throw Object.assign(new Error(UNEXPECTED_EXIT + code), { exitCode: code });
        throw Object.assign(new Error(RUNTIME_SIGNALLED + (code >= 0 ? code : 137) + " (" + reason + ")"), { exitCode: code });
      }
    },
    async destroy(reason) {
      await settle();
      await op("destroy", { reason: reason === void 0 ? "" : String(reason?.message ?? reason) });
      running = false;
      save();
    },
    signal(signo) {
      if (!Number.isInteger(signo) || signo < 1 || signo > 64) throw new RangeError("signal: bad signal number");
      const p = settle().then(() => op("signal", { signal: signo }));
      p.catch(() => {
      });
      keep(p.catch(() => {
      }));
    },
    async setInactivityTimeout(ms) {
      const n = Number(ms);
      if (!Number.isFinite(n) || n < 0) throw new RangeError("setInactivityTimeout: bad duration");
      await settle();
      if (!running) return;
      await op("setInactivityTimeout", { ms: Math.floor(n) });
    },
    async inspect() {
      await settle();
      const v = await op("inspect");
      return v.info ?? null;
    },
    getTcpPort(port) {
      if (!Number.isInteger(port) || port < 1 || port > 65535) throw new RangeError("getTcpPort: bad port");
      return {
        async fetch(input, init) {
          await settle();
          const src = new Request(input, init);
          const sig = init?.signal ?? (input instanceof Request ? input.signal : void 0);
          if (sig?.aborted) throw sig.reason ?? new Error("aborted");
          const hasBody = src.method !== "GET" && src.method !== "HEAD";
          const req = new Request(src.url, { method: src.method, headers: src.headers, body: hasBody ? src.body : void 0, redirect: "manual" });
          const pending = tw.ctrFetch({ ...as, port }, req);
          const r = sig ? await new Promise((res, rej) => {
            const onAbort = () => rej(sig.reason ?? new Error("aborted"));
            sig.addEventListener("abort", onAbort, { once: true });
            pending.then(
              (v) => {
                sig.removeEventListener("abort", onAbort);
                res(v);
              },
              (e) => {
                sig.removeEventListener("abort", onAbort);
                rej(e);
              }
            );
          }) : await pending;
          const code = r.headers.get("twd-error");
          if (code && (code === "not_listening" || code === "container_not_running" || code.startsWith("container_"))) {
            throw await errorOf(r);
          }
          return r;
        },
        connect() {
          throw Object.assign(new Error("getTcpPort().connect() is not supported on twd yet (ADR-032: deferred)"), { code: "not_supported" });
        }
      };
    },
    async exec(argv, options = {}) {
      if (!Array.isArray(argv) || argv.length === 0 || argv.some((a) => typeof a !== "string")) throw new TypeError("exec: argv must be a non-empty string array");
      await settle();
      if (!running) throw startErr ?? new Error(NOT_RUNNING);
      const sig = options.signal;
      if (sig?.aborted) throw sig.reason ?? new Error("aborted");
      const ptyOpt = options.pty;
      const pty = !!ptyOpt;
      const stdoutMode = options.stdout ?? "pipe";
      const stderrMode = pty ? "ignore" : options.stderr ?? "pipe";
      const stdinOpt = options.stdin;
      const execSpec = {
        argv,
        env: options.env,
        cwd: options.cwd,
        user: options.user,
        stdin: stdinOpt !== void 0 && stdinOpt !== null,
        stdout: stdoutMode,
        stderr: stderrMode,
        pty,
        cols: typeof ptyOpt === "object" ? ptyOpt.cols : void 0,
        rows: typeof ptyOpt === "object" ? ptyOpt.rows : void 0
      };
      const r = await tw.ctrExec({ ...as, spec: execSpec });
      if (!r.ok || !r.body) throw await errorOf(r);
      const id = r.headers.get("x-twd-exec") ?? "";
      const pid = Number(r.headers.get("x-twd-exec-pid") ?? 0);
      const { stdout, stderr, exitCode } = demux(r.body, stdoutMode !== "ignore", stderrMode === "pipe");
      let done = false;
      exitCode.then(
        () => done = true,
        () => done = true
      );
      let stdin = null;
      if (execSpec.stdin) {
        let src;
        if (stdinOpt === "pipe") {
          const ts = new TransformStream();
          stdin = ts.writable;
          src = ts.readable;
        } else {
          src = stdinOpt;
        }
        const p = tw.ctrStdin({ ...as, exec: id }, src).catch(() => {
        });
        keep(p);
      }
      const kill = (signo = 15) => {
        if (done) return;
        const p = op("exec-signal", { exec: id, signal: signo }).catch(() => {
        });
        keep(p);
      };
      if (sig) {
        const onAbort = () => kill(9);
        sig.addEventListener("abort", onAbort, { once: true });
        exitCode.finally(() => sig.removeEventListener("abort", onAbort)).catch(() => {
        });
      }
      return {
        stdin,
        stdout,
        stderr,
        pid,
        isPty: pty,
        exitCode,
        async output() {
          const [o, e, code] = await Promise.all([readAll(stdout), readAll(stderr), exitCode]);
          return { stdout: o, stderr: e, exitCode: code };
        },
        kill,
        resize(cols, rows) {
          if (!pty) throw new Error("resize: not a PTY exec");
          const p = op("exec-resize", { exec: id, cols, rows }).catch(() => {
          });
          keep(p);
        }
      };
    },
    // Egress interception: hostd's sandbox egress proxy delivers the
    // container's matching requests back to this object, which runs them
    // through the handler kept here (dispatchIntercept). HTTPS is
    // terminated with the container's own ephemeral CA (its certificate is
    // at /etc/cloudflare/certs/cloudflare-containers-ca.crt in the
    // container). At most 128 per container.
    async interceptOutboundHttp(addr, binding) {
      await intercept("http", String(addr), binding);
    },
    async interceptOutboundHttps(addr, binding) {
      await intercept("https", String(addr), binding);
    },
    async interceptAllOutboundHttp(binding) {
      await intercept("all", "*", binding);
    },
    async snapshotContainer() {
      throw Object.assign(new Error("snapshotContainer() is not supported on twd yet (ADR-032 D6: v1.1)"), { code: "not_supported" });
    }
  };
  void cls;
  return Object.freeze(c);
}
function defineContainer(ctx, c) {
  try {
    Object.defineProperty(ctx, "container", { value: c, enumerable: false, configurable: true });
  } catch {
  }
}

// js/packages/tailnet/src/trace.ts
import { AsyncLocalStorage } from "node:async_hooks";
var MAX_SPANS = 64;
var TP_RE = /^([0-9a-f]{2})-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})(-.*)?$/;
function parseTraceparent(v) {
  if (typeof v !== "string") return null;
  const m = TP_RE.exec(v.trim());
  if (!m || m[1] === "ff" || m[1] === "00" && m[5]) return null;
  if (/^0+$/.test(m[2]) || /^0+$/.test(m[3])) return null;
  return { traceId: m[2], spanId: m[3], flags: m[4] };
}
function randHex(bytes) {
  const b = new Uint8Array(bytes);
  for (; ; ) {
    crypto.getRandomValues(b);
    if (b.some((x) => x !== 0)) return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
  }
}
function newSpanId() {
  return randHex(8);
}
var als = null;
try {
  als = new AsyncLocalStorage();
} catch {
  als = null;
}
function headerOf(req) {
  try {
    return req?.headers?.get?.("traceparent");
  } catch {
    return null;
  }
}
async function runTraced(req, fn, flush) {
  if (!als) return fn();
  const header = headerOf(req);
  const tp = parseTraceparent(header);
  if (!tp) return als.run(void 0, fn);
  const st = { ...tp, incoming: String(header), spans: [] };
  return als.run(st, async () => {
    try {
      return await fn();
    } finally {
      if (st.spans.length && flush) {
        const spans = st.spans.splice(0);
        try {
          flush(spans);
        } catch {
        }
      }
    }
  });
}
function withTraceparent(input, init, value, incoming) {
  let base = init?.headers;
  if (base === void 0 && typeof Request !== "undefined" && input instanceof Request) base = input.headers;
  let h;
  try {
    h = new Headers(base);
  } catch {
    return null;
  }
  const cur = h.get("traceparent");
  if (cur && cur !== incoming) return null;
  h.set("traceparent", value);
  return [input, { ...init ?? {}, headers: h }];
}
var installed = false;
function installFetchPropagation() {
  if (installed || !als || typeof globalThis.fetch !== "function") return;
  installed = true;
  const orig = globalThis.fetch;
  const traced = function fetch2(input, init) {
    const st = als?.getStore();
    if (st) {
      const a = withTraceparent(input, init, `00-${st.traceId}-${st.spanId}-${st.flags}`, st.incoming);
      if (a) return orig.call(globalThis, a[0], a[1]);
    }
    return orig.call(globalThis, input, init);
  };
  try {
    Object.defineProperty(globalThis, "fetch", { value: traced, writable: true, configurable: true });
  } catch {
  }
}
async function tracedStubFetch(cls, name, args, call) {
  const st = als?.getStore();
  if (!st) return call(...args);
  const spanId = newSpanId();
  const a = withTraceparent(args[0], args[1], `00-${st.traceId}-${spanId}-${st.flags}`, st.incoming);
  if (!a) return call(...args);
  let method = "GET";
  try {
    method = String(a[1]?.method ?? args[0]?.method ?? "GET").toUpperCase();
  } catch {
  }
  const rec = {
    traceId: st.traceId,
    spanId,
    parentSpanId: st.spanId,
    name: `${cls} fetch`,
    start: Date.now(),
    end: 0,
    error: false,
    attrs: { "twd.do.class": cls, "twd.do.name": name, "http.request.method": method }
  };
  const done = () => {
    rec.end = Date.now();
    if (st.spans.length < MAX_SPANS) st.spans.push(rec);
  };
  try {
    const r = await call(...a, ...args.slice(2));
    rec.attrs["http.response.status_code"] = r?.status ?? 0;
    rec.error = (r?.status ?? 0) >= 500;
    done();
    return r;
  } catch (e) {
    rec.error = true;
    done();
    throw e;
  }
}

// js/packages/tailnet/src/runtime.ts
installFetchPropagation();
function spanFlusher(tw, ctx) {
  return (spans) => {
    if (!tw || typeof tw.call !== "function") return;
    const p = Promise.resolve().then(() => tw.call("spans", { spans })).catch(() => {
    });
    try {
      ctx?.waitUntil?.(p);
    } catch {
    }
  };
}
var twOf = /* @__PURE__ */ new WeakMap();
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
    const err2 = await fromResponse(r);
    if (err2) throw err2;
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
function wrapFacetClass(C, name, bindings, opts = {}) {
  if (typeof C !== "function") throw new Error(`twd: durable_objects class ${name} is not exported by the main module`);
  const W = {
    [name]: class extends C {
      constructor(ctx, env) {
        const tw = env?.__TWD;
        const e = guardEnv(env, bindings, "tailnet", admitter(tw, {}));
        define(ctx, createTailnetContext(tw, e, bindings));
        installAlarmShim(ctx, tw);
        if (tw && opts.container) defineContainer(ctx, createContainer(tw, ctx, name, opts.container));
        super(ctx, e);
        twOf.set(this, tw);
      }
      // Called by the Host DO when the delegated alarm fires.
      async __twd_alarm(info) {
        const self = this;
        if (typeof self.alarm === "function") return self.alarm(info);
      }
      // Called by the Host DO with a request the container sent to an
      // intercepted destination (hostd's sandbox egress proxy).
      async __twd_intercept(key, req) {
        return dispatchIntercept(this.ctx, key, req);
      }
    }
  }[name];
  const ff = C.prototype?.fetch;
  if (typeof ff === "function") {
    W.prototype.fetch = function(req) {
      return runTraced(req, () => ff.call(this, req), spanFlusher(twOf.get(this), this.ctx));
    };
  }
  if (opts.container) noPreviewURLs(W);
  return W;
}
var PREVIEW_HEADER = "x-sandbox-preview-proxy";
function noPreviewURLs(W) {
  const proto = Object.getPrototypeOf(W.prototype);
  if (typeof proto?.exposePort === "function") {
    W.prototype.exposePort = async function() {
      throw notSupportedPreview();
    };
  }
  const f = W.prototype.fetch;
  if (typeof f === "function") {
    W.prototype.fetch = async function(req) {
      if (req?.headers?.get?.(PREVIEW_HEADER)) {
        const e = notSupportedPreview();
        return new Response(JSON.stringify({ code: "not_supported", message: e.message }), {
          status: 501,
          headers: { "content-type": "application/json", "twd-error": "not_supported" }
        });
      }
      return f.call(this, req);
    };
  }
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
        if (tw && oname && opts.container) defineContainer(ctx, createContainer(tw, ctx, name, opts.container, { class: name, name: oname }));
        super(ctx, e);
        twOf.set(this, tw);
        if (tw && oname && opts.node) {
          const p = tw.call("ensure", { as: { class: name, name: oname } }).then((r) => {
            if (r && !r.ok) {
              const err2 = TailnetError.from(r.error);
              if (err2.code === "create_not_admitted") this.__twd_denied = err2;
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
      async __twd_intercept(key, req) {
        const self = this;
        const k = self.ctx.storage.kv.get("__twd_killed");
        if (k) return killedResponse(k);
        return dispatchIntercept(self.ctx, key, req);
      }
    }
  }[name];
  if (typeof proto.fetch === "function") {
    W.prototype.fetch = async function(req) {
      const k = this.ctx.storage.kv.get("__twd_killed");
      if (k) return killedResponse(k);
      if (this.__twd_ensure) await this.__twd_ensure;
      if (this.__twd_denied) return this.__twd_denied.toResponse();
      return runTraced(req, () => proto.fetch.call(this, req), spanFlusher(twOf.get(this), this.ctx));
    };
  }
  if (opts.container) noPreviewURLs(W);
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
          if (p === "fetch") return tracedStubFetch(cls, name, args, (...a) => target.fetch(...a));
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
          return await runTraced(req, () => f.call(this, req), spanFlusher(tw, this.ctx));
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
        r = k === "fetch" ? runTraced(a, () => H[k].call(H, a, env, ctx), spanFlusher(tw, ctx)) : H[k].call(H, a, env, ctx);
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
