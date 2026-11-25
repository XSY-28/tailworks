# Name npm scaffolds consistently

Addresses #540. The local `create-tailworks` fallback applies the same names and template tags as `tw init`. It uses the destination directory when the template flag is omitted. Both paths replace each original template tag once, including when a new tag coincides with a different old tag.

## Tests

- The new Go parity test runs the Node bin with an empty `PATH` and compares the three generated config files against `tw init` for all nine shipped templates. It failed on the original fallback and passes with the change.
- The deterministic two-tag fixture failed before single-pass replacement. Twenty runs of each actual CLI with the shipped `ai-agent` template and directory `wl` now produced identical files and kept the workload and class tags distinct.
- Unpacked the npm tarball and ran its bin without `tw` or installed Node dependencies. Verified the workload, package, `APP_NAME`, generated tags, and `.gitignore`. An unusable destination label failed before any files were copied.
- `GOFLAGS=-p=1 GOMAXPROCS=8 make lint test` passed. An earlier run recorded a timing failure in `TestContainerFenceInheritance`, which passed in the final run.
- `make templates-typecheck test-sdk` passed. All eight TypeScript templates installed and passed `tsc --noEmit` and `tw types --check`. The script skips the Python Worker for TypeScript checks. The hermetic SDK integration suite passed.
- `make npm-cli-test`, the selected documentation check, and `git diff --check` passed.
