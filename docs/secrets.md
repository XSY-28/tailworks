# Worker secrets

`wrangler.jsonc` `vars` are plaintext and live in git. For API keys and
other credentials, twd has **secrets**, which work like Cloudflare Worker
secrets:

- You set a secret with `twd secret put <NAME>`. The value is read from
  stdin, never from the command line.
- Your code reads it as `env.NAME`, a string, in the Worker and in every
  Durable Object class. For `"runtime": "node"` it is
  `process.env.NAME`.
- A secret overrides a var of the same name.

The design and the threat model are in
[ADR-034](design/adr/034-worker-secrets.md).

```sh
cd my-agent                         # the app: the workload is its name, as for twd deploy
twd secret put LLM_API_KEY          # hidden prompt on a terminal
printf %s "$KEY" | twd secret put LLM_API_KEY   # or from a pipe (one trailing newline is dropped)
twd secret ls                       # names, last update and who set them; never values
twd secret rm LLM_API_KEY
twd deploy                          # secrets take effect on the next deploy
```

```ts
export default {
  async fetch(req: Request, env: { LLM_API_KEY: string }) {
    const r = await fetch("https://api.example.com/v1/chat", {
      headers: { authorization: `Bearer ${env.LLM_API_KEY}` },
    });
    // …
  },
};
```

## Commands

| Command | What it does |
|---|---|
| `twd secret put <NAME>` | Creates or replaces a secret. It reads the value from stdin: a hidden prompt on a terminal, or everything piped in, less one trailing newline. A value given on the command line is refused and never echoed. |
| `twd secret ls` | Lists the secret names with their last update time and actor. No command or API ever returns a value. |
| `twd secret rm <NAME>` | Deletes a secret. |

All three take these flags:

- `--workload <w>`: defaults to the app in the current directory, named the
  way `twd deploy` names it.
- `--tenant <t>`: defaults to the `twd up` tenant.
- `--json`

The commands work on the host and from a `twd link`ed laptop: both go to
mgmt's `/v1` API, and over the tailnet they need the admin capability
(`twd.dev/cap/admin`), like every other write.

The workload doesn't have to be deployed yet, so you can put its secrets
before the first deploy. With `"replicas": N`, every replica gets the
workload's secrets. Each replica is released them under its own lease.

**Names** are JS identifiers that are also environment variable names
(`[A-Za-z_][A-Za-z0-9_]*`, up to 128 characters). Names starting with
`TWD_` or `__TWD` are reserved. For `"runtime": "node"`, these are also
refused because twd sets them: `PATH`, `HOME`, `TMPDIR`, `NODE_ENV`,
`PORT`, `HOST` and the proxy variables. A secret can't have the name of a
Durable Object binding.

**Limits** are Cloudflare's: 128 secrets per workload and 5 KiB per value
(UTF-8 text).

## When changes take effect

A running workload keeps the secrets it started with. A new or changed
value reaches it on its **next deploy**: run `twd deploy` again, the same
as for a changed var. A restart of twd-hostd also refetches secrets, since
it redeploys from its assignments. A crash restart of the same instance
(workerd or node exiting) reuses the same values.

Secrets are not versioned with deployments. `twd rollback` redeploys an
older bundle with the secrets the workload has **now**, so a secret you
rotated stays rotated and a secret you deleted stays deleted.

## Local development: `.dev.vars`

`twd dev` reads `.dev.vars` from the app directory, if there is one, the
same way `wrangler dev` does. It's a dotenv file:

```sh
# .dev.vars — never commit this
LLM_API_KEY=sk-local-…
GREETING="multi\nline"
```

Every entry becomes a secret of the dev workload, so it overrides a var of
the same name. It reaches your code through the same path as
`twd secret put` in production. If you edit the file, the app reloads.

The format:

- Blank lines and `#` comments are skipped.
- `export ` is allowed before a name.
- `"double quotes"` allow `\n` escapes and may span lines.
- `'single quotes'` are literal.
- Unquoted values end at ` #`.

The templates' `.gitignore` lists `.dev.vars`.

## Where values live

- **At rest:** mgmt's custody, one sealed entry per workload
  (`twd/<tenant>/workloads/<workload>/secrets`). The homelab file custody
  is AES-256-GCM with the entry name bound as additional data; setec
  custody keeps its own encryption. mgmt's database never holds a value.
- **In flight:** a value leaves mgmt only in a secret grant
  (`POST /v1/tenants/{id}/workloads/{w}/secret-grants?epoch=N`). mgmt
  answers it only to the host that holds the workload's lease at that
  epoch, the same fence that protects microVM volume keys. The answer
  carries `Cache-Control: no-store`.
- **On the host:** the values go into the instance directory only, which
  is `0700` and is deleted when the instance stops:
  - workerd: text bindings in `config.capnp`, plus `twd-config.json`,
    where facet Durable Objects get their `env`;
  - runtime `node`: a `0600` file there, which a `--require` preload
    copies into `process.env` at start-up and then deletes (every start
    writes it again). The values are deliberately **not** in the node
    process's exec-time environment: on macOS any process of the same user
    can read another process's environment (`ps -E`, `KERN_PROCARGS2`),
    and Seatbelt can't refuse that read.

  The process sandbox keeps every other tenant's workloads out of that
  directory and, on Linux, out of other processes' `/proc` entries.
- **Never logged or audited.** The audit records `secret_put`,
  `secret_delete` and `secret_release` carry names only.

## Not supported yet

- **The microVM tier.** A `"tier": "microvm"` deployment of a workload that
  has secrets fails with `secrets_unsupported_tier`. On a cold boot, the
  microVM's config drive (a tar of the instance directory) is written to
  the VM's run directory, outside the instance directory. Deploy such
  workloads with `"tier": "process"` for now, or keep the secret out of
  the workload.
- `wrangler secret bulk`, secret versions and gradual deployments, and the
  Secrets Store binding (`secrets_store_secrets`).
- Restarting a running workload when a secret changes. Redeploy instead.
