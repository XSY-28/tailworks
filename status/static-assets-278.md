Last update: 2026-11-23 PT. Static assets (wrangler `assets`, issue #278) built; `static-site` template added.

# Static assets (`assets`), issue #278

Design: `docs/design/adr/static-assets.md` (`ADR-static-assets`, Proposed;
number it when merging, AGENTS.md rule 5. `docs:TestADRIndex` fails in CI
until it is numbered).

## What's built

- **CLI** (`cmd/tw/assets.go`): `tw deploy` / `tw dev` read
  `assets.directory` and add its files to the bundle as `assets/<path>`.
  `.assetsignore` (gitignore subset), `.git`, `node_modules`, `.DS_Store`
  are skipped; `_headers` / `_redirects` are left out with a load-time
  warning. Settings are validated before the build
  (`bundle.CheckAssets`). The packed bundle is checked against the deploy
  request's 64 MiB JSON body (`checkBundleSize`). The `assets: not
  supported` warning is gone (`site` still warns). `tw types` types the
  binding as `Fetcher`. `tw dev`'s change detector also walks
  `assets.directory`, even when it is `dist/`.
- **Bundle** (`hostd/bundle/assets.go`): with wrangler `assets`, files under
  `assets/` are assets (by URL path), never modules; without it `assets/` is
  an ordinary directory as before. `App.Assets` holds binding,
  `html_handling`, `not_found_handling`, `run_worker_first` (bool or
  patterns) and the files.
- **Gen** (`hostd/gen/assets.go`, `js/assets.js`, `js/assets-front.js`):
  files and an ETag manifest in the instance dir under `assets/`; a
  read-only `disk` service; a router Worker (`assets` service, no network:
  `globalOutbound = "deny"`); the tenant main module's default export is
  wrapped by `withAssets` (object and `WorkerEntrypoint` class forms), which
  asks the router first for workload GET/HEAD requests only. microVM: the
  disk path is relative (`assets`), since guestd runs workerd in its app
  dir, which receives the whole instance dir.
- **Placement**: new host feature `assets` (`internal/features`); mgmt
  won't place an assets bundle on an older host.
- **Template** `static-site`: `public/` (no build step) + a Worker for
  `/api/whoami` and a people-only `/team/` (`run_worker_first` +
  `getCaller` + `env.ASSETS.fetch`), `404-page`. Has `test/app.test.js`
  (run by `cmd/tw:TestTemplateTests/static-site`). Registered in the
  `tw init` lists, help, `scripts/ci-tarball-check.sh`, READMEs, and the
  tw-scaffold skill.
- **Docs**: `docs/wrangler.md#assets`, COMPAT.md (`assets` row ⚠️ with
  tests; wrangler-config rows), README (compat row; "Try it: a static site
  with a Worker"), `docs/limits.md`, `docs/cli.md` (`make cli-docs`),
  `changes/static-assets-278.md`.

## Tests

See the bottom of this file for the commands run and their results.

## Gaps / next steps

- `_headers`, `_redirects`; assets-only projects (no `main`); `env.ASSETS`
  in facet-mode objects; a separate upload path for sites over ~48 MiB.
- `create-tailworks`'s `lib/build.mjs` (used by `hostdtest.Build`) doesn't
  read `assets`; `tw` does, for every bundler path.
- microVM tier not run here (needs Linux KVM): covered by the generated
  config test (`TestGenerateAssetsFiles`, relative guest disk path) only.
