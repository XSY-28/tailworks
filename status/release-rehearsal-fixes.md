Last update: 2026-11-23 13:30 PT. Fixes from the first release.yml rehearsal.

# Release rehearsal fixes

The first rehearsal (branch release-all-runtimes, run 45260846780) passed
every hermetic suite, gates, headscale, test-pg and the guest builds. It
failed on:

- `test-images`: TestCLIImages skipped ("needs workerd and node"), since the
  job didn't run `make deps`. Fixed: the job runs it.
- `microvm-amd64`, `mac-vz`: e2e-vm/e2e-vz passed but scripts/ci-suite.sh
  failed them for skips that are expected there: TestBootBench (opt-in
  benchmark), TestContainerSmoke / TestVZContainer* (run in the container
  step that follows, with images), TestE2EVMSnapshot on vz (Firecracker
  only). Fixed: per-step TW_CI_ALLOW_SKIP, with the reason beside it.
- `mac-firecracker`: the Lima dev VM had go1.27.3, go.mod wants 1.27.4
  (GOTOOLCHAIN=local). Fixed: scripts/ci-lima-make.sh installs go.mod's Go
  in the VM from go.dev, SHA-256 checked. Tried on the runner's VM: ok.
- `build`, `tailnet`: expected on a branch (the release environment deploys
  only from master and v* tags).

Seen, not failing: on vz the "no keys in guest memory" scan couldn't save
VM state ("permission denied"), so guest memory wasn't scanned under the
runner; files and disks were.

## Rehearsal 2 → real-tailnet suites (2026-11-23)

Run locally on the release runner's machine (same credentials) before the
next rehearsal, one suite at a time:

- e2e-oauth: pass after #265 (tag:tw-wl-mcp2 owned).
- e2e-mm-svc, e2e-coldid-real: pass.
- e2e-svc (TestE2EService, TestE2EReplicas): two stale assumptions, both
  from changes the suite hadn't been run against: `tw up` is single-process
  by default (the suite now passes `--separate-host`, as cliflow does), and
  minting is OAuth-only, so the run's OAuth client needs the `services`
  scope for a workload with a Service (cliflow.RealOAuthClientScopes). Now
  pass.
- e2e-svc-caps: tailnet 2's policy had lost the H22 entries. The test now
  adds them itself (internal/tssvc/real_caps_policy_test.go: additive,
  validated, If-Match, previous policy saved next to the key file), with
  the user's OK. Now pass.
- e2e-public: the first run got its first public answer in 48 s, then a
  route fetch failed on a transport error (IPv6 no route; next run, DNS no
  such host). fetch now retries dial/DNS errors 3× (any response is
  returned as is). A third run never got a Funnel certificate in 12 min
  (cert prefetch timed out twice; CI saw an ACME DNS-01 NXDOMAIN for a mgmt
  name): certificate issuance, not tailworks. Open: every run mints certs
  for fresh *.tail7906d5.ts.net names, and Let's Encrypt limits certs per
  registered domain per week.
