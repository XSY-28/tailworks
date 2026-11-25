Last update: 2026-11-25 08:50 PT. Share deployment readiness polling across deploy, promote and rollback.

This implements the readiness portion of #525. The shared loop reports
`deploy_crashloop` for all three commands and keeps the deployment event stream,
the active timing used by deploy and the existing replica wait.

- `go test ./cmd/tw -run '^TestPlacementCommandsReportCrashloop$' -count=1 -v`
  reproduced `identity_pending` for both commands on the original code.
- `go test ./cmd/tw -run 'TestPlacementCommandsReportCrashloop|TestCLIReferenceUpToDate|TestCompletion|TestDocsNameRealCommands' -count=1 -v`
  passed with the fix. A separate built-binary smoke against a local fake
  management endpoint checked exit status, empty stdout, the status event,
  JSON error code, both detail lines and the log hint for both commands.
- `make js-build` and `go test ./cmd/tw -run '^(TestCLIJSONContract|TestAgentEval)$' -count=1 -v`
  passed. On macOS, `TW_WORKERD` pointed to the installed native workerd binary
  for the integration run.
- `go vet ./cmd/tw`, the Go format check, `git diff --check` and `make changes-check` passed.
- The first `make lint test` passed lint and the complete `cmd/tw` race suite.
  Two tests failed in the concurrent Go suite: `TestTenantAdminScope` in
  `mgmt` hit the host heartbeat deadline, and `TestContainerPolyfill/MonitorAndRestart`
  in `hostd` observed a stopped container during monitor setup. Both passed
  twice when rerun individually with `go test -race`.
- `GOFLAGS=-p=1 make lint test` passed with Go 1.27.4 and the same native
  workerd. The full Go race suite and all 71 JavaScript tests passed.
