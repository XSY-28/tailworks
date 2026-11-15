# My first deploy doesn't answer

The usual reasons a first `twd deploy` fails, or succeeds but the workload
doesn't answer, in the order people hit them. For problems with `twd up`
itself, see [INSTALL.md › Troubleshooting](INSTALL.md#troubleshooting).
Every error code links to its entry in [errors.md](errors.md), and
`twd docs show errors.md#<code>` prints it offline.

The examples use the quick start's `support` app: workload tag
`tag:twd-wl-support`, object class tag `tag:twd-support-support-agent`.
Your tags are in your app's `twd.jsonc` (`tailnet.tags` and
`tailnet.objects`); the words are explained in [concepts.md](concepts.md).

## `curl` hangs, or the name doesn't resolve

`twd deploy` said `✓ deployed`, but
`curl http://twd-support-<id>.<tailnet>.ts.net/…` hangs or fails with
`Could not resolve host`.

**Most likely: no grant lets you reach the workload.** A tailnet policy
that doesn't allow everything lets nothing reach a new tag until a grant
does, and without a grant the node isn't even in your device's netmap.
`twd deploy` doesn't check who may call your app, so the deploy succeeds
anyway. Add a grant to the workload's tag on port 80 (workload nodes listen
on port 80):

```jsonc
"grants": [
  // everyone in the tailnet may call the support app
  {"src": ["autogroup:member"], "dst": ["tag:twd-wl-support"], "ip": ["tcp:80"]},
]
```

`twd deploy` prints this line next to the tagOwners snippet when it finds
new tags. Grants apply within seconds of saving the policy; nothing needs a
restart.

- `autogroup:member` is every **user** of the tailnet. A tagged device
  (a server, a CI runner, another workload) is not a member: name its tag
  in `src`.
- To call an object on its **own** node (its name in `twd ls`'s NODE
  column) rather than through the workload's URL, grant its class tag too
  (`tag:twd-support-support-agent`).
- To check from the calling device: `tailscale status` lists the workload
  node once a grant lets you see it, and `tailscale ping
  twd-support-<id>` tells you if it answers.
- Still nothing? Is the calling device on the same tailnet as twd, and
  connected? Then `twd status` and `twd ls` on the host: the deployment
  should be `active`.

## `identity_pending`

From **`twd deploy`**: `error [identity_pending]: support not ready after
3m0s (status …, node …)`. The deployment was placed, but its workload node
never became ready within `--wait` (3 minutes by default). The status and
node in the message say where it stopped.

- `twd status` and `twd logs support`; `twd logs --component hostd` on the
  host shows the node joining.
- If the deploy said `preflight skipped`, mgmt couldn't read your tailnet
  policy, so it didn't check your tags first. A tag with no tagOwners
  entry then fails when twd mints the node, usually as
  [`bootstrap_failed`](errors.md#bootstrap_failed). Add the tagOwners (see
  [below](#a-tag-isnt-in-tagowners-after-renaming-the-app)), and give the
  credential Policy File › Read so later deploys check it.
- A control plane that is slow to register nodes can push a first deploy
  past the limit: `twd deploy --wait 10m`.

From **a request** (`503`, header `twd-error: identity_pending`,
`Retry-After: 1`): a new object's own node isn't ready yet. The first
request to a new object normally waits for it (about 0.2 s with the
default [pool nodes](concepts.md#pool-node-_pool)), and answers
`identity_pending` only if the node takes longer than 30 s. Retry. If it
keeps happening, `twd ls` shows the object's status and `twd logs
support` the reason.

## A tag isn't in tagOwners after renaming the app

`twd deploy` prints `› preflight: tagOwners tag:… … ✗ not in tagOwners`
and fails with [`acl_denied`](errors.md#acl_denied).

`twd init` names an app's tags after the app (`tag:twd-wl-<name>`,
`tag:twd-<name>-<class>`; see
[twd-jsonc.md](twd-jsonc.md#tags-and-twd-init)). If you rename the app
and its tags, or run `twd init` again under a new name, the new tags need
their own tagOwners entries. `twd deploy` prints them: add them and deploy
again. Two more things a rename changes:

- Your caller grant (above) still names the old tag. Update its `dst`.
- A new `name` is a new workload. The old one keeps running under its old
  name until you `twd undeploy <old-name>`.

## `peer_not_visible` when the workload calls something

A `fetch()` from your workload to another tailnet device fails with `403`,
header `twd-error: peer_not_visible`. The destination isn't in the calling
node's netmap: no grant lets that node see it, or the name doesn't exist.
([`acl_denied`](errors.md#acl_denied) is the next step: the peer is
visible, but not on that port.)

Which node calls depends on the class's mode
([concepts.md](concepts.md#facet-mode-and-plain-do-mode)): in facet mode,
the default, `fetch()` leaves from the **object's own node**, so the grant's
`src` is the class tag; from the workload's own code, or a plain-DO class
(`"egress": "workload"`), it's the workload tag.

The easy way to get the grant right: list the destination's tag in
`tailnet.connects`, and `twd deploy` checks a grant from each of the app's
tags to it, printing any that are missing:

```jsonc
// twd.jsonc
{ "tailnet": { "tags": ["tag:twd-wl-support"], "connects": ["tag:db"] } }
```

```jsonc
// tailnet policy: the line twd deploy prints (port 80; change it to the port the destination listens on)
{ "src": ["tag:twd-support-support-agent"], "dst": ["tag:db"], "ip": ["tcp:80"] },
```

`twd dev` has no policy evaluator (every dev node sees every other), so a
call that works in `twd dev` can still need a grant once deployed.
Destinations outside the tailnet, the host itself and sibling objects are
refused with [`forbidden_destination`](errors.md#forbidden_destination)
instead.

## The deployment is `failed` in `twd ls`

`twd deploy` failed with [`deploy_failed`](errors.md#deploy_failed), or
`twd ls` shows the deployment as `failed` with an `error:` line under it:

```
  support      dp_…                   h1       failed     http://…/
               error: workerd exited 6 times: … · twd logs support --console
```

The host couldn't start the app: workerd (or Node) exited. The `error:`
line is the first line of what it printed. `twd logs support --console`
shows the app's raw output (workerd's log, the Node app's log, or the
microVM console), usually with the exception or the config error that
stopped it. Fix it and `twd deploy` again. If the app runs under `twd dev`
but not deployed, compare the tier and runtime (`"tier"`, `"runtime"` in
`twd.jsonc`) and any [secrets](secrets.md): `.dev.vars` is for `twd dev`
only, and `twd secret put` is applied on the next deploy.
