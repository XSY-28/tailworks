Last update: 2026-11-24 PT. MCP logs/audit filter parity with the CLI (#472); MCP kill and admin UI kill cascade by default (#451 follow-up).

# mcp-logs-parity

## What changed

- `mgmt/mcp.go`: the `logs` tool gains `outcome` and `caller` (schema and
  forwarded query keys), so it has every `obs.Filter` key `tw logs` has;
  `audit` gains `human` (forwarded to `GET /v1/audit?human=`).
- `mgmt/mcp_parity_test.go` (new): `TestMCPLogsFilterParity` fills every
  `obs.Filter` field by reflection, checks each `Filter.Values()` key is a
  `logs` tool property, runs the tool against a fake `/v1` and checks the
  `/logs` query parses back (`obs.ParseFilter`) to the same filter. Dropping
  a key from the forward list fails it (checked). `TestMCPAuditKill` covers
  `human` and the kill cascade default.
- `kill` tool: cascades unless `cascade: false` (#451 called aligning it with
  `tw kill` a follow-up). Safe for agent callers: the reaper kills a killed
  parent's live sub-agents within `ReapInterval` anyway, so the blast
  radius is unchanged; the cascade is attributed to the caller and reason
  instead of `tw-reaper`. The tool still requires `reason` and a grant naming
  `kill`.
- Admin UI object page: the cascade checkbox is checked by default; unticking
  it still sends no `cascade` (kill only the object).
- MCP `ls` state filter (#452's sibling bug named in #472) was already fixed
  on master (`mcpLs` passes `LiveStatuses` unless `all`); nothing to do.
- Docs: `docs/agents.md` tool table, `docs/ADMIN-UI.md`. Fragment
  `changes/mcp-logs-parity.md` (`config-default`).

## Tests

See the PR's Tests section for the final run.
