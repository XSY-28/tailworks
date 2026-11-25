Last update: 2026-11-25 PT. Issue #481: prompt to write the policy by default on a terminal (stacked on policy-fix-texts, PR #581).

# policy-prompt-default

## What changed

- `cmd/tw/policy_ask.go` (new): `policyModeOf` (print-only / ask-first /
  `--apply-policy`), `bootstrapPolicyGate` (validate the merged policy,
  then ask; "n" = print-only), `policyAskHint`.
- `tw up` Tailscale (`upApplyPolicy`, mode `policyAskFirst`): asks only
  when the policy is readable and the control plane validates the merge;
  "n" or a refusal runs print-only tw up's check (`oauthNeededCheck`,
  split out of `oauthPreflight`).
- `tw up` Headscale, the Headscale profile check, `tw tenant add`:
  `bootstrapPolicyGate`. Headscale keeps its hard stop on "n".
- `tw deploy` (`deployAskPolicy`, one call in app.go): plan; ask only when
  mgmt's plan says `canWrite`; "n", a refused plan (tenant admin), a
  read-only client or a failed write falls back to `acl_denied`'s snippet.
  Skipped under `--allow-missing-grants`. `tw deploy --preview` takes the
  same path; `tw promote` keeps the snippet. `tw policy` stays print-only
  (`tw policy --apply` is the explicit write).
- mgmt: dryRun plan sets `canWrite` (`mgmt/policyapply_canwrite.go`;
  `api/openapi.yaml`, `make api`).
- Docs: README steps 4-5, INSTALL.md, headscale.md, errors.md
  (`acl_denied`, `cancelled`, `confirmation_required`), agents.md, skills
  (tw, tw-deploy, tw-policy), THREAT-MODEL.md, RED-TEAM.md; `make cli-docs`.

## Tests

See the PR's Tests section.
