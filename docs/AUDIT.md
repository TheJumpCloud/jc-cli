# `jc audit` — check reference

`jc audit` runs a battery of cross-resource health checks against the
configured JumpCloud org. Each check is severity-tagged (`info`, `low`,
`medium`, `high`, `critical`) and ships with a `remediation_hint` that
names the exact `jc` command to fix it. Output formats:

- Default human (grouped by category, severity-glyph prefix)
- `--output json` — wrapper object with `results`, `findings`, `warnings`
- `--output ndjson` — one finding per line, for streaming pipelines
- `--output table` / `--output csv` — tabular, fields:
  `severity, category, check_id, resource_ref, title`

For CI gating, pair `--exit-code` with `--threshold high` (or any other
severity). Non-zero exit when any finding meets or exceeds the threshold.

```bash
jc audit                               # everything, human-readable
jc audit --category security           # security checks only
jc audit --severity high               # high/critical findings only
jc audit --output json                 # machine-readable for skills
jc audit --exit-code --threshold high  # CI gate: fail on high+ findings
```

The check registry lives in `internal/audit/checks.go`. Adding a new
check is one `Register` call from `init()` — the CLI surface, JSON shape,
sidebar showcase entry, and skill prompts all surface it automatically.

## Security

### `admins-without-mfa` — CRITICAL

Admin accounts without `enableMultiFactor` AND `totpEnrolled` both true.
Admin accounts are the highest-value target in any directory; missing
MFA on even one admin is a single-step compromise path to org-wide
takeover.

**Remediation:** Admin Portal → Administrators → edit → Enable
Multi-Factor Authentication. No automatable API for this — JumpCloud
requires the admin to enroll via the portal.

### `users-without-mfa` — HIGH

Active (not suspended, not locked) users without an enrolled MFA factor
(`totp_enabled=false` AND `mfa.configured=false`). Scopes to active
users only: a suspended/locked account isn't a live attack surface.

**Remediation:** Enforce MFA via an auth policy on the user's groups
(`jc auth-policies create`), or have the user enroll via the JumpCloud
user portal.

### `suspended-not-locked` — MEDIUM

Users where `suspended=true` but `account_locked=false`. Suspension
prevents new login but doesn't necessarily invalidate active sessions
or refresh tokens; locking forces re-auth on every gate.

**Remediation:** `jc users lock <username>`.

### `iplists-empty` — LOW

IP lists with zero IP entries. An empty IP list referenced by an auth
policy fails open or closed depending on policy semantics — both are
footguns: in fail-open you've eliminated the gate; in fail-closed
you've locked the user base out of the gated resource.

**Remediation:** Populate (`jc iplists update`) or delete
(`jc iplists delete`).

## Compliance

### `mfa-adoption-rate` — scales by adoption

Org-wide MFA adoption among active users. Severity scales:

| Adoption | Severity |
|----------|----------|
| <50% | CRITICAL |
| <80% | HIGH |
| <95% | MEDIUM |
| ≥95% | silent (no finding) |

The 95% threshold matches the bar in SOC 2 and ISO 27001 audit
frameworks for "material control gap."

**Remediation:** Enforce MFA via auth policies covering user groups.

### `admin-mfa-coverage` — CRITICAL

Admin MFA adoption with a hard 100% target. Reported as a single
finding when adoption is below 100%; pair with `admins-without-mfa` for
the per-admin list.

### `password-age` — MEDIUM

Active users with `password_date` older than 90 days. 90 days mirrors
the common compliance bar (HIPAA, PCI DSS).

If your compliance framework has moved off mandatory rotation (NIST SP
800-63B recommends against forced rotation absent compromise), filter
this check out with `--severity high`.

### `fde-coverage` — scales by coverage

Full-disk encryption coverage across managed macOS and Windows devices.
Linux / iOS / etc. are excluded (no comparable FDE telemetry via the
JumpCloud API).

| Coverage | Severity |
|----------|----------|
| <50% | CRITICAL |
| <90% | HIGH |
| <100% | MEDIUM |
| 100% | silent |

**Remediation:** Push the JumpCloud FDE policy to unencrypted devices.
FileVault / BitLocker keys are escrowed to JumpCloud for recovery.

## Hygiene

### `stale-devices` — MEDIUM

Devices with `lastContact` more than 30 days ago. Either the device is
decommissioned (delete to reclaim the license) or the agent has
crashed (investigate). Stale devices count toward your license while
contributing no telemetry.

**Remediation:** `jc devices delete <id>` (if decommissioned) or
`jc devices get <id>` to investigate agent state.

### `auth-policies-disabled` — LOW

Authentication policies in the disabled state. Disabled policies are
dead code — silent until a future operator wonders why traffic isn't
being gated.

**Remediation:** Re-enable (`jc auth-policies enable <name>`) or delete
(`jc auth-policies delete <name>`).

## Identity

### `recently-created-admins` — INFO

Admin accounts created in the last 14 days. Newly-created admins are
both a legitimate onboarding signal AND a common post-compromise
persistence mechanism — surface them for cross-check against IAM
tickets, Slack approvals, or a CMDB.

This is INFO severity, not LOW: a legitimate new admin shouldn't
contribute to a hygiene score, but every new admin should be **seen**.

## Roadmap

Checks deferred for v1 because they require per-resource follow-up
calls (N+1 patterns that hurt at scale):

- `empty-user-groups` / `empty-system-groups` — needs `/usergroups/{id}/members`
- `suspended-users-with-ssh-keys` — needs `/systemusers/{id}/sshkeys`
- `policies-without-scope` — needs the graph traversal

Filed under cleanup follow-ups once we have a batched member-count
endpoint or sustained-load benchmarks justifying the fan-out cost.
