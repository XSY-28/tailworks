---
kind: feature
upgrade: [cli-json]
---
Coding agents can drive twd: `twd agent setup` installs skills, `--json` output is a stable, documented contract (docs/cli-json.md, `schema/cli.schema.json`), errors carry `hint` and `doc_url`, `twd docs` searches the docs offline, and twd-mgmt serves an MCP server at `/mcp` (ADR-035).
