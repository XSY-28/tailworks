---
kind: breaking
upgrade: [protocol, cli-json, config-default]
---
twd now mints node keys on Tailscale with an OAuth client only. The
`apikey` and `wif` (workload identity federation) mint modes are removed
(ADR-042). The Headscale profile is unchanged: it still
mints with a Headscale API key.

Removed: `twd up --wif`, `--wif-client-id`, `--wif-lifecycle-client-id`,
`--issuer-authkey-file`, `--login-timeout`, and `--api-key-file` outside
`--control=headscale`; `twd tenant set --wif-client-id` and
`--wif-lifecycle-client-id`; `twd doctor --tailnet/--api-key-file`; mgmt's
OIDC issuer (`twd-mgmt -issuer-url`, `-issuer-listen`, `-funnel`,
`-issuer-tsnet-*`, and the discovery and JWKS endpoints);
`POST /v1/tenants/{id}/wif-check`; the tenant fields `wifClientID`,
`wifLifecycleClientID` and `wif`; `twd-netd -mint-api-key-file` (now
`-mint-oauth-client-file`). New: `twd tenant add --oauth-client-file`,
`twd tenant set --oauth-client-file`, and the error code
`mint_mode_removed`.

Operator action: if `twd doctor` fails its `mint_mode` row, or `twd up`
says `mint_mode_removed`, the install mints with an API key or WIF. Create
an OAuth client (docs/INSTALL.md, "The OAuth client"), then run
`twd up --oauth-client-file <file>`. This keeps the tenant, its deployments
and mgmt's node. Do the same for each added tenant with
`twd tenant set <tenant> --oauth-client-file <file>`. Then revoke the old
API key, or delete the WIF trust credentials, the `twd-issuer` machine and
the `tag:twd-issuer` policy entries. Running nodes keep serving until then,
but nothing new can be minted.
