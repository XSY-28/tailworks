Last update: 2026-11-25 PT. tw dev honours wrangler `dev.port` (#473).

## What's built

- `cmd/tw/dev_port.go`: `resolveDevPort` picks tw dev's port: `--port`
  (explicit, fails when busy), else wrangler `dev.port` (explicit, fails
  when busy, the error names the file), else 8787, falling back to a free
  port with one stderr line when 8787 is busy. `dev.port` is read from the
  config file as written, since `dev` is top-level only (any `--env`).
- `wranglerKeyNote("dev")` no longer warns about `dev.port`; other `dev.*`
  keys (`ip`, `local_protocol`, `host`, …) are named as ignored (tw dev
  binds 127.0.0.1 only, on purpose). An invalid `dev.port` is warned about.
- The busy-port error's fix adds `tw dev --port 0`.
- Docs: `docs/dev-debugging.md`, `docs/wrangler.md` table, `docs/cli.md`.

## Tests

See the PR's Tests section.
