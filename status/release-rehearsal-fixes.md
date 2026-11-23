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
