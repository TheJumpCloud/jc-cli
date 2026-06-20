# jc-cli-skills

Conversational Claude Code skills for JumpCloud administration via the
[`jc` CLI](https://github.com/TheJumpCloud/jc-cli).

The skills delegate every JumpCloud operation to `jc`, so the LLM never
issues raw API calls — it composes auditable, plan-mode-previewable CLI
invocations instead. Same auth, same audit log, same `--plan` safety
rails as a human operator at the terminal.

## Skills

| Skill | What it does |
|---|---|
| `/jc-cli-skills:jc` | Generic JumpCloud Q&A and ad-hoc resource queries |
| `/jc-cli-skills:jc-onboard-user` | New-user provisioning — create, group memberships, device assignment, welcome email |
| `/jc-cli-skills:jc-offboard-user` | Offboarding runbook — suspend, lock, remove from groups, audit log capture |
| `/jc-cli-skills:jc-troubleshoot-auth` | Auth failure investigation across SSO, RADIUS, LDAP, and policy denials |
| `/jc-cli-skills:jc-security-audit` | Cross-resource security posture audit (admins without MFA, stale devices, FDE coverage, …) |
| `/jc-cli-skills:jc-compliance-check` | Compliance posture against a chosen baseline |
| `/jc-cli-skills:jc-create-recipe` | Author a new YAML recipe interactively, with schema validation |

## Prerequisites

1. **Install the `jc` CLI** — `brew install thejumpcloud/tap/jc` or grab a release binary from
   [the GitHub releases page](https://github.com/TheJumpCloud/jc-cli/releases).
2. **Authenticate** — `jc auth login` (API key or OAuth service account; multi-profile if you manage multiple orgs).
3. **Verify** — `jc doctor` should print a green active profile and a passing API probe.

The skills assume `jc` is on `$PATH` and that an authenticated profile is active.

## Install (Claude Code)

```bash
/plugin marketplace add TheJumpCloud/jc-cli
/plugin install jc-cli-skills@jc-cli
```

That adds this repo as a Claude Code marketplace and installs the
`jc-cli-skills` plugin from it. Skills become available as
`/jc-cli-skills:<name>` slash commands.

## Updating

```bash
/plugin update jc-cli-skills
```

## Safety model

Every skill that performs a mutation walks the operator through
`--plan` previews before execution. Destructive operations
(`delete`, `lock`, `erase`) require explicit confirmation in the
skill prompt before `jc` is invoked without `--plan`. Combined with
the operator's local `jc` audit log, every action the LLM took is
recoverable.

For the deeper auth + audit + MCP threat model, see
[docs/AUTH.md](https://github.com/TheJumpCloud/jc-cli/blob/main/docs/AUTH.md).
