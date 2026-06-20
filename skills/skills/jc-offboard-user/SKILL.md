---
name: jc-offboard-user
description: Offboard a JumpCloud user — lock account, remove from groups, reset MFA, optionally delete using the jc CLI
---

# Offboard a JumpCloud User

Walk through securely removing a user's access, cleaning up group memberships, and optionally deleting the account.

## Prerequisites

- `jc` installed and authenticated (`jc auth status`)

## Gather Information

Ask the user for:
- **Username or ID** of the user to offboard (required)
- **Delete the account?** (yes/no — default: no, just lock)

## Steps

### 1. Verify the user exists

```bash
jc users get USERNAME -t
```

Confirm the user identity before proceeding.

### 2. Lock the account immediately

```bash
jc users lock USERNAME
```

This prevents login right away.

### 3. Find all group memberships

```bash
jc graph traverse --from user:USERNAME --to user_group -t
```

### 4. Remove from all groups

For each group found in step 3:

```bash
jc groups remove-member GROUP_NAME --user USERNAME --force
```

### 5. Reset MFA enrollment

```bash
jc users reset-mfa USERNAME
```

This invalidates any TOTP tokens the user has configured.

### 6. (Optional) Delete the user

If the admin confirmed deletion:

```bash
jc users delete USERNAME --plan
```

Show the plan first. Then:

```bash
jc users delete USERNAME --force
```

### 7. Verify

If deleted:
```bash
jc users get USERNAME
# Should return "not found"
```

If locked (not deleted):
```bash
jc users get USERNAME -t
# Should show account_locked: true
```

## Safety Notes

- Always lock the account FIRST (step 2) before any other actions
- Use `--plan` before `--force` on destructive operations
- Removing from groups is reversible; deletion is not
- Consider whether you need to revoke app access separately (`jc graph unbind`)
