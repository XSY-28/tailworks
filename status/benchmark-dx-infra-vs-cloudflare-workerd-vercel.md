Last update: 2026-11-15 PT. DX benchmark against Cloudflare (wrangler/workerd), Vercel, Fly, Deno Deploy and self-hosted PaaS; quick fixes and a docs pass landed, larger gaps filed as #41–#48.

# Developer-experience benchmark (branch `benchmark-dx-infra-vs-cloudflare-workerd-vercel`)

Started from master at 7ca356d. The twd measurements are hands-on, on macOS arm64. The competitor baselines come from current docs: wrangler 4.162, vercel 65.x, flyctl, Kamal 2.12, Dokku 0.38, Coolify 4.7. Note that Deno Deploy announced a sunset in April 2027.

## Measured: the local loop, twd dev vs wrangler dev

Same hello Worker, same machine, `curl` polled every 10–50 ms.

| | twd dev | wrangler dev 4.60 |
|---|---|---|
| `init` | 0.015 s | (C3 is interactive, with npm install) |
| cold start to first 200 | 0.6 s | 0.7 s |
| edit → served | 0.03–0.17 s (was ~0.3 s: 500 ms poll) | ~0.06 s |
| build error | compiler output, last good version keeps serving | same |
| console.log / request line | yes (before this branch: **nothing**, even with `-v`) | yes |
| exception stack | `dist/index.js` and runtime frames (#42) | source-mapped `src/index.ts` |
| `req.url` | the dev node's IP (#42) | `localhost:<port>` |
| port taken | typed error and `fix: twd dev --port 8788` | wrangler picks a port |

twd's loop is as fast as wrangler's. It was missing console output and readable stacks.

## Fixed on this branch

- **`twd dev` prints requests.** Each request prints with its `console.*` lines and the stack of any uncaught exception (`cmd/twd/devrecords.go`, following hostd's request-record file). `--json` emits a `request` event (`schema/cli.schema.json` `dev.event`, `docs/cli-json.md`, `twd-dev` skill). Before this, a 500 printed nothing, and the records sat in a temp dir that's deleted on exit. Test: `TestFollowDevRecords`.
- **`twd dev` polls every 150 ms instead of 500 ms.** The rebuild itself takes ~25 ms.
- **CLI: "did you mean".** A typo (`twd deplo`) or another CLI's verb (`twd tail` → `logs`, `list` → `ls`, `delete` → `undeploy`) now gets a suggestion (`cmd/twd/didyoumean.go`, `schema.Closest`). Test: `TestUnknownCommandSuggests`.
- **CLI: flag names in `--help`.** A backquoted word in a flag's usage was replacing the flag's type: `-env wrangler --env`, `-tenant twd up`, `-ephemeral twd down`. Fixed for every command. Test: `TestHelpFlagNames`.
- **`twd ls` shows why a deployment failed.** Before, it printed only `failed`; the reason was in `--json` alone. Test: `TestDeployErrorLine`.
- **`twd init`.** `package.json` is named after the app (it stayed `twd-hello`). Templates get `npm run dev`, and the `build` script that called the uninstalled `create-twd` is gone (node-workflow keeps `build-node`). Test: `TestInitNamesPackage`.

## Docs pass

- The quick start and INSTALL showed a wrong workload URL
  (`support-<host>…`). It is `twd-support-<id>.<tailnet>.ts.net`, which
  matches a live install.
- New INSTALL sections: Upgrade, Back up and restore, Uninstall. The
  Uninstall section notes that `down --purge` leaves twd's own nodes in place.
  README links to all three.
- `docs/wrangler.md`: "What's different from Cloudflare, in short".
- Fixed: INSTALL said microVM isolation needs Linux (macOS has the VZ tier
  too); the `node_required` fix said Node 22+ while the docs and `twd init`
  say 24; INSTALL's step numbering.
- Link check (relative links and anchors in README, llms.txt, docs/*.md):
  none broken. CLI drift: every command and flag the docs name exists.

## Issues filed

- #41 Publish the npm packages, give templates `tsconfig` and types, add `twd types`. `npm install` in any scaffold fails with a 404, and editors have no types.
- #42 Dev parity with wrangler: source maps, `req.url`, `--inspector-port`, hotkeys.
- #43 hostd: one workerd crash left a live deployment permanently `failed`. Restarts hit EADDRINUSE on `ingress.sock`, and hostd gave up after 6 tries in about 4 s. Seen on a real install.
- #44 `twd dev` and the test harnesses orphan workerd and leak `/tmp/twd-dev-*` on SIGKILL (agent timeouts).
- #45 Deploy lifecycle: deployment history, `twd rollback`, preview deploys.
- #46 CLI ergonomics: completion, `twd whoami`, grouped help, update check, the doctor hint in a source checkout.
- #47 Testing: vitest-plugin compatibility, `getCaller` test helpers, a dev harness.
- #48 Docs: caller grants missing from the quick start, first-deploy troubleshooting, a glossary, splitting INSTALL.md.

## Where twd is ahead

- `--json` with a schema on every command, typed errors that carry a fix and a doc URL, `twd docs search` offline, skills for coding agents, and an MCP server. On agent tooling only Vercel's CLI is comparable.
- `twd doctor` runs in 0.17 s and checks things the others don't (sandbox enforcement, tsnet versus the system tailscaled).
- Identity, policy and kill per object have no equivalent in the hosted platforms.

## Test results

- `make lint`: ok.
- `go test ./cmd/twd ./schema ./internal/obs -count=1`: ok. This worktree has no `node_modules`, so tests that need `make deps` skip.
- Measured by hand with `twd dev` against the installed create-twd (`TWD_CREATE_TWD=~/.twd/share/twd/create-twd`).
