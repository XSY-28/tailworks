Last update: 2026-11-24. Internet egress to TCP port 25 is refused by default (`internet_port_refused`); `tailnet.egress.internet` entries may name a port (#339, ledger H-egress-abuse, C-egress-port25).

# fix-339-egress-port25: a default port policy on the internet path

## What

Issue #339 (the review against Cloudflare's threat models) asked what a
hostile workload can do with the host's address once ADR-052 let
`connect()` reach any public port. The coordinator decided option 1 (a
default port policy) and deferred the rate cap (2) and a separate TCP
opt-in (3); option 4's text (say what a hostile workload can do and what
the control is) is in docs/security-model.md.

- **Policy** (`netd/publicip/ports.go`, `netd/internetports.go`): TCP 25 to
  a public address is refused on netd's internet path, in `internetRoute`,
  so every path that names a port gets it: `fetch()` to `http://host:25/`,
  `connect()`, `node:net`, the Node tier's CONNECT (`DialEgress`). Before
  DNS and before any dial. Typed error `internet_port_refused` (403);
  hostd's `serveEgress` and `serveConnect` already audit every refusal as
  `deny` with its code, so the refusal is audited without a new call.
  Ports 465 and 587 are untouched.
- **Containers** (`netd/sbxnet`): `Accept` applies the same rule and the
  same port-qualified allowlist, so the two paths share one policy as they
  share the SSRF guard. Two new `case` lines.
- **Override**: `tailnet.egress.internet` entries may carry `:port`, in the
  syntax ADR-052 §6 gave `tailnet.connects` (`"smtp.example.com:25"`,
  `"*:25"`, `"203.0.113.0/24:25"`, `"[2001:db8::1]:25"`;
  `publicip.SplitEntryPort`). An entry with a port covers that port only
  (`Allowlist.AllowsPort`); an entry without one covers every port the
  policy allows; only an entry that names 25 lifts the refusal
  (`Allowlist.NamesPort`). `"*"` and `true` do not open port 25. No
  operator flag was needed.
- **Deploy and mixed versions**: a bad port in an entry fails the parse
  (`bundle.InternetEgress.UnmarshalJSON`, `publicip.EntryPortOK`). An entry
  that names a port requires host feature `internet-ports`
  (`features.InternetPorts`), because an older hostd reads it as a name
  that matches nothing.
- **Docs**: docs/errors.md (`internet_port_refused`, When and Fix),
  docs/limits.md "Limits that don't exist" (new row: new internet flows per
  unit of time, none, and why), docs/security-model.md Egress (what a
  workload can do with the host's address; what is bounded and what isn't;
  links limits.md), docs/tailworks-jsonc.md, docs/containers.md,
  docs/upgrading.md (feature list), schema/tailworks.schema.json,
  security/THREAT-MODEL.md B11, ADR-052 "Follow-up (2026-11-24): port
  policy" (decided, deferred, why), security/LEDGER.md (H-egress-abuse
  fixed/disclosed, C-egress-port25 pinned, run log line),
  changes/fix-339-egress-port25.md (`security`, `feature-string`).

## Tests

Run from this worktree on 2026-11-24.

| Command | Result |
|---|---|
| `go test -count=1 ./netd/publicip ./netd/sbxnet ./hostd/bundle ./internal/features` | ok (new: `publicip.TestSplitEntryPort`, `TestEntryPortOK`, `TestAllowlistPorts`, `TestRefusedPort`; `sbxnet.TestInternetPortPolicy`; `bundle.TestInternetEgressPorts`; `features.TestRequiredInternetPorts`) |
| `go test -count=1 -v -run TestInternetEgress ./netd` | ok, 13 subtests incl. new `Port25Refused`, `SubmissionPortsProceed`, `Port25Override` (real workerd and netd) |
| **Before/after:** same run with `origin/master`'s `netd/internet.go` in place | `Port25Refused` FAILS (`connect(smtp.example.com:25): <nil>, want internet_port_refused`), `Port25Override` FAILS (`connect(smtp.example.com:587) with only :25 listed: <nil>`); both pass with the fix |
| `go test -count=1 -v -run TestWorkerInternetEgress ./hostd` | ok, 5 subtests incl. new `Port25` (a Worker's `fetch()` to `api.example.com:25` end to end: 403 `internet_port_refused`, no dial, a `deny` record with the code, 200 with `["api.example.com:25"]`) |
| `go test -count=1 ./docs/... .` | ok (limits citations, error docs) |
| `make lint` | ok (gofmt, vet, staticcheck, vulncheck, api-check, compat-check, changes-check, check-secrets) |
| `sh ~/.claude/scratch/mbp-test.sh test` (`make test` on mbp) | see below |

Skipped: the suites that need credentials, a real tailnet, Docker or KVM
(`e2e-real`, `e2e-compat`, `e2e-vm`, …). Nothing in the table of extra
suites applies: no store, placement, queues, telemetry, templates, wrangler,
demo, install or npm changes. `make cli-docs` not needed (no flag or help
text changed).

## Decisions and notes

- Entry semantics: "an entry that names a port covers that port only"
  matches ADR-052 §6 for `tailnet.connects` (per port when the entry names
  one). The alternative (a `:25` entry only lifts the port policy and the
  host is otherwise allowed on every port) would make `["smtp.example.com:25"]`
  open the relay's port 443 too, which nobody asked for.
- The policy is applied to containers' `enableInternet` too (not only
  Workers): a Sandbox-SDK container running model-written code is the
  prompt-injection case the issue describes, and `publicip` exists so the
  two paths can't disagree. Containers already needed an allowlist entry
  for any internet host; with the policy, `true` or `"*"` no longer covers
  port 25 for them either. That is the one behaviour change for containers.
- Not done (deferred by the coordinator, recorded in ADR-052 and
  docs/limits.md): a per-workload rate cap on new flows; a separate opt-in
  for raw TCP.
