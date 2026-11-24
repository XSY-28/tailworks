# Python Workers

tailworks runs Cloudflare Python Workers: a wrangler project whose `main` is a
`.py` module, with `"python_workers"` in `compatibility_flags`, deploys with
`tw deploy` and runs under `tw dev` as on Cloudflare. The code runs in Pyodide
inside workerd, with the same `workers` SDK (`WorkerEntrypoint`,
`DurableObject`, `Response`, `fetch`) and the same `js` interop. Design:
[ADR-055](design/adr/055-python-workers.md). What matches Cloudflare
and what doesn't: [COMPAT.md](COMPAT.md#python).

```sh
tw init my-py --template python
tw dev my-py                     # http://127.0.0.1:8787
tw deploy my-py
```

## A Worker

```jsonc
// wrangler.jsonc
{
  "name": "hello-py",
  "main": "src/entry.py",
  "compatibility_date": "2026-04-01",
  "compatibility_flags": ["python_workers"]
}
```

```python
# src/entry.py
from workers import Response, WorkerEntrypoint


class Default(WorkerEntrypoint):
    async def fetch(self, request):
        return Response("Hello from Python")
```

Bindings work as in a JavaScript Worker, through `self.env`: `vars`,
[secrets](secrets.md), [KV](kv.md), [R2](r2.md), [D1](d1.md), Durable
Object namespaces and [service bindings](wrangler.md#service-bindings-services).
Outbound `fetch()` leaves from the workload's node, so tailnet policy decides
what it can reach, as for any Worker ([COMPAT.md](COMPAT.md#fetch)).

## Who is calling

The caller's tailnet identity is in `request.cf`, as for a JavaScript Worker.
tailworks checks it before your code runs: `tailscale.caller` is present only
when hostd's seal over it verified, so it can't be forged by a client. The
`python` template ships a small `tailworks.py` with the helper:

```python
from tailworks import get_caller


class Default(WorkerEntrypoint):
    async def fetch(self, request):
        who = get_caller(request)  # None for a public request
        login = who["user"]["login"] if who and who["user"] else "anonymous"
        return Response(f"hello {login}")
```

`get_caller` returns a dict with `getCaller`'s fields from
`@tailworks/tailnet`: `nodeId`, `tags`, `hostname`, `node`, `addrs`, `user`
(`{"login": …}` for a person, `None` for a tagged node), `capMap`,
`objectId` and `traceId`. It reads `request.js_object.cf.tailscale.caller`;
copy the file into your own project to use it there.

## Durable Objects

A Python `DurableObject` class works with its storage (`self.ctx.storage`,
KV and SQL), RPC methods, `fetch` and the WebSocket hibernation API.

```python
from workers import DurableObject, Response, WorkerEntrypoint


class Counter(DurableObject):
    async def increment(self):
        n = (await self.ctx.storage.get("n") or 0) + 1
        await self.ctx.storage.put("n", n)
        return n


class Default(WorkerEntrypoint):
    async def fetch(self, request):
        n = await self.env.COUNTER.getByName("global").increment()
        return Response(str(n))
```

Python Durable Objects are **internal objects**: they run in plain-DO mode
and have no tailnet node of their own, like a class with `"node": false` in
[tailworks.jsonc](tailworks-jsonc.md). They are reached through the Worker's
`env`, not over the tailnet, and their egress leaves as the workload. So
`tailnet.objects.<Class>` can't give a Python class tags, `agent`, `ttl` or
`public`, and `ctx.tailnet` (spawn, kill) isn't available to Python code.
[Egress modes](egress-modes.md) explains the difference.

## Modules and files

There is no bundler. `tw deploy` ships, as pywrangler does:

- every file under the directory of `main` (`src/` for `src/entry.py`):
  `.py` files as Python modules named relative to it (`import helper` for
  `src/helper.py`), and the rest as files your code can read
  (`Path(__file__).parent / "page.html"`). `__pycache__`, `.pyc` files,
  dot-directories, `node_modules` and `venv` are left out.
- the vendored packages in `python_modules/` (next to `main`, else at the
  project root), under `python_modules/`.

## Packages

Packages are vendored at build time and shipped with the Worker, never
installed on a host. Vendor them with Cloudflare's tooling, then deploy:

```sh
uv run pywrangler sync     # writes python_modules/ from pyproject.toml
tw deploy
```

Only pure-Python packages work. `tw deploy` warns when `pyproject.toml` or
`requirements.txt` declares packages and there is no `python_modules/`.

From `compatibility_date` 2026-04-21 the `workers` SDK itself is a package
(`workers-runtime-sdk`, which `pywrangler sync` vendors). Without it,
`tw deploy` refuses a Worker that imports `workers` and says how to fix it:
vendor it, add `"disable_python_external_sdk"` to `compatibility_flags`, or
use an earlier date. The template uses 2026-04-01, which has the SDK built in.

## Pyodide on the host

workerd needs a Pyodide bundle to start a Python Worker. tailworks pins the
bundles the pinned workerd uses, by SHA-256 (`internal/pyodide`). The first
Python deployment on a host fetches them once into the host's bundle
directory (`tw-hostd -pyodide-dir`, default `<data-dir>/pyodide`; `tw dev`
uses the per-user cache, `~/Library/Caches/tailworks/pyodide` on macOS and
`~/.cache/tailworks/pyodide` on Linux) and checks each digest. workerd then
reads them read-only: its sandbox gains read access to that one directory and
nothing else, and it never downloads anything itself.

### Offline hosts

On a host with no internet access, copy the bundles into the directory before
the first Python deployment. The file names and digests are in
[`internal/pyodide/pyodide.go`](../internal/pyodide/pyodide.go); each file is
at `https://pyodide-capnp-bin.edgeworker.net/<file name>`. A missing or
corrupt bundle fails the deployment with `python_unavailable`
([errors.md](errors.md#python_unavailable)).

### Updating the pins

The bundles follow the workerd pin. When a workerd bump changes them,
`internal/pyodide:TestBundlesMatchWorkerd` fails; add the new bundle's name,
size and SHA-256 to `Bundles`.

## Limits

Not supported for Python Workers yet; `tw deploy` and `tw dev` refuse each
one with a message naming it:

- cron triggers (`triggers.crons`), Queues, Workflows, static assets and
  containers;
- the microVM tier (`"tier": "microvm"`, or a tenant whose default is
  microVM): the guest image has no Pyodide bundles yet. The deployment is
  refused (`tier_unavailable`), not run in the process tier instead;
- `--wrangler` and `--from-wrangler-out`: they bundle JavaScript.

Also different from Cloudflare: no memory snapshots, so every cold start
loads Pyodide (about a second on a laptop); Pyodide's own non-pure-Python
packages (`numpy` and the like) aren't available, because nothing is
downloaded at run time.
