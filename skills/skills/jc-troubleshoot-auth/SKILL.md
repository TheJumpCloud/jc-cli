---
name: jc-troubleshoot-auth
description: Diagnose JumpCloud authentication issues for a user — check account status, MFA, groups, auth events, and policies using the jc CLI
---

# Troubleshoot JumpCloud Authentication

Systematically diagnose why a user can't log in to JumpCloud or a connected application.

## Prerequisites

- `jc` installed and authenticated (`jc auth status`)

## Gather Information

Ask for:
- **Username or email** of the affected user (required)
- **What they're trying to access** (JumpCloud portal, SSO app, LDAP, RADIUS?)
- **Error message** they're seeing (if any)

## Diagnostic Steps

### 1. Check account status

```bash
jc users get USERNAME -t
```

Look for:
- `activated`: false = user hasn't activated their account yet
- `suspended`: true = account is suspended by an admin
- `account_locked`: true = account is locked (too many failed attempts or manual lock)

If locked: `jc users unlock USERNAME`
If suspended: `jc users update USERNAME --suspended false` (if appropriate)

### 2. Check MFA status

From the user get output, check:
- `totp_enabled`: Is TOTP/MFA configured?
- If MFA is required by auth policy but not set up, the user can't complete login

To reset MFA: `jc users reset-mfa USERNAME`

### 3. Check group memberships

```bash
jc graph traverse --from user:USERNAME --to user_group -t
```

If the user is trying to access an SSO app, verify they're in a group that's bound to that app:
```bash
jc graph traverse --from user:USERNAME --to application -t
```

### 4. Check recent auth events

```bash
jc insights query --service all --last 24h --query "[?initiated_by.username=='USERNAME']" -t
```

Look for:
- `sso_auth_failed` events — indicates SSO login failures
- `user_login_failed` — indicates portal login failures
- `success: false` — any failed operations
- The `client_ip` field — is the user coming from an expected IP?

### 5. Simulate auth policies

```bash
jc auth-policies simulate --user USERNAME -t
```

If you know the user's IP:
```bash
jc auth-policies simulate --user USERNAME --ip THEIR_IP -t
```

This shows which conditional access policies would apply and whether they'd allow, deny, or require MFA.

### 6. Check the specific application (if SSO)

```bash
jc apps get APP_NAME -t
```

Verify:
- The app is active
- The user (or their group) is bound to the app via graph

## Common Causes

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| "Account not activated" | User never completed activation | Resend activation or reset password |
| "Account locked" | Too many failed attempts | `jc users unlock USERNAME` |
| "Account suspended" | Admin action | `jc users update USERNAME --suspended false` |
| "MFA required" but not set up | Auth policy requires MFA | User needs to enroll MFA, or reset with `jc users reset-mfa` |
| Can access portal but not app | Missing group/app binding | Check `jc graph traverse --from user:USERNAME --to application` |
| Denied by auth policy | IP/location/device restriction | Check `jc auth-policies simulate` output |

## Escalation

If the above steps don't identify the issue:
1. Check auth policy blast radius: `jc auth-policies blast-radius POLICY_NAME -t`
2. Review all org auth policies: `jc auth-policies list --all -t`
3. Check RADIUS/LDAP config if the user accesses network resources: `jc radius list -t` / `jc ldap list -t`
