// The static assets router Worker (wrangler "assets", ADR-static-assets).
// Generated per deployment by hostd/gen/assets.go: one workerd service,
// "assets", with the deployment's files on a read-only disk service
// (env.FILES) and a manifest of their ETags. It follows Cloudflare's asset
// worker: html_handling, not_found_handling, ETag / If-None-Match, and
// run_worker_first.
//
// Two ways in:
//   - the router (ctx.props.router, from the tenant's __tw_main.js) for a
//     request to the workload: it answers with the file, a redirect, or a
//     miss (MISS header) that sends the request on to the app's Worker;
//   - env.<binding>.fetch(req) from the app: a file, with
//     not_found_handling for a miss.
import MANIFEST from "tw-assets-manifest.json";
import CFG from "tw-assets-config.json";

const MISS = "x-tw-asset-miss";

const TYPES = {
  avif: "image/avif", bmp: "image/bmp", css: "text/css; charset=utf-8", csv: "text/csv; charset=utf-8",
  gif: "image/gif", gz: "application/gzip", htm: "text/html; charset=utf-8", html: "text/html; charset=utf-8",
  ico: "image/x-icon", jpeg: "image/jpeg", jpg: "image/jpeg", js: "text/javascript; charset=utf-8",
  json: "application/json; charset=utf-8", map: "application/json; charset=utf-8", md: "text/markdown; charset=utf-8",
  mjs: "text/javascript; charset=utf-8", mp3: "audio/mpeg", mp4: "video/mp4", oga: "audio/ogg", ogg: "audio/ogg",
  otf: "font/otf", pdf: "application/pdf", png: "image/png", svg: "image/svg+xml", tar: "application/x-tar",
  ttf: "font/ttf", txt: "text/plain; charset=utf-8", wasm: "application/wasm", wav: "audio/wav",
  webm: "video/webm", webmanifest: "application/manifest+json", webp: "image/webp", woff: "font/woff",
  woff2: "font/woff2", xml: "application/xml; charset=utf-8", zip: "application/zip",
};

function contentType(p) {
  const base = p.slice(p.lastIndexOf("/") + 1);
  const i = base.lastIndexOf(".");
  return (i > 0 && TYPES[base.slice(i + 1).toLowerCase()]) || "application/octet-stream";
}

const has = (p) => Object.prototype.hasOwnProperty.call(MANIFEST, p);

// run_worker_first patterns: "*" matches any run of characters (slashes
// too); a "!" pattern excludes what it matches.
function globRE(g) {
  return new RegExp("^" + g.split("*").map((s) => s.replace(/[.+?^${}()|[\]\\]/g, "\\$&")).join(".*") + "$");
}
const WF = Array.isArray(CFG.runWorkerFirst) ? CFG.runWorkerFirst : [];
const WF_IN = WF.filter((p) => !p.startsWith("!")).map(globRE);
const WF_OUT = WF.filter((p) => p.startsWith("!")).map((p) => globRE(p.slice(1)));

function workerFirst(pathname) {
  if (CFG.runWorkerFirst === true) return true;
  return WF_IN.some((r) => r.test(pathname)) && !WF_OUT.some((r) => r.test(pathname));
}

// resolve maps a decoded request path to {serve: file} or {redirect: path}
// per html_handling, or null when nothing matches.
function resolve(p, mode = CFG.htmlHandling) {
  const slash = p.endsWith("/");
  const stem = slash ? p.slice(0, -1) : p; // "/a/" -> "/a", "/" -> ""
  switch (mode) {
    case "none":
      return has(p) ? { serve: p } : null;
    case "force-trailing-slash":
      if (slash) {
        if (has(p + "index.html")) return { serve: p + "index.html" };
        if (stem && has(stem + ".html")) return { serve: stem + ".html" };
        return null;
      }
      if (p.endsWith("/index.html") && has(p)) return { redirect: p.slice(0, -"index.html".length) };
      if (p.endsWith(".html") && has(p)) return { redirect: p.slice(0, -".html".length) + "/" };
      if (has(p)) return { serve: p };
      if (has(p + ".html") || has(p + "/index.html")) return { redirect: p + "/" };
      return null;
    case "drop-trailing-slash":
      if (p === "/") return has("/index.html") ? { serve: "/index.html" } : null;
      if (slash) return has(stem + ".html") || has(stem + "/index.html") || has(stem) ? { redirect: stem } : null;
      if (p.endsWith("/index.html") && has(p)) return { redirect: p.slice(0, -"/index.html".length) || "/" };
      if (p.endsWith(".html") && has(p)) return { redirect: p.slice(0, -".html".length) };
      if (has(p)) return { serve: p };
      if (has(p + ".html")) return { serve: p + ".html" };
      if (has(p + "/index.html")) return { serve: p + "/index.html" };
      return null;
    default: // auto-trailing-slash
      if (slash) {
        if (has(p + "index.html")) return { serve: p + "index.html" };
        if (stem && has(stem + ".html")) return { redirect: stem };
        return null;
      }
      if (p.endsWith("/index.html") && has(p)) return { redirect: p.slice(0, -"index.html".length) };
      if (p.endsWith(".html") && has(p)) {
        const bare = p.slice(0, -".html".length);
        // /a.html -> /a, unless /a/index.html would answer /a/ (then keep it).
        return has(bare + "/index.html") ? { serve: p } : { redirect: bare };
      }
      if (has(p)) return { serve: p };
      if (has(p + ".html")) return { serve: p + ".html" };
      if (has(p + "/index.html")) return { redirect: p + "/" };
      return null;
  }
}

// notFound finds the not_found_handling file for p: index.html (SPA) or
// the nearest 404.html walking up from p's directory (404-page).
function notFound(p) {
  if (CFG.notFound === "single-page-application") return has("/index.html") ? { file: "/index.html", status: 200 } : null;
  if (CFG.notFound === "404-page") {
    let d = p.slice(0, p.lastIndexOf("/") + 1);
    for (;;) {
      if (has(d + "404.html")) return { file: d + "404.html", status: 404 };
      if (d === "/") return null;
      d = d.slice(0, d.slice(0, -1).lastIndexOf("/") + 1);
    }
  }
  return null;
}

function etagMatches(header, etag) {
  if (!header) return false;
  if (header.trim() === "*") return true;
  return header.split(",").some((t) => t.trim().replace(/^W\//, "") === etag);
}

async function serveFile(req, env, file, status) {
  const etag = '"' + MANIFEST[file] + '"';
  const h = new Headers({ "content-type": contentType(file), etag, "cache-control": "public, max-age=0, must-revalidate" });
  if (status === 200 && etagMatches(req.headers.get("if-none-match"), etag)) return new Response(null, { status: 304, headers: h });
  const r = await env.FILES.fetch("http://assets" + file.split("/").map(encodeURIComponent).join("/"));
  if (!r.ok) return new Response("asset unavailable", { status: 500 });
  if (r.headers.get("content-length")) h.set("content-length", r.headers.get("content-length"));
  return new Response(req.method === "HEAD" ? null : r.body, { status, headers: h });
}

function redirect(url, to) {
  return new Response(null, { status: 307, headers: { location: to + url.search } });
}

const miss = () => new Response(null, { status: 404, headers: { [MISS]: "1" } });

export default {
  async fetch(req, env, ctx) {
    const router = !!ctx?.props?.router;
    const url = new URL(req.url);
    if (req.method !== "GET" && req.method !== "HEAD") {
      return router ? miss() : new Response(null, { status: 405, headers: { allow: "GET, HEAD" } });
    }
    if (router && workerFirst(url.pathname)) return miss();
    let p;
    try {
      p = decodeURIComponent(url.pathname);
    } catch {
      return router ? miss() : new Response(null, { status: 404 });
    }
    const r = resolve(p);
    if (r?.serve) return serveFile(req, env, r.serve, 200);
    if (r?.redirect) return redirect(url, r.redirect);
    // A miss. With a Worker, the router hands it to the Worker, except a
    // browser navigation in single-page-application mode, which gets
    // index.html (Cloudflare's assets_navigation_prefers_asset_serving).
    if (router && !(CFG.notFound === "single-page-application" && req.headers.get("sec-fetch-mode") === "navigate")) return miss();
    const nf = notFound(p);
    if (nf) return serveFile(req, env, nf.file, nf.status);
    return router ? miss() : new Response(null, { status: 404 });
  },
};
