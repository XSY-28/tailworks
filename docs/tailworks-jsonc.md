# tailworks.jsonc reference

A tailworks app has a `tailworks.jsonc` next to its wrangler config (`wrangler.jsonc`,
`wrangler.json` or `wrangler.toml`; [wrangler.md](wrangler.md)). The split:

- **The wrangler config** is the Worker itself, as on Cloudflare: `main`,
  `compatibility_date`, `durable_objects` bindings, `migrations`, `vars`,
  `triggers.crons`, `queues` ([queues.md](queues.md)), `containers`,
  `d1_databases` ([d1.md](d1.md)), and `env.<name>` environments
  (`tw deploy --env`).
- **`tailworks.jsonc`** is what tailworks adds: the tailnet identity of the workload and
  of each Durable Object class, the isolation tier, placement, and public
  routes.

`tailworks.jsonc` is JSONC: comments and trailing commas are fine.

A project with only a wrangler config deploys too. Then tailworks uses defaults:
the workload node is tagged `tag:tw-wl-<workload>`, and every Durable
Object class is internal (`"node": false`). `tw deploy` says when it does
this ([wrangler.md](wrangler.md#without-a-twjsonc)).

## Validation and editor completion

`tw dev` and `tw deploy` check `tailworks.jsonc` against
[`schema/tailworks.schema.json`](../schema/tailworks.schema.json) before doing anything
else. An unknown key or a bad value stops the command:

```
error [bad_request]: tailworks.jsonc has 1 problem
  tailnet.objcts: unknown key (did you mean "objects"?)
```

This matters most for `tailnet.objects`. If that key is misspelt, every
class deploys as an ordinary Durable Object with no node of its own, and
nothing else would tell you.

For completion and inline errors in your editor, point `$schema` at the
schema. `tw init` writes this line for you:

```jsonc
{
  "$schema": "https://raw.githubusercontent.com/rosskukulinski/tailworks/master/schema/tailworks.schema.json",
  "name": "support",
  ...
}
```

## Top-level keys

| key | type | default | what it does |
|---|---|---|---|
| `$schema` | string | | the schema, for your editor; tailworks ignores it |
| `name` | string | wrangler `name`, then the directory name | the workload name, a DNS label. `tw deploy --name` overrides it |
| `runtime` | `"workerd"` \| `"node"` | `"workerd"` | `"node"` runs a Node.js app in the Node process tier ([node-tier.md](node-tier.md)); then there is no `wrangler.jsonc` |
| `main` | string | | `runtime: "node"` only: the entry file, e.g. `"src/server.ts"` |
| `node` | object | | `runtime: "node"` only: `env` (string map), `listen`, `health`, `args` ([node-tier.md](node-tier.md)) |
| `tier` | `"process"` \| `"microvm"` | the tenant's tier | `"microvm"` runs the workload in a Firecracker VM, on a Linux host with KVM ([INSTALL.md](INSTALL.md#linux-the-microvm-tier-firecracker-kvm)) |
| `replicas` | integer ≥ 1 | 1 | run a stateless workload on this many hosts ([services.md](services.md)) |
| `placement` | object | | which host runs the workload; [below](#placement) |
| `public` | object | | paths reachable from the public internet over Funnel; [below](#public) |
| `containers` | array or object | | container classes ([containers.md](containers.md)); `tw deploy` also writes image pins here |
| `tailnet` | object | | the workload's tailnet identity; [below](#tailnet) |

## `tailnet`

| key | type | what it does |
|---|---|---|
| `tags` | `["tag:…"]` | **Required.** Tags of the workload's own node. Your policy must let `tag:tw` own them; `tw policy` (and `tw deploy`'s preflight) prints the snippet if it doesn't, and `tw policy --apply` writes it ([below](#tags-and-tw-init)) |
| `connects` | `["tag:…" or name]` | what the workload calls. `tw deploy` preflights these grants, and smart placement measures paths to them |
| `service` | `true` or `"svc:<name>"` | a Tailscale Service: a stable name and VIP in front of the workload ([services.md](services.md)) |
| `egress.internet` | `true`, `false` or `[host, "*.suffix", IP, CIDR]` | internet egress for Worker code and containers; [below](#tailnetegressinternet) |
| `spawn.maxLive` | integer | the most live sub-agents (`ctx.tailnet.spawn`) |
| `spawn.maxDepth` | integer | the deepest spawn chain |
| `objects` | map of class → [object](#tailnetobjectsclass) | the Durable Object classes that get a tailnet node per instance |

### `tailnet.egress.internet`

Whether outbound `fetch()` from the workload's Worker, Durable Objects and
Node app may reach the public internet, and which hosts a container
started with `enableInternet: true` may reach
([containers.md](containers.md)). Tailnet egress is not affected.

| value | Workers and Node apps | containers (`enableInternet: true`) |
|---|---|---|
| absent | any public host | none |
| `true` | any public host | any public host |
| `false` or `[]` | none | none |
| `["api.example.com", "*.github.com", "203.0.113.0/24"]` | only these | only these |

```jsonc
{ "tailnet": { "tags": ["tag:tw-wl-app"], "egress": { "internet": false } } }
```

- Internet traffic leaves from the **host's own network**, not from a
  tailnet node, so the other side sees the host's address (each host has
  its own) and no tailnet identity. For a fixed source address, use a
  gateway on your tailnet ([recipes.md](recipes.md#call-an-internet-api)).
- Loopback, private, link-local (cloud metadata), CGNAT and tailnet
  ranges and the host's own addresses are always refused
  (`forbidden_destination`), even when listed, and even through a DNS name
  that resolves to one. Names under `.internal`, `.local`, `.localhost`,
  `.lan` and other special-use suffixes are never looked up.
- A destination the setting refuses gets `internet_egress_unavailable`.
  The host's operator can also turn internet egress off for every
  workload on a host (`tw up --worker-internet=off`, or
  `tw-hostd -worker-internet=off`; `tw status` and `tw doctor` show it).
- A list entry is an exact host name, `*.suffix` (any name under it),
  `*` (any name), an IP or a CIDR. An IP literal in a URL matches only IP
  and CIDR entries.
- A change takes effect when the deployment that makes it becomes active.
- `true` and `false` need hosts that offer the `worker-internet` feature
  ([upgrading.md](upgrading.md)); `[]` works on older hosts too.

### `tailnet.objects.<Class>`

`<Class>` is a class name from `wrangler.jsonc` `durable_objects.bindings`.
A class listed here with no matching binding is an error.

| key | type | default | what it does |
|---|---|---|---|
| `tags` | `["tag:…"]` | | tags of each instance's own node |
| `agent` | boolean | `false` | the class is an Agents SDK `Agent` |
| `ttl` | string, e.g. `"10m"` | | lifetime from creation: then the object (and its sub-agents) is killed. The default for `spawn()`, which can set its own |
| `node` | boolean | `true` | `false`: an internal class with no tailnet node (implies `"egress": "workload"`) |
| `egress` | `"object"` \| `"workload"` | `"object"` | whose node outbound `fetch()` leaves from ([egress-modes.md](egress-modes.md)). Changing it later empties the class's storage |
| `fence` | `"never"` | | instances tolerate running on two hosts at once, so mgmt can move the workload off a dead host without `--force-takeover` |
| `public.create` | boolean | `false` | public requests may create new objects of this class ([public-ingress.md](public-ingress.md)) |
| `public.maxObjects` | integer | | required with `create`: the most objects public requests may create |

## `placement`

How mgmt picks a host when there is more than one
([INSTALL.md](INSTALL.md#where-a-workload-runs-placement-multi-host)).

| key | type | default | what it does |
|---|---|---|---|
| `mode` | `"smart"` \| `"off"` | `"smart"` when there are targets | `"off"` places by free capacity only |
| `targets` | `[tag or name]` | `tailnet.connects` | the peers to place close to |
| `aggregate` | `"worst"` \| `"sum"` | `"worst"` | how several targets combine |
| `host`, `hostname` | string | | Cloudflare's placement hints: one target |
| `region` | string | | accepted for Cloudflare compatibility and ignored |

## `public`

Paths served to the public internet over Funnel
([public-ingress.md](public-ingress.md)). Without `public` the workload is
tailnet-only.

| key | type | what it does |
|---|---|---|
| `routes` | `["/x", "/x/*", "/*"]` | **Required.** exact paths, prefixes, or everything |
| `upgrade` | `[route]` | routes (also in `routes`) that may upgrade to WebSocket or SSE |
| `limits` | object | `rps`, `burst`, `ipRps`, `ipBurst`, `maxBodyBytes`, `maxHeaderBytes`, `timeoutSeconds`, `maxConns`, `maxConnsPerIp`, `upgradeLifetimeSeconds`, up to tailworks' ceilings |

## Tags and `tw init`

`tw init <dir> --template …` names the template's tags after your app:
`tag:tw-wl-<workload>` for the workload node, and
`tag:tw-<workload>-<class>` for each class (`SupportAgent` becomes
`support-agent`). Two apps made from the same template then don't share
tags, so they don't share grants either. `--keep-tags` keeps the template's
tag names.

Each new tag needs a `tagOwners` entry in your tailnet policy. `tw deploy`
checks this first and prints the lines to add:

```jsonc
"tagOwners": {
  "tag:tw-wl-support":            ["autogroup:admin", "tag:tw"],
  "tag:tw-support-support-agent": ["autogroup:admin", "tag:tw"],
  ...
}
```

`tw policy --apply` (or `tw deploy --apply-policy`) writes those lines for
you, along with the `tailnet.connects` grants and, for `public` routes,
`tag:tw-public` and its funnel `nodeAttrs`. It prints the lines first and
asks, or takes `--yes` when there's no terminal. mgmt then reads the policy
with its ETag, merges in only the missing lines (comments kept, nothing
changed or removed, nothing outside `tag:tw*`), validates the result and
writes it with `If-Match`. If someone saved the policy in the meantime, it
retries once, then stops with
[`policy_conflict`](errors.md#policy_conflict). The write is audited
(`tw audit --all`, kind `policy_apply`). It needs the tenant's OAuth client
to have the **Policy File** write scope (not just Read). Without that scope,
copy the snippet `tw policy` prints into the admin console.

When `tailnet.connects` names another tailworks app's tag that isn't defined
yet (say `tag:tw-wl-mcp`, before `mcp` is deployed), `tw policy` adds its
`tagOwners` entry, the same one deploying `mcp` would add, so the grant to
it can be written. A tenant with its own owner tag (a team) doesn't get
this: the tag may belong to another team's app, so you define it yourself.

Some lines are never written: a grant to `*` or `autogroup:*`, a grant to a
tag outside `tag:tw*` that your policy doesn't define (such as `tag:db`
before you add it to `tagOwners`, which the control plane would reject), a Service's
`autoApprovers`, and the grant that lets people call the app. `tw policy`
lists them for you to add yourself.

If you'd rather not edit the policy for every app, reuse a small set of tags
that an admin owns once, e.g. `tag:tw-app` for workloads and
`tag:tw-agent` for agent classes. The catch: every app with the same tag
gets the same grants. Grants are made per tag, so per-app tags are the only
way to give two apps different access.

### Reusing existing tags

When your tailnet policy is managed by someone else, `tw init` can use tags
the policy already has, so a new app needs no policy change:

```sh
tw init billing --template agent --tags tag:tw-app --object-tags tag:tw-agent
tw policy    # nothing to add when the policy already owns and grants these tags
```

- `--tags tag:a,tag:b` sets the workload node's `tailnet.tags`.
- `--object-tags tag:a,tag:b` sets the tags of every Durable Object class
  that has its own node. `--object-tags <Class>=tag:a,tag:b` sets one class,
  and wins over the form without a class. Repeat the flag for more classes.
- Tags you don't set are still named after the app (or kept, with
  `--keep-tags`).

`tw init` checks the tags before it writes anything: they must be valid
tags, can't be one of tailworks' own tags
([`reserved_tag`](errors.md#reserved_tag)), and each class you name must
have a node of its own in the template. The policy itself is checked later,
by `tw policy` and `tw deploy`'s preflight: the tags still need a
`tagOwners` entry that includes `tag:tw` (or your team's owner tag,
[teams.md](teams.md)), and the grants for `tailnet.connects`.

The tags are written to `tailworks.jsonc`, which has no environments, so
`tw deploy --env <name>` uses the same tags. A project with only a wrangler
config instead gets `tag:tw-wl-<name>-<env>` (unless the env sets its own
`name`), a new tag per environment: add a `tailworks.jsonc` to keep one set.
