# Debugging in tw dev

`tw dev` runs your app on the same tw-hostd, netd and workerd as a deployed
host. This page covers the debugging tools it has in common with
`wrangler dev`: source-mapped exception stacks, `req.url`, the V8 inspector
(Chrome DevTools, VS Code) and the terminal hotkeys.

## Exception stacks point at your sources

An uncaught exception shows up under its request line, with the stack mapped
back to your TypeScript:

```text
2026-11-15T21:12:35.176Z GET /boom 500 1.9ms exception  caller=you@example.com  trace=c4e7…
    error Error: boom at /boom
          at explode (src/boom.ts:3:9)
          at Object.fetch (src/index.ts:8:7)
          … 4 tw runtime frames (__tw_rt.js)
```

- The built-in bundler (esbuild) writes `dist/index.js.map` next to the
  bundle. The map holds positions and names but no source text. tw-hostd
  rewrites every exception stack with it before it writes the request
  record, so `tw dev`, `tw logs`, `tw logs --json` and the OTLP export all
  show `src/…` positions.
- The text output folds frames from tailworks' own modules (`__tw_rt.js`, the
  generated `__tw_main.js` and `__tw_facet.js`) into one `… N tw runtime
  frames` line. The JSON record (`tw dev --json`, `tw logs --json`) keeps
  the whole stack.
- `tw deploy` ships the map too, unless the wrangler config sets
  `"upload_source_maps": false`. Then deployed stacks show `dist/index.js`
  positions. `tw dev` always maps.
- Bundles built by wrangler's bundler (`--wrangler`, `--from-wrangler-out`)
  ship no map yet, so their stacks show bundle positions.

## `req.url` is the URL you opened

The dev proxy on `--port` forwards each request to the workload's tailnet
node and leaves the `Host` header alone. So `req.url` is
`http://127.0.0.1:8787/…` (or `http://localhost:8787/…`, whichever you typed),
as in `wrangler dev`. It is not the dev node's `100.64.x.y` address. Code
that builds absolute URLs (OAuth redirect URIs, links,
`new URL(path, req.url)`) gets the address the browser can reach.

`getCaller(req)` is unaffected. The caller is always the dev client node's
identity (WhoIs of the tailnet connection), never anything in the request's
headers. On a deployed host, `Host` is whatever the tailnet client sent (the
MagicDNS name, the 100.x address or a Funnel hostname), and tailworks never uses it
for identity or routing either.

## Attach a debugger: `--inspector-port`

```sh
tw dev --inspector-port 9229
```

This starts workerd with its V8 inspector and serves it on
`127.0.0.1:9229`, loopback only. On every reload, tw dev forwards the fixed
port to the new workerd, so you don't need to reconfigure the debugger. With
the flag on, the bundle also carries its source map inline, with the
sources, so the debugger shows `src/…` files.

There are a few targets. Pick **`workerd: worker tenant`**: that is your
app. `platform` and `deny` are tailworks' own isolates. Breakpoints and `debugger;`
statements stop in a request handler, so make a request after you attach.

### Chrome DevTools

1. Open `chrome://inspect` and select **Configure…**.
2. Add `127.0.0.1:9229`.
3. Under **Remote Target**, select **inspect** next to `workerd: worker tenant`.

The **Sources** panel lists `src/…` from the inline source map.

### VS Code

Add this to `.vscode/launch.json`, run `tw dev --inspector-port 9229`, then
start **Attach to tw dev**:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Attach to tw dev",
      "type": "node",
      "request": "attach",
      "address": "127.0.0.1",
      "port": 9229,
      "cwd": "${workspaceFolder}",
      "resolveSourceMapLocations": null,
      "attachExistingChildren": false,
      "autoAttachChildProcesses": false,
      "sourceMaps": true
    }
  ]
}
```

This is the configuration Cloudflare documents for `wrangler dev`. When VS
Code offers several targets, choose `workerd: worker tenant`.

With `--json`, the `inspector` event gives the address
(`{"event":"inspector","addr":"127.0.0.1:9229","ws":"ws://127.0.0.1:9229"}`).

The inspector gives full control of the isolate, so it listens on loopback
only. tw-hostd's `WorkerdInspector` option takes effect only in tw dev's
process-tier mode, never on a host that serves tenants.

## Look at the data: the dev console

`tw dev` serves a read-only data console on `127.0.0.1`, as `wrangler
dev`'s `e` hotkey opens a local explorer. The ready output prints its
address (`console on http://127.0.0.1:<port>/`); press `e` to open it.

- **Workers KV**: each namespace that holds data, its keys (sorted, with a
  prefix filter, 100 per page), each value's size, expiration and
  metadata, and the value itself (JSON indented, binary as hex).
- **D1**: each database the app binds, its tables with their row counts,
  and the rows of a table, 50 per page.
- **Other stores** (R2 buckets, queues): listed with their journal size,
  not browsable.

It reads the run's own stores, so it shows what the app wrote a moment
ago. It never writes: change data from the app (for a deployed app, `tw
kv` and `tw d1 execute` also write). Durable Object storage isn't shown
yet.

`--console-port <port>` picks the port (default `0`, a free one);
`--console-port -1` turns the console off. It listens on loopback only and
answers only requests whose `Host` is that loopback address, so a web page
can't read it through DNS rebinding. With `--json`, the `console` event
gives its URL.

## Hotkeys

When stdin is a terminal and `--json` is off, `tw dev` reads single keys:

| Key | Action |
|---|---|
| `b` | open the app's URL (with the MCP route, if the app has one) in the default browser |
| `e` | open the data console (below) in the default browser |
| `x` or `q` | stop, like Ctrl-C |

While tw dev runs, the terminal doesn't echo keys. It goes back to normal
when tw dev exits. Ctrl-C works as usual. With stdin redirected, under
`--json`, or as a background job, tw dev doesn't read hotkeys.
