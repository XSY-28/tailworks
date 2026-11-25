# Workers KV

tailworks runs Cloudflare's Workers KV binding. Declare namespaces in
`wrangler.jsonc` as you would for Cloudflare, and use `env.<BINDING>` with
the same API: `get` (with `text`, `json`, `arrayBuffer` and `stream`),
`getWithMetadata`, bulk `get`, `put` with `expiration`, `expirationTtl` and
`metadata`, `delete`, and `list` with `prefix`, `limit` and `cursor`.
Libraries that need KV, like `@cloudflare/workers-oauth-provider` and its
`OAUTH_KV`, run unmodified. The design is in
[ADR-037](design/adr/037-workers-kv.md).

```jsonc
// wrangler.jsonc
{
  "kv_namespaces": [
    { "binding": "OAUTH_KV", "id": "oauth" },
    { "binding": "CACHE" } // no id: the namespace is named "CACHE"
  ]
}
```

## Namespaces

A namespace belongs to the **workload** (and its replicas). Its name is the
entry's `id`, or the binding name when there is no `id`. Two bindings with
the same `id` in one app share a namespace. Another workload that uses the
same `id` gets its own namespace: workloads don't share namespaces in v1.
To share data, call the owning workload over the tailnet.

A namespace's first write creates it, or you can create it first:

```sh
tw kv namespace create oauth          # for the app in the current directory
tw kv namespace list
tw kv namespace delete oauth          # deletes the data, irrecoverably
```

## Keys

```sh
tw kv key put client:1 '{"name":"app"}' --binding OAUTH_KV --metadata '{"kind":"client"}'
tw kv key put grant:1 --path grant.json --namespace oauth --ttl 3600
printf %s "$TOKEN" | tw kv key put token:1 --binding OAUTH_KV
tw kv key get client:1 --binding OAUTH_KV        # the raw value on stdout
tw kv key list --binding OAUTH_KV --prefix grant:
tw kv key delete token:1 --binding OAUTH_KV
```

`--binding` is looked up in the app's `wrangler.jsonc`; `--namespace`
names the namespace directly; `--workload` picks another workload than the
app in the current directory. Every command takes `--json`. The same
operations are on mgmt's REST API (`GET …/stores/kv/{name}/keys`,
`GET|PUT|DELETE …/stores/kv/{name}/value?key=`; see
[mgmt-api.md](contracts/mgmt-api.md)). Only admins can use them. Each
write is in the audit trail (`tw audit --all`) with the namespace and who
made it, but not the key.

## Where the data is, and what that means

Each namespace is served by a platform-owned SQLite Durable Object on the
workload's host, so reads are local. Every write is first appended to a
journal in tw-mgmt, sealed with the namespace's own key. Only the host
holding the workload's lease can append. So:

- **Durable.** An acknowledged write survives a hostd restart, a lost disk,
  and a move to another host (`tw deploy --replace`, a forced takeover
  after a host failure). A new host rebuilds the namespace from the
  journal.
- **Read-your-writes.** A workload always reads its own writes. A write by
  another writer (another replica, `tw kv key put`) shows up on the next
  read that wants fresher data than the last sync: within 60 s by default,
  or the read's `cacheTtl`. Cloudflare KV is eventually consistent, with
  about 60 s between locations.
- **Writes need mgmt.** If mgmt is unreachable, `put` and `delete` fail
  (`KV PUT failed: 503 …`). Reads serve the local copy: a read that wants
  fresher data waits at most 2 s for mgmt, and later ones don't wait until
  mgmt is back. A host that has never synced the namespace fails reads.
  If the workload has a Durable Object class (not `"fence": "never"`), the
  host stops the whole workload after 30 s without mgmt, reads included.
  Use a Durable Object for state that is written often.
- **Expiry.** An expired key disappears from `get` and `list` at once. The
  namespace's alarm then deletes it from the journal too.

## Limits

| | tailworks | Cloudflare |
|---|---|---|
| Key | 1 to 512 bytes, not `.` / `..` | same |
| Value | 2 MiB − 4 KiB | 25 MiB |
| Metadata | 1024 bytes of JSON | same |
| `expirationTtl` / `expiration` | at least 60 s ahead | same |
| `list` page | up to 1000 keys, UTF-8 order, `cursor` | same |
| Bulk `get` | 1 to 100 keys, `text` or `json` | same |
| Namespace | 1 GiB of live data | 1 GB on the Free plan |

## Local development

`tw dev` gives the app its namespaces, in the dev process. They are saved
to `<app>/.tailworks/dev-stores.json` when `tw dev` stops and loaded when it
starts, so local data lasts between runs (as `wrangler dev`'s local state
does). The file is readable by its owner only and is not encrypted. The
`.tailworks` directory git-ignores itself. Delete the file to start empty.
