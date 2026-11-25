Last update: 2026-11-25 12:54 PT. Keep Node and Go scaffold names and generated tags consistent after #620.

The Node initializer lowercases U+0130 like Go before converting the directory
name into a workload label. The Go initializer substitutes template tags in a
single pass. With the `ai-agent` template and a directory named `wl`, one
generated tag can be another tag's original value.

## Verification

- The expanded Go scaffold parity test compares complete Node and Go output
  trees for all nine templates and two directory names. Before the fix, all nine
  U+0130 cases failed. All 18 cases passed after the fix, including `hello`
  with no Node template flag.
- The two-tag Go regression failed before the fix and passed after it.
- Forty `tw init wl --template ai-agent` runs kept the workload and class tags
  distinct and matched the Node initializer with `tw` absent. Normal directory
  names and the `ai-agent` directory passed both initializers.
- The Node initializer's four existing tests, `make changes-check`, and full
  `GOFLAGS=-p=1 GOMAXPROCS=8 make lint test` passed.
- `make test-sdk` passed.
- `make templates-typecheck` passed for seven templates and skipped the Python
  template as designed. Installation of the Node workflow template stopped at
  the public registry's unavailable `@vercel/oidc@5.0.0` dependency. The
  unmodified `9d71291` base failed at the same step.
