Last update: 2026-11-24 PT. Subcommand help listing (#438) and docs/cli.md "Same flags as" links (#437).

# cli-help-subcommands

Issues #438 and #437 from the interface-simplification audit (#434).

## What's built

- `cmd/tw/subcommands.go` (new): splits a command's usage line into its
  alternatives (reusing completion.go's `compAltSep`; a segment that doesn't
  start with the command name, like d1 execute's `| --file <file>`, stays
  with the one before), the trailing three-space "common flags" part, and the
  subcommand words (`migrations create|list|apply`). `tw <cmd> --help` and
  `tw help --all` print one alternative per line under `subcommands:`;
  commands whose subcommands own their flag sets add
  `tw <cmd> <subcommand> --help   its flags`. `needsSubcommand(name)`
  replaces the hand-written "needs a subcommand: …" errors (agent, d1, docs,
  image, queues, r2, secret, tenant, and the bare host, release, custody and
  kv errors).
- Usage lines: `tenant set` is `tenant set <tenant> [--<setting> <value>]…
  (tw tenant set --help; docs/limits.md)`; host, image public add and
  upgrade use `[flags]`, keeping required flags (`tenant add … --oauth-client-file`,
  `host setup --microvm [flags] | --undo`). kv/d1/queues/secret/r2 grammar
  unchanged (B3).
- `initTemplates`: one list for `tw init --template`'s flag help and usage,
  the no-config fix hint, `tw help` and `tw help --all`;
  `TestInitTemplates` checks it against js/packages/create-tailworks/templates.
- `cliReference` (clidoc.go): a subcommand whose flag block equals its
  command's or an earlier subsection's keeps its `####` heading and gets
  `Same flags as [tw X](#tw-x).` (52 sections). docs/cli.md 2327 → 1655 lines.
  `TestSearchCLIOnce` (docs/cli_search_test.go): `delay` has at most one
  cli.md hit in the top 10.

## Checks

- The completion tree (every path, its subcommands and flags, and the
  value-flag set) dumped before and after the change with a throwaway test:
  identical, so `tw tenant set` and the host subcommands still complete
  every flag.
- `make cli-docs`, `make lint`: ok.
- `make test`: see below.

## Decisions

- `subcommands:` is printed for every command with subcommand words,
  including single-alternative ones (expose, completion, custody), so the
  two styles converge as the issue asks.
- "Earlier" for the cli.md dedupe means within the same command, in the
  completion tree's (sorted) path order; no links across commands.
