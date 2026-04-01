---
name: jc-compliance-check
description: Run a JumpCloud compliance check — MFA enforcement, device management, policy coverage, password policy, admin security using the jc CLI
---

# JumpCloud Compliance Check

Run a structured compliance check across a JumpCloud organization and produce a pass/warn/fail report.

## Prerequisites

- `jc` installed and authenticated (`jc auth status`)

## Compliance Checks

### Check 1: MFA Enforcement

**Goal:** All active users should have MFA enabled.

```bash
# Total active users
jc users list --filter "activated:eq:true" --query "length(@)"

# Active users with MFA
jc users list --filter "activated:eq:true" --query "length([?totp_enabled==\`true\`])"

# Users missing MFA (the actionable list)
jc users list --filter "activated:eq:true" --query "[?totp_enabled==\`false\`].{username:username,email:email}" -t
```

| Threshold | Result |
|-----------|--------|
| 100% MFA | PASS |
| 80-99% MFA | WARN |
| < 80% MFA | FAIL |

### Check 2: Device Management

**Goal:** All devices should be actively managed (contacted recently).

```bash
# All devices
jc devices list --query "length(@)"

# Devices by OS
jc devices list --query "[].os" | sort | uniq -c | sort -rn

# Devices not contacted in 30+ days (likely unmanaged)
jc devices list --all --query "[?lastContact < '$(date -u -v-30d +%Y-%m-%dT%H:%M:%SZ)'].{hostname:hostname,os:os,lastContact:lastContact}" -t
```

| Threshold | Result |
|-----------|--------|
| All contacted within 30 days | PASS |
| > 90% contacted within 30 days | WARN |
| < 90% contacted within 30 days | FAIL |

### Check 3: Policy Coverage

**Goal:** Key security policies should be applied to all devices.

```bash
jc policies list -t
```

For each critical policy (FileVault, screen lock, firewall, etc.):

```bash
jc policies results POLICY_NAME --query "[?status=='failed' || status=='pending'].{system:systemID,status:status}" -t
```

| Threshold | Result |
|-----------|--------|
| All devices applied | PASS |
| Pending but no failures | WARN |
| Any failures | FAIL |

### Check 4: Password Policy

**Goal:** Organization has a strong password policy configured.

First, get the org ID:
```bash
jc org list --ids
```

Then check password policy:
```bash
jc org settings THE_ORG_ID --query "passwordPolicy"
```

Check for:
- Minimum length >= 12
- Complexity requirements enabled
- Account lockout configured

### Check 5: Admin Account Security

**Goal:** Minimal admin accounts, all with MFA.

```bash
jc admins list -t
```

Check:
- Number of admins (fewer than 5 for small orgs, proportional for larger)
- All admins should have `enableMultiFactor: true`
- No inactive/unused admin accounts

| Threshold | Result |
|-----------|--------|
| All admins have MFA, reasonable count | PASS |
| Some admins missing MFA | WARN |
| Many admins, no MFA enforcement | FAIL |

### Check 6: Conditional Access Policies

**Goal:** At least one conditional access policy is active.

```bash
jc auth-policies list -t
```

Check:
- At least one policy exists and is enabled
- Policies cover MFA requirements for sensitive access
- IP-based restrictions are in place if applicable

```bash
jc iplists list -t
```

| Threshold | Result |
|-----------|--------|
| Active policies with MFA/IP restrictions | PASS |
| Policies exist but some disabled | WARN |
| No conditional access policies | FAIL |

## Report Format

Produce a summary table:

```
JumpCloud Compliance Report — ORG_NAME — DATE

| # | Check                    | Status | Detail                           |
|---|--------------------------|--------|----------------------------------|
| 1 | MFA Enforcement          | PASS   | 100% of 150 active users         |
| 2 | Device Management        | WARN   | 3 of 200 devices stale (>30 days)|
| 3 | Policy Coverage          | PASS   | FileVault 100%, Firewall 100%    |
| 4 | Password Policy          | PASS   | Min 12 chars, complexity enabled |
| 5 | Admin Security           | WARN   | 4 admins, 1 missing MFA          |
| 6 | Conditional Access       | PASS   | 3 policies active, MFA required  |

Overall: 4 PASS, 2 WARN, 0 FAIL
```

For each WARN or FAIL, include a specific recommendation with the jc command to fix it.
