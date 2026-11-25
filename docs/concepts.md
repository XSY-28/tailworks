# tailworks concepts

The words tailworks' docs and CLI use, in the order you meet them. Where
Cloudflare uses a different word, it's in the entry.

## Tailnet and policy

### Tailnet
Your Tailscale network (or a self-hosted [Headscale](headscale.md)
server). Tailworks adds nodes to it; it never uses your machine's own Tailscale
app or tailscaled.

### Node
A device on the tailnet, with its own MagicDNS name, IP addresses and tags.
Tailworks runs its nodes in-process (tsnet). mgmt has one, every
[workload](#workload) has one, and so does every [object](#object) of a
class that has tags.

### Tag
A label on a node, like `tag:tw-wl-support`. Tags are what grants and
tagOwners name. `tw init` names an app's tags after the app
([tailworks-jsonc.md](tailworks-jsonc.md#tags-and-tw-init)).

### tagOwners
The part of the tailnet policy that says who may create nodes with a tag.
Every tag of your app needs an entry that includes `tag:tw`, or tailworks can't
create its nodes. `tw deploy` checks this and prints the lines to add.

### Grant
A rule in the tailnet policy that lets a source (users, groups, tags) reach
a destination (tags) on some ports. Grants decide who can call a workload
and what a workload can call. Nothing reaches a new workload until a grant
allows it: see the [quick start](../README.md#quick-start)
and [first-deploy troubleshooting](troubleshooting.md).

### Caller
Whoever sent a request, as WireGuard identifies them: a user login, or a
node's tags. `getCaller()` returns it. Tailworks has no tokens or API keys for
callers (Cloudflare would use Access JWTs).

## What tailworks runs

### Tenant
One tailnet that tailworks serves, with its own credential, nodes, storage and
audit trail. `tw up` creates the first one; `tw tenant add` adds another
tailnet to the same install. Most installs have one tenant.

### mgmt
The control plane, `tw-mgmt`: it holds tenants, deployments, objects,
the audit trail and the key custody, mints every node's auth key, and
serves the `/v1` API and the admin console on its node `tw-mgmt`.

### Host
A machine that runs workloads, through the host agent `tw-hostd`. By
default `tw up` runs the host agent inside mgmt's process, so there is
one process and one infrastructure node. `tw host add` adds more hosts,
each with its own `tag:tw-host` node.

### Workload
One deployed app: the `name` in `tailworks.jsonc`. Cloudflare calls it a
**Worker**. A workload has its own node, tagged with `tailnet.tags`, whose
address `tw deploy` prints (`http://tw-<name>-<id>.<tailnet>.ts.net/`).
Deploying a new version replaces it in place; deploying under another name
makes a second workload.

### Object
One Durable Object instance (Cloudflare: a Durable Object). Each object of
a class with tags in `tailnet.objects` gets its **own node**, with the
class's tags, so it has its own identity for calls in and `fetch()` out,
and `tw kill` can cut it off on its own. `tw ls` lists objects with their
nodes. Each of those nodes is a device on your tailnet for as long as the
object lives, so a class with many objects counts against your Tailscale
plan's device limit; a class with `"node": false` adds none
([Tailnet node budget](limits.md#tailnet-node-budget)).

### Sub-agent
An object created by another object with `ctx.tailnet.spawn()`. It gets
at most its parent's grants, and `tw kill` kills it with its parent.

## How it runs

### Process tier
The default way to run a workload: workerd (or Node.js, with
`"runtime": "node"`) as a process on the host, in a per-workload sandbox
(Seatbelt on macOS, Landlock and seccomp on Linux). Good isolation for your
own code, not a VM boundary: see [Isolation](INSTALL.md#isolation).

### microVM tier
Each workload in its own virtual machine: Firecracker on Linux with KVM,
Virtualization.framework on macOS. Set `"tier": "microvm"` in `tailworks.jsonc`.
On Linux it needs a one-time `sudo tw host setup --microvm`
([INSTALL.md](INSTALL.md#linux-the-microvm-tier-firecracker-kvm)).

### Facet mode and plain-DO mode
Two ways a Durable Object class can run, set per class with `"egress"`.
**Facet mode** (the default, `"egress": "object"`): each object's outbound
`fetch()` leaves from its own node. **Plain-DO mode**
(`"egress": "workload"`): outbound `fetch()` leaves from the workload's
node, shared by all its objects. Calls in reach the object's own node in
both. Switching a class's mode later starts its objects with empty storage.
The full comparison is in [egress-modes.md](egress-modes.md).

### Pool node (`_pool`)
A node that tailworks joins to your tailnet ahead of time, so that the next new
object of a class gets a node in about 0.2 s instead of joining a new one.
`tw ls` lists them under `_pool`, so you'll see them on a stock install.
Idle ones are deleted after an hour without new objects. `tw up
--pool-nodes N` sets how many per class that had a new object in the last
hour (default 1; 0 turns them off). See [Tuning](INSTALL.md#tuning-how-fast-a-new-objects-own-node-comes-up).

### Warm keys
Single-use auth keys that tailworks mints ahead of time, so a new object doesn't
wait for the key API. Unused ones are kept in tailworks' custody for up to 30
minutes; `tw down --purge` revokes them. `tw up --warm-keys N` sets how
many (default 5 per tag set; 0 turns them off).

## Using it

### Developer machine and `tw link`
The machine you deploy from, if it isn't the host. `tw link` points its
CLI at mgmt over your own Tailscale connection; mgmt knows you by your
login, so there's no key to copy.

### `tw dev`
Runs an app on your machine with the same host agent and workerd as
production, against an embedded test control plane, so you don't need a
tailnet ([INSTALL.md](INSTALL.md#try-it-without-a-tailnet)).

### Kill
`tw kill <object>` takes an object's node off the tailnet and leaves a
tombstone: later requests get `410 killed`. It kills the object's
sub-agents too (`--cascade=false` leaves them to the reaper, which kills
the sub-agents of a dead parent within seconds).
