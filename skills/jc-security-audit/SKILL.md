---
name: jc-security-audit
description: Run a JumpCloud security audit — check MFA adoption, find inactive users, review auth failures, audit admins using the jc CLI
---

# JumpCloud Security Audit

Run a comprehensive security audit across a JumpCloud organization.

## Prerequisites

- `jc` installed and authenticated (`jc auth status`)
- Insights access requires appropriate permissions

## Audit Steps

### 1. MFA Adoption

Find users without MFA enabled:

```bash
jc users list --query "[?totp_enabled==\`false\`].{username:username,email:email,activated:activated}" -t
```

Count total vs MFA-enabled:
```bash
jc users list --query "length([?totp_enabled==\`true\`])"
jc users list --query "length(@)"
```

Report the MFA adoption percentage. Flag any activated users without MFA as a risk.

### 2. Suspended and Locked Accounts

```bash
jc users list --filter "suspended:eq:true" -t
jc users list --filter "account_locked:eq:true" -t
```

Review whether these accounts should be deleted or are appropriately locked.

### 3. Failed Authentication Events (Last 24h)

```bash
jc insights query --service sso --last 24h --event-type sso_auth_failed -t
```

Look for:
- Repeated failures from the same user (possible brute force)
- Failures from unusual IPs
- Failures at unusual times

### 4. All Auth Events (Last 7 Days)

```bash
jc insights count --service all --last 7d
jc insights query --service all --last 7d --limit 20 -t
```

### 5. Admin Accounts

```bash
jc admins list -t
```

Check:
- Number of admin accounts (fewer is better)
- Whether all admins have MFA enabled
- Any inactive or unnecessary admin accounts

### 6. Auth Policy Coverage

```bash
jc auth-policies list -t
```

Review:
- Are there conditional access policies in place?
- Are any disabled that should be enabled?
- Do policies require MFA for sensitive operations?

### 7. Device Compliance

```bash
jc policies list -t
```

For key policies (e.g., FileVault, screen lock):
```bash
jc policies results <policy-name> -t
```

Check for devices with `failed` or `pending` status.

### 8. IP Lists

```bash
jc iplists list -t
```

Verify that office/VPN IP ranges are configured for auth policies.

## Report

Summarize findings as:

| Area | Status | Finding |
|------|--------|---------|
| MFA Adoption | PASS/WARN/FAIL | X% of users have MFA enabled |
| Inactive Accounts | PASS/WARN | X suspended, Y locked |
| Auth Failures (24h) | PASS/WARN/FAIL | X failed attempts |
| Admin Accounts | PASS/WARN | X admins, MFA status |
| Auth Policies | PASS/WARN | X policies active |
| Device Compliance | PASS/WARN/FAIL | X devices non-compliant |

Include specific recommendations for any WARN or FAIL items.
