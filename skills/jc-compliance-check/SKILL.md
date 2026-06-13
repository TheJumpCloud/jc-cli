---
name: jc-compliance-check
description: Run a JumpCloud compliance check — MFA adoption rate, admin MFA coverage, FDE coverage, password age, plus password-policy + admin-account inspection, using the jc CLI
---

# JumpCloud Compliance Check

Produce a structured compliance report against the configured
JumpCloud org. The numeric compliance ratios (MFA, FDE, password age,
admin MFA coverage) come from `jc audit --category compliance`; the
config-state checks (password policy, admin count) come from direct
`jc org settings` / `jc admins list` inspection.

## Prerequisites

- `jc` installed and authenticated (`jc auth status`)
- Org-admin role for `jc org settings`

## Step 1 — Run the built-in compliance audit

```bash
jc audit --category compliance --output json
```

Returns structured findings for:

- **mfa-adoption-rate** — % of active users with MFA. Severity scales:
  <50% CRITICAL, <80% HIGH, <95% MEDIUM, ≥95% silent (no finding).
- **admin-mfa-coverage** — % of admins with MFA. CRITICAL if <100%.
- **fde-coverage** — % of managed macOS/Windows devices with full-disk
  encryption active. <50% CRITICAL, <90% HIGH, otherwise MEDIUM.
- **password-age** — count of active users with `password_date` older
  than 90 days. Single MEDIUM finding (drop with `--severity high` if
  your framework has moved off mandatory rotation per NIST SP 800-63B).

Each finding carries a `detail` line you can render directly (it
includes the percentages and counts).

## Step 2 — Inspect password policy

`jc audit` doesn't yet introspect the org-wide password policy; do
this directly.

```bash
# Get the active org ID (or pass via JC_ORG_ID env var)
jc org list --ids

# Inspect the policy
jc org settings $ORG_ID --query "passwordPolicy"
```

Check (PASS/WARN/FAIL per compliance framework you're targeting):

- Minimum length ≥ 12 (PASS), ≥ 8 (WARN), < 8 (FAIL)
- Complexity requirements enabled
- Account lockout threshold configured
- Password history depth ≥ 5

## Step 3 — Admin account inventory

```bash
jc admins list --output json
```

Beyond the MFA coverage `jc audit` already reports:

- **Count** — fewer admins = smaller blast radius. Heuristic: <5 for
  small orgs, scale with org size, no hard rule.
- **Roles** — verify the spread of `roleName` matches your access
  model (e.g. one "Administrator" + several "Manager" / "Read Only").
- **Stale logins** — admins whose `lastLogin` is >90 days old; consider
  rotating or deprovisioning.

## Step 4 — Report

Produce a layered PASS/WARN/FAIL table.

```
JumpCloud Compliance Report — ORG — DATE

| # | Check                | Status | Detail                                  |
|---|----------------------|--------|-----------------------------------------|
| 1 | MFA adoption         | WARN   | 87 of 100 active users (87%) — target 95%+ |
| 2 | Admin MFA coverage   | PASS   | 4 of 4 admins have MFA                  |
| 3 | FDE coverage         | FAIL   | 3 of 10 macOS/Windows devices (30%)     |
| 4 | Password age (>90d)  | WARN   | 12 active users overdue                 |
| 5 | Password policy      | PASS   | Min 14 chars, complexity, lockout       |
| 6 | Admin inventory      | PASS   | 4 admins, 0 stale logins                |

Overall: 3 PASS, 2 WARN, 1 FAIL
```

For each WARN or FAIL: cite the remediation hint from the `jc audit`
finding (or a direct `jc` command for the config-state checks).

## CI gating

For a deterministic compliance gate (e.g. quarterly cron, GitHub Action):

```bash
jc audit --category compliance --exit-code --threshold high
```

Non-zero exit when any compliance finding is at or above HIGH —
suitable for blocking a release pipeline or paging on-call.

## Why this layout

Pre-2026.06 this skill scripted ~6 inline `jc users list` /
`jc devices list` queries with bash-side filtering. `jc audit`
consolidates the ratio math into structured findings with consistent
severity language, so the skill now interprets rather than re-derives.
The config-state checks (password policy, admin inventory) remain
inline because they're org-settings introspection, not cross-resource
aggregation.
