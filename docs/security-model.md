# Security model

How tailworks decides who is calling, what each workload can reach, and what
stands between tenants. Read this first; the red team's detailed view is
in [THREAT-MODEL.md](../security/THREAT-MODEL.md) (a dated assessment),
[RED-TEAM.md](../security/RED-TEAM.md) (findings and fixes) and
[LEDGER.md](../security/LEDGER.md) (what is still open). To report a
vulnerability, see [SECURITY.md](../SECURITY.md).

## Identity: WireGuard, not tokens

- **Every workload and every Durable Object instance is its own tailnet
  node**, with its own MagicDNS name and the tags from `tailworks.jsonc`. Your
  tailnet policy (grants) decides who may reach which node, as for any
  other device.
- **Callers are identified by the network.** netd asks the tailnet who
  owns the WireGuard peer (WhoIs) and puts the answer in `request.cf`.
  `getCaller(req)` returns that node, its user (for a person's device),
  its tags and the app capabilities its grants carry (`caps()`). Client
  copies of identity headers are stripped on the way in, by prefix
  (`Tailscale-*`, `x-tw-*`, `tw-*`, `X_TW_*`, `tw_*`, `CF-*`,
  `X-Forwarded-*`) and by name (`Forwarded`, `X-Real-IP`, …;
  `isIdentityHeader` in `netd/ingress.go`, [COMPAT.md](COMPAT.md#identity)).
  There are no bearer tokens between workloads. This holds between nodes;
  inside one workload, see [Known gaps](#known-gaps).
- **tailworks' own API works the same way.** tw-mgmt serves `/v1` on its
  tailnet node and authorizes each request by WhoIs: admins hold
  `github.com/rosskukulinski/tailworks/cap/admin`, approvers `github.com/rosskukulinski/tailworks/cap/approve`, MCP tool users
  `github.com/rosskukulinski/tailworks/cap/mcp`, and a team's own tenants `github.com/rosskukulinski/tailworks/cap/tenant-admin`
  ([agents.md](agents.md), [approvals.md](approvals.md), [teams.md](teams.md)).
  `tw link` stores only mgmt's address; no key or token.
- **One credential mints node keys:** an OAuth client for a Tailscale
  tenant, or the Headscale API key for a Headscale tenant
  ([INSTALL.md](INSTALL.md#the-oauth-client)).
  Each object's key is single use, pre-authorized, tagged and expires
  within 5 minutes (`KeyExpiry` in `mgmt/types.go`). Warm keys, minted
  ahead of time so new objects start faster, wait in mgmt's custody for up
  to 30 minutes ([INSTALL.md](INSTALL.md#tuning-how-fast-a-new-objects-own-node-comes-up)).
  `tw status` shows how many each tenant has; `tw down` and `tw tenant rm`
  revoke them. Revoking the credential
  stops all future minting.
- **Kill is a network cut.** `tw kill` tombstones the object, its node
  stops answering (410), and with the Devices scope the device is deleted.

## Isolation tiers

| Tier | Where | What separates a workload from the host and other tenants |
|---|---|---|
| **process** (default) | macOS, Linux | V8 isolates (workerd) or a Node.js process, running as tailworks' own user inside a per-workload OS sandbox (`tw-hostd -sandbox auto`): Seatbelt on macOS, Landlock + seccomp on Linux. What each sandbox confines is below; it differs a lot by platform. |
| **microVM** (`"tier": "microvm"`) | Linux with KVM (Firecracker), macOS on Apple silicon (Virtualization.framework) | A VM per workload. On Linux, Firecracker runs under its jailer (own chroot, uid, network namespace, cgroup) and Durable Object storage sits on a per-workload encrypted volume whose key mgmt releases only to the host that holds the workload's lease. Node keys stay on the host and never enter the guest ([containers.md](containers.md), [INSTALL.md](INSTALL.md#linux-the-microvm-tier-firecracker-kvm)). |

The process tier is not a VM boundary. Don't run code you don't trust on
it.

### What the process-tier sandbox confines

| | macOS (Seatbelt) | Linux, Landlock ABI ≥ 4 (kernel 6.7+) | Linux, Landlock ABI 1–3 |
|---|---|---|---|
| Profile shape | `(allow default)` (Mach services, sysctls) with writes, network, signals and exec denied, then allowed back for what the workload needs, plus a read deny list (`seatbeltProfile` in `hostd/seatbelt_strict.go`) | allow list (`internal/procsb`) | allow list (`internal/procsb`) |
| Reads | denied under your home dir, tailworks' state and run dirs, hostd's data and run dirs, and every tenant's credential files; **everything else is readable** (`/etc`, `/tmp`, …) | only the OS dirs (`/usr`, `/lib`, `/bin`, `/etc`, `/opt`, …), the runtime binary (and a Node install prefix), a few global `/proc` and `/sys` files, the process's own `/proc` entries and its own dirs | same as ABI ≥ 4 |
| Writes | own dirs only (plus `/dev/null` and similar devices); `/tmp` and the rest of the disk are refused | own dirs only | own dirs only (ABI 1 also refuses renames and links across directories) |
| Unix sockets | only its own (in its own dirs); every other socket, including the system tailscaled socket and DNS (mDNSResponder), is refused | none: seccomp refuses `socket(AF_UNIX)` (except a Node app in `listen: "unix"` mode) | same as ABI ≥ 4 |
| TCP | connect only to `localhost:<port>` for the ports hostd gives it (a Node app: its egress proxy and its own port; workerd: none); bind only its own port | connect only to the ports hostd gives it (a Node app: its egress proxy and its own port; workerd: hostd's loopback services); bind only its own port. Landlock matches port numbers, not addresses | **unrestricted** (Landlock has no TCP rules before ABI 4) |
| Other sockets | none (no UDP, so no DNS of its own; netd resolves names) | TCP only for IPv4/IPv6 (no UDP, ICMP, raw or MPTCP); netlink route only | same as ABI ≥ 4 |
| Signals | only itself and its own children (`same-sandbox`): not tw-hostd, tw-mgmt or another workload | only itself and its own process group | same as ABI ≥ 4 |
| Other syscalls | not filtered; exec only of the runtime binary, its own dirs and the OS dirs (`/bin`, `/usr`, `/opt`, …) | seccomp refuses ptrace, `process_vm_readv/writev`, mount, bpf, perf, keyrings, new namespaces, kexec, modules, io_uring, … | same as ABI ≥ 4 |
| Node-tier egress (`tw status`) | `enforced` | `enforced` | `unenforced`: the egress proxy is cooperative |

On every platform the process tier caps each workload's V8 heap and sets
no other CPU or memory limit (see
[Availability between tenants](#availability-between-tenants)), and the
workload runs as the same user as tailworks.

So on Landlock ABI 1–3, a hostile Node app, or workerd code after a V8
escape, can open TCP connections with the host's network identity
(including to other tenants' loopback ports and to services on your LAN).
[node-tier.md](node-tier.md) describes what the Node tier still guarantees
there. On macOS, `tw-hostd -sandbox seatbelt-legacy` keeps the previous
profile for one release in case the strict one breaks a workload: it
confines reads, writes and unix socket connects under the deny list only,
so TCP, signals to tailworks' own processes and writes elsewhere (`/tmp`)
are open again, and `tw status` reports Node egress as `unenforced`.

### When a host is shared between tenants

tailworks runs a second tenant's process-tier workloads on a host only when
the host passes these checks, unless you start it with
`tw up --insecure-shared-host` (tenants are then not isolated from each
other):

- **No sandbox** (Linux without Landlock, or `-sandbox none`): the second
  tenant is refused (`sandbox_unavailable`), and so is a Node app on a host
  that already serves another tenant (`egress_unenforced`).
- **Your user can reach a container engine** (or is in the
  docker/podman/lxd/incus groups): refused.
- **Your user can reach the system tailscaled socket**: refused on Linux
  when a Node app uses `listen: "unix"`. (On macOS the Seatbelt profile
  refuses every unix socket outside the workload's own dirs; under
  `-sandbox seatbelt-legacy` it refuses only those under the deny list,
  and hostd doesn't check for the tailscaled socket.)

A host whose sandbox works but doesn't confine TCP (Landlock ABI 1–3, or
macOS with `-sandbox seatbelt-legacy`) **is admitted** as a shared host.
`tw status` reports its Node workloads as `egress unenforced` and
`tw doctor` warns. If you share such
a host between tenants that don't trust each other, use
`"tier": "microvm"`, a Linux host with kernel 6.7+, or macOS with the
default Seatbelt profile.

### Availability between tenants

The sandboxes above are about confidentiality and integrity: what a
workload can read, write and reach. They don't give tenants their own
share of the machine. What each tier does about one tenant using up a
host:

| | process tier | microVM tier |
|---|---|---|
| Memory | each V8 isolate's heap is capped (`tw-hostd -workerd-heap-mib`, default 1024 MiB; a Node app's `--max-old-space-size`, default 2048 MiB). Past it, that workload's workerd or Node process aborts and hostd restarts it. Memory outside the V8 heap (native buffers, WebAssembly memory) is not capped | the guest's memory (`-vm-mem-mib`, default 512 MiB) bounds everything in the workload |
| CPU | not limited: a busy workload competes with every other process on the host | the guest's vCPUs (`-vm-vcpus`, default 2) |
| Disk, logs, telemetry | bounded per workload ([limits.md](limits.md)) | as on the process tier, plus the guest's volume (`-vm-volume-mib`) |
| Requests | not limited per workload on the tailnet: your tailnet policy decides who may call it. Public (Funnel) traffic is rate-limited per workload and per client IP | the same |

So **availability isolation between tenants is not provided on the
process tier**: the heap cap stops one runaway allocation from taking the
host's memory, but a tenant can still slow its neighbours down with CPU,
native memory or a flood of requests from callers your policy lets in.
If tenants that don't trust each other share a host and that matters,
put them on the microVM tier or on separate hosts.

mgmt protects itself from its tailnet callers: each WhoIs node has its
own request-rate bucket (hosts a larger one), and non-host callers share a
bounded number of in-flight requests, so neither a script nor a flood
from one admin's machine can starve the hosts' heartbeats. Over either
limit is `429 rate_limited` ([limits.md](limits.md#management-api)).

## Egress

- **Tailnet egress** leaves from the workload's node, or from the object's
  own node in facet mode ([egress-modes.md](egress-modes.md)), so your
  tailnet policy decides what each object can reach, and the other side's
  `getCaller` sees which object called. Tailnet names resolve from the
  node's netmap only, never from host DNS; a tailnet name that isn't in the
  netmap is refused (`peer_not_visible`).
- **Internet egress is on by default** (ADR-049). A
  `fetch()` to a public host leaves from the **host's own network**, never
  from a tailnet node: the destination sees the host's source address
  (different on each host) and no tailnet identity, and tailworks strips
  its identity headers. `tailnet.egress.internet: false` turns it off and a
  list limits it ([tailworks-jsonc.md](tailworks-jsonc.md#tailnetegressinternet));
  `tw-hostd -worker-internet=off` turns it off for a whole host. A refused
  destination is `internet_egress_unavailable`.
- **The SSRF guard always applies.** netd refuses loopback, private
  (RFC 1918, ULA), link-local (including `169.254.169.254` and other cloud
  metadata), CGNAT `100.64.0.0/10` and the tailnet ranges, the host's own
  addresses, and translated forms of these (IPv4-mapped, NAT64, 6to4,
  Teredo), with `forbidden_destination`. It checks the address it actually
  dials, after DNS: a name with **any** forbidden answer is refused (no DNS
  rebinding), and names under special-use suffixes (`.internal`, `.local`,
  `.localhost`, `.lan`, …) are refused without being looked up.
- **What host DNS sees.** An internet name that the workload's setting
  allows is resolved by the host's resolver, which sees the name (not which
  tenant asked). Tenants on one host share that resolver's cache. A
  workload that must not leak its lookups turns internet egress off and
  uses a gateway on the tailnet ([recipes.md](recipes.md#call-an-internet-api)).
- **Audit.** The first internet flow per object and destination is an
  `egress` audit record naming the address dialled; refusals are `deny`
  records.
- **What this means for your data.** A Worker that can read something on
  the tailnet can now send it to the internet. Give a workload that holds
  sensitive tailnet data `"internet": false` unless it needs the internet.
- This is netd's guard on the path every `fetch()` takes; code that
  escapes workerd's V8 sandbox, and a Node app, are held to it only where
  the OS sandbox confines TCP (see the table above). Containers' internet
  egress uses the same guard and the same setting
  ([containers.md](containers.md)).

## Trust boundaries

| Boundary | Who is trusted | What holds it |
|---|---|---|
| Tenant code ↔ tailworks | tailworks, never the tenant's code | the tier's sandbox or VM; the platform wrapper sets `request.cf`; egress goes through netd's guard |
| Tenant ↔ tenant on one host | neither | per-tenant netd registries (one tailnet's netmap each), per-workload sandboxes and 0700 state directories; one tenant only without a sandbox |
| Object ↔ object in a workload | the workload's code | per-object nodes and egress attribution; a sub-agent's capabilities are narrowed to what its spawner allowed (`lim`) |
| Host ↔ mgmt | mgmt | WhoIs `tag:tw-host`; every host write is fenced by its lease epoch |
| Admin's browser ↔ mgmt | mgmt's own pages only | every tailnet request must name mgmt in `Host` (its MagicDNS names or an IP literal), which defeats DNS rebinding; browser mutations need a same-origin `Origin`/`Sec-Fetch-Site` ([ADMIN-UI.md](ADMIN-UI.md#browser-origin-guard-csrf-dns-rebinding)) |
| Agents ↔ mgmt (`/mcp`) | mgmt | the same WhoIs gate as `/v1`; read tools need `…/cap/mcp` or the admin cap, and deploy, kill and undeploy need a grant that names them ([agents.md](agents.md)) |
| tailworks ↔ Tailscale control | the tailnet admin | one revocable mint credential; tags you own in `tagOwners` |
| Release ↔ host | the release signing keys pinned in each binary and script | `get.sh`, `install.sh`, `tw upgrade` and hostd's opt-in self-upgrade verify the release's signed `SHA256SUMS`; tw-mgmt can order an upgrade but can't sign one ([upgrading.md](upgrading.md)) |
| Internet ↔ workload | nobody | tailnet-only by default; public routes are opt-in paths over Funnel on `tag:tw-public`, and `getCaller` throws on a public request ([public-ingress.md](public-ingress.md)) |

## Secrets and state

- Worker secrets are sealed in mgmt's custody (AES-256-GCM per entry) and
  reach only the workload's own process; mgmt's database never holds a
  value ([secrets.md](secrets.md)).
- The state directory (`~/.tailworks`) holds node keys and the custody key.
  Back it up like a secret ([INSTALL.md](INSTALL.md#back-up-and-restore)).
- The audit trail is hash-chained, so truncation and gaps show
  (`tw audit`).

## Who you trust

| Who | What they can see and do |
|---|---|
| **Host operator** (root, or anyone who can act as the user tailworks runs as on a host) | Read and impersonate every tenant with workloads leased on that host, across tailnets. Each object's node state (keys) lives in 0700 directories that tw-hostd owns and uses; Worker secrets are plaintext in the 0700 instance directory ([secrets.md](secrets.md)); process-tier Durable Object storage is a plain directory; a microVM's encrypted volume is opened with dm-crypt on the host. Lease fencing and volume encryption protect a tenant from *other* hosts and from a copied disk image, not from the host that holds the lease. |
| **mgmt operator** (whoever runs tw-mgmt or can read its state directory) | Holds the custody key (sealed Worker secrets, microVM volume keys), each tenant's mint credential and warm keys, the database and the audit trail. Can mint node keys with a tenant's tags, read every secret, and choose which host runs which workload. |
| **Tailnet admin** | Owns the tags and grants. Decides who can reach which workload and object and which capabilities they carry, and can revoke the mint credential or remove devices. Sees every tailworks node as a device. |

On a single-machine install all three are usually you. When you run
tailworks for other people, they trust the operators of mgmt and of every
host their workloads are placed on.

## Telemetry and the update check

tailworks sends no telemetry, usage data or crash reports anywhere. (`tw
telemetry` configures OTLP export from your workloads to a collector you
run on your tailnet; nothing goes to the tailworks project.)

The only request the CLI makes without being asked is the update check:

- **What:** `GET https://api.github.com/repos/rosskukulinski/tailworks/releases/latest`
  with a `tw/<version>` User-Agent. Nothing else is sent: no identifiers,
  no tailnet or workload names. GitHub sees your IP address and the version.
- **When:** after a successful interactive command, at most once every 24
  hours. The answer is cached in `<state>/update-check.json`
  (`~/.tailworks` by default); when a newer release is out, `tw` prints a
  one-line notice on stderr.
- **Never:** after `tw init` or `tw dev` (both run with no tailnet, no
  credentials and no outbound request of their own; under `tw dev`, your
  Worker's internet `fetch()` calls do go out, as on a host), `tw version`, `tw completion` or
  `tw mcp`; with `--json`; when stderr isn't a terminal; with `CI` set; or
  in a development build.
- **Off:** `TW_NO_UPDATE_CHECK=1`. `tw version --json` reports whether it
  is on (`updateCheck`).

Commands you run about releases (`tw version --check`, `tw upgrade`) also
contact GitHub, to look up and download the release.

## Known gaps

The red team's open items are tracked in
[LEDGER.md](../security/LEDGER.md), each with a GitHub issue (label
`security`). Among them:

- **`request.cf` is not sealed inside a workload.** netd sets the caller
  for requests that arrive over the tailnet, but tenant code that forwards
  a request in-process (for example to a Durable Object stub, or by RPC)
  can attach any `cf` it likes, and the receiving object's `getCaller()`
  and `caps()` return it. So an object that authorizes on `getCaller()`
  trusts every other piece of code in its own workload. Across workloads
  and hosts the network identity holds (ledger C04, C05; H08).
- **Snapshot-restored microVMs share some kernel secrets.** On Linux,
  microVMs restored from one base snapshot (rebuilt at least hourly) get
  fresh randomness before tenant code runs. They still share the kernel
  secrets created before the snapshot: the TCP sequence-number and port
  keys, the hash-table seeds, and the kernel layout (on amd64 the layout
  is the same for every guest anyway: Firecracker guests there have no
  KASLR slide). A
  tenant can't use these against another tenant. A guest's TCP never
  leaves its host: it ends at that VM's own gateway, and guests have no
  network path to each other. Linux can't re-key these secrets after boot
  ([ADR-030](design/adr/030-microvm-snapshot-restore.md), ledger H24).
- **On-behalf-of assertions** (`actingFor`) aren't implemented.
- **Smart placement trusts each host's own path measurements.** A host
  reports only its own paths, only for tenants it already runs, within
  bounds, and a report never moves a running workload: a stateful move
  always needs `--force-takeover` (or a drain with
  `--allow-local-state-loss`). But a compromised host can claim the best
  path and so be chosen for that tenant's next new workload, or as the
  destination of a move you confirm. Check the placement summary that
  `tw deploy` prints, and pin with `--host` where it matters (ledger H23).
