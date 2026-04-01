---
name: jc-onboard-user
description: Onboard a new JumpCloud user — create account, add to groups, verify setup using the jc CLI
---

# Onboard a JumpCloud User

Walk through creating a new user, adding them to groups, and verifying setup.

## Prerequisites

- `jc` installed and authenticated (`jc auth status`)

## Gather Information

Ask the user for:
- **Username** (required)
- **Email** (required)
- **First name** (optional)
- **Last name** (optional)
- **Department** (optional)
- **Groups to add to** (optional, comma-separated)

## Steps

### 1. Preview the user creation

```bash
jc users create --username <username> --email <email> --firstname <first> --lastname <last> --department <dept> --plan
```

Show the plan output. Confirm with the user before proceeding.

### 2. Create the user

```bash
jc users create --username <username> --email <email> --firstname <first> --lastname <last> --department <dept>
```

Capture the user ID from the output (`--ids` flag returns just the ID).

### 3. Add to groups

For each group the user should be added to:

```bash
jc groups add-member <group-name> --user <username>
```

If a group doesn't exist, offer to create it first:
```bash
jc groups user create --name <group-name>
```

### 4. Verify setup

```bash
jc users get <username> -t
```

Check that the user exists, is activated, and has the correct details.

### 5. Verify group memberships

```bash
jc graph traverse --from user:<username> --to user_group -t
```

Confirm the user appears in all expected groups.

## Safety Notes

- Use `--plan` to preview any create/update/delete before executing
- The `users create` command does NOT send an activation email by default
- If the user should receive an activation email, note this to the admin
- All mutations require confirmation unless `--force` is set
