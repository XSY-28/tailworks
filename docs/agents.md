# Using twd with a coding agent

twd ships **skills** that teach coding agents (Claude Code, Codex, Cursor,
Gemini CLI, OpenCode and others) how to drive the `twd` CLI. With them, you
can ask your agent to "build me an MCP server and run it on twd" and it
knows how to scaffold it, run it locally, deploy it, read its logs, debug a
failing deploy and stop it, without guessing at commands.

The skills are Markdown files in [`agent/skills/`](../agent/skills/), built
into the `twd` binary, so they always match the CLI you have installed.

## Install the skills

```sh
twd agent setup --dry-run      # what it would write, and where
twd agent setup                # asks before writing anything
```

`twd agent setup` looks for the coding agents on this machine (their config
directories in your home, and their binaries on `PATH`), prints every file it
would create or change, and asks before it writes. Run it again after
upgrading twd to update the skills; when nothing changed it does nothing.
`twd agent remove` deletes exactly what setup wrote.

| Agent | `--scope user` (default) | `--scope project` |
|---|---|---|
| Claude Code | `~/.claude/skills/<skill>/SKILL.md` | `.claude/skills/<skill>/SKILL.md` |
| Codex | a block in `~/.codex/AGENTS.md` | a block in `AGENTS.md` |
| OpenCode | a block in `~/.config/opencode/AGENTS.md` | a block in `AGENTS.md` |
| Gemini CLI | a block in `~/.gemini/GEMINI.md` | a block in `GEMINI.md` |
| Cursor | not supported (Cursor keeps user rules in its settings) | `.cursor/rules/<skill>.mdc` |

- Claude Code and Cursor get one file per skill. The `AGENTS.md` and
  `GEMINI.md` agents get one block: the core `twd` skill in full, and a list
  of the others, which the agent prints with `twd agent show <skill>` when it
  needs one.
- The block is delimited by `<!-- BEGIN twd agent skills -->` and
  `<!-- END twd agent skills -->`. Everything outside it is yours; setup and
  remove never touch it.
- Every file and block twd writes carries a marker comment. Setup won't
  overwrite a file without it (it reports `skip`), and remove only deletes
  files and blocks with it.
- `--agent claude,cursor` picks agents explicitly, for example to set up a
  project for an agent that isn't installed on this machine.
- `--scope project --dir <dir>` writes into a project (default: the current
  directory), so the skills are committed with it. The files contain no
  credentials.
- `--json` prints the plan (and the outcome) as JSON. Without a terminal,
  setup and remove need `--yes`; they never write without it.

## Without setup

- `twd agent ls` lists the skills, and which agents it found.
- `twd agent show <skill>` prints one; paste it, or point your agent at it.
- [`llms.txt`](../llms.txt) at the repo root indexes these docs for agents
  that read the repo.
- `twd docs search <words>` and `twd docs show <doc>[#<anchor>]` (for
  example `twd docs show errors.md#acl_denied`) read the docs offline.

## The skills

| Skill | For |
|---|---|
| `twd` | the entry point: what twd is, the rules, which skill for which task |
| `twd-scaffold` | `twd init` templates: Worker, Durable Object, Agent, MCP server, AI agent, Node workflow |
| `twd-dev` | `twd dev`, the `--once` smoke test, `.dev.vars` |
| `twd-deploy` | `twd deploy`, preflight failures, `twd ls`, `twd status` |
| `twd-debug` | `twd logs`, error codes, `twd doctor`, `twd audit` |
| `twd-policy` | tailnet policy: `tagOwners`, grants, MCP tool capabilities |
| `twd-secrets` | `twd secret`, without the agent ever seeing a value |
| `twd-cron` | cron triggers and testing them locally |
| `twd-public` | public routes over Funnel, `twd expose` |
| `twd-teardown` | `twd undeploy`, `twd kill`, and which commands ask first |

## Safety rules the skills give agents

- **Credentials stay with you.** The skills tell agents never to write API
  keys, OAuth client secrets or auth keys into files, commands or config.
  `twd secret put` reads a value from stdin; the skill asks the agent to let
  you type it. `twd up` and `twd link` need your tailnet credentials, so the
  agent asks you to run them.
- **Policy stays with you.** When a deploy needs a `tagOwners` line or a
  grant, twd prints it; the agent shows it to you and you add it in the
  Tailscale admin console. twd never edits the policy during a deploy.
- **The MCP server enforces it.** Over MCP, `kill`, `undeploy` and
  `deploy` exist for an agent only if a `twd.dev/cap/mcp` grant (or the
  operator's `twd mcp --allow`) names them. See
  [twd's MCP server](#twds-mcp-server).
- **Destructive commands need your go-ahead.** `twd kill`, `twd undeploy`,
  `twd down`, `twd tenant rm`, `twd host rm` and `twd secret rm` act
  immediately and have no undo. The skills tell agents to run them only after
  you've agreed to that specific action.
- **Confirmations aren't bypassed for agents.** `twd up`, `twd agent setup`
  and `twd agent remove` ask before acting. Without a terminal (which is how
  most agents run commands) they refuse with `bad_request` (`twd up`) or
  `confirmation_required` (`twd agent`) instead of proceeding. `--yes` skips
  the question; the skills tell agents not to add it on your behalf, and to
  show you `--dry-run` output instead.

## Output agents can parse

Every command takes `--json`. Results are one JSON document on stdout;
errors are `{"error": {"code", "message", "hint", "doc_url"}}` on stderr,
with codes from [errors.md](errors.md). `twd deploy` streams progress events,
and `twd dev` and `twd logs -f` stream one JSON object per line. Details and
the schema: [cli-json.md](cli-json.md).

## How the skills are kept honest

- `cmd/twd:TestSkillCommandsExist` runs every `twd …` command in every skill
  as `twd … --help`, which parses each flag. A skill that names a command or
  flag that no longer exists fails `make test`.
- `cmd/twd:TestAgentEval` is a scripted agent with no LLM. It replays the
  skills' marked command blocks (`<!-- twd-eval: <step> -->`) in order
  (`twd init`, `twd dev --once`, `twd deploy`, `twd ls`, `twd logs`,
  `twd undeploy`) against a real twd on the hermetic test harness. That
  includes the first deploy failing with `acl_denied` and succeeding once
  the `tagOwners` line from the error's hint is added. Every output is checked
  against the schema.
- `cmd/twd:TestAgentSetupSnapshot` snapshots `twd agent setup --dry-run` for
  a home directory with five agents' config dirs, and checks that setup is
  idempotent and remove restores your files byte for byte.

## twd's MCP server

As well as the CLI, an agent can operate twd through **MCP**. mgmt serves
an MCP server at `/mcp`, next to its `/v1` API, behind the same tailnet
identity check. The design is in
[ADR-035](design/adr/035-mgmt-mcp.md).

| Tool | What it does | Who gets it |
|---|---|---|
| `whoami` | who twd thinks you are, your tools, the grant for the rest | everyone admitted |
| `status`, `ls`, `logs`, `audit` | like the CLI commands (`logs` gives request records, `console: true` the raw output) | twd admins and any `twd.dev/cap/mcp` holder |
| `deploy` | deploy a bundle, or redeploy (`replace: true`: re-place) the current one | a `twd.dev/cap/mcp` grant naming it |
| `kill`, `undeploy` | no undo; a `reason` is required and audited | a `twd.dev/cap/mcp` grant naming it |

Register it with your agent through the CLI's stdio bridge. It works on
the twd host and on any `twd link`ed machine, and needs no credentials:

```sh
twd mcp --config                       # the commands for your agents, and your tools
claude mcp add twd -- twd mcp          # Claude Code; other agents: command "twd", args ["mcp"]
```

An agent on a tailnet machine without the twd CLI can use the URL
`twd mcp --config` prints (`http://twd-mgmt.<tailnet>.ts.net/mcp`).

**Who may do what is your tailnet policy.** A grant to `tag:twd-mgmt` with
the app capability `twd.dev/cap/mcp` admits a user, group or tagged machine
to `/mcp` (and only `/mcp`: it isn't a twd admin) with the read tools.
Destructive tools need the grant to name them, even for admins:

```jsonc
// a coding agent's machine: read-only
{"src": ["tag:agent"], "dst": ["tag:twd-mgmt"], "ip": ["tcp:443", "tcp:80"], "app": {"twd.dev/cap/mcp": [{}]}},
// this user's agents may also kill and undeploy
{"src": ["alice@example.com"], "dst": ["tag:twd-mgmt"], "ip": ["tcp:443", "tcp:80"], "app": {"twd.dev/cap/mcp": [{"tools": ["kill", "undeploy"]}]}},
```

A tool you don't have answers `cap_missing`, with the grant to add as its
`snippet`. On the twd host itself the bridge runs as the operator: it is
read-only unless you enable more when you register it, for example
`claude mcp add twd -- twd mcp --allow kill,undeploy`.

`deploy` over MCP needs a bundle, which only `twd deploy` builds, so in
practice an MCP agent redeploys or re-places what's running; new code
still goes through the CLI.
