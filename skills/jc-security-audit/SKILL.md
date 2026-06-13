---
name: jc-security-audit
description: Run a JumpCloud security audit — admins without MFA, users without MFA, suspended-not-locked accounts, IP list hygiene, plus auth-failure trends from Directory Insights, using the jc CLI
---

# JumpCloud Security Audit

Run a focused security audit across a JumpCloud organization. The bulk
of the checks ride on `jc audit` (the built-in cross-resource audit
registry); we layer Directory Insights queries on top for the runtime
attack signal (failed authentications) that `jc audit` doesn't model.

## Prerequisites

- `jc` installed and authenticated (`jc auth status`)
- Insights access requires Directory Insights enabled on the org

## Step 1 — Run the built-in security audit

```bash
jc audit --category security --category identity --output json
```

This returns structured findings: each one has `check_id`, `severity`
(info/low/medium/high/critical), `resource_ref` (e.g. `admin:foo@bar`),
`remediation_hint`, and `detail`. Group by `severity` and surface the
critical ones first.

Categories included:

- **security** — admins without MFA, active users without MFA,
  suspended-but-not-locked users, IP lists with no entries
- **identity** — admins created in the last 14 days (sanity check for
  post-compromise persistence)

## Step 2 — Layer in auth-failure signal (Directory Insights)

`jc audit` is a control-state audit; it doesn't see live traffic.
Combine with Insights to spot active attacks:

```bash
# Failed SSO authentications in the last 24 hours
jc insights query --service sso --last 24h --event-type sso_auth_failed -o json

# Volume comparison: total auth events vs failures
jc insights count --service all --last 24h
jc insights count --service sso --last 24h --event-type sso_auth_failed
```

Flag:
- Repeated failures from the same `initiated_by.email` → possible brute force
- Failures from unfamiliar `client_ip` ranges → possible credential theft
- Failure clusters at unusual times (off-hours, weekends)

## Step 3 — Report

Produce a layered report:

1. **Findings from `jc audit`** — table with `severity | check_id |
   resource_ref | detail | remediation_hint`. Order CRITICAL → HIGH →
   MEDIUM → LOW → INFO.
2. **Auth-failure signal** — counts, top offending IPs, top targeted
   users from the Insights data.
3. **Recommended actions** — for each finding, the exact `jc` command
   from `remediation_hint`.

### Example report shape

```
JumpCloud Security Audit — ORG — DATE

Control-state findings (jc audit):
| Severity | Check                  | Resource                 | Action |
|----------|------------------------|--------------------------|--------|
| CRITICAL | admins-without-mfa     | admin:alice@acme.com     | Enable MFA in Admin Portal |
| HIGH     | users-without-mfa      | user:bob                 | jc auth-policies create … |
| MEDIUM   | suspended-not-locked   | user:carol               | jc users lock carol |
| LOW      | iplists-empty          | iplist:OfficeNAT         | jc iplists update OfficeNAT --ips … |

Runtime signal (Directory Insights, last 24h):
- Total auth events: 1,247
- Failed SSO auths: 18 (1.4%)
- Top offending IP: 185.220.101.42 (12 failures, all targeting alice@acme.com)
- Recommendation: jc auth-policies create — block this IP range
```

## Why this layout

The pre-2026.06 version of this skill scripted ~10 individual `jc`
queries inline. `jc audit` consolidates 8 of those into a single
structured fetch with severity tagging, so the skill now:

- Spends most of its token budget on **interpretation** rather than
  re-deriving findings from raw data
- Gets new checks automatically when the audit registry grows
- Produces consistent severity language with `jc-compliance-check` and
  CI gates (`jc audit --exit-code --threshold high`)
