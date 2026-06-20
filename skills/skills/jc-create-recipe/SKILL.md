---
name: jc-create-recipe
description: Create jc CLI recipes — multi-step YAML workflows with template variables, conditional steps, and output capture for JumpCloud automation
---

# Create a jc Recipe

Recipes are YAML-defined multi-step workflows for the jc JumpCloud CLI. They support template variables, conditional execution, output capture between steps, and plan/execute modes.

## Recipe Format

```yaml
name: recipe-name
description: What this recipe does
author: Author Name
version: "1.0"
tags: [tag1, tag2]

parameters:
  - name: param_name
    description: What this parameter is for
    required: true
    type: string           # string, bool, int
  - name: optional_param
    description: Optional parameter with default
    required: false
    type: string
    default: "default_value"

steps:
  - name: step-name
    command: 'jc-command --flag {{ .param_name }}'
    capture: output_var    # optional: save stdout to a variable
    when: "{{ .condition }}"  # optional: only run if truthy
    continue_on_error: false  # optional: don't stop on failure

on_success:
  message: "Recipe completed: {{ .output_var }}"

on_failure:
  message: "Failed at step: {{ .failed_step }}"
```

## Template Syntax

Templates use Go `{{ }}` syntax:

- `{{ .param_name }}` — insert a parameter or captured variable
- `{{ if .param }}...{{ end }}` — conditional block (runs if param is non-empty)
- `{{ if .param }} --flag "{{ .param }}"{{ end }}` — conditional flag inclusion

## Key Rules

1. **Commands are jc commands WITHOUT the `jc` prefix.** Write `users list -t`, not `jc users list -t`.
2. **Capture stores trimmed stdout** into the named variable for use in later steps.
3. **When conditions:** empty string, `"false"`, and `"0"` are falsy. Everything else is truthy.
4. **Quote values with spaces** in commands: `--name "{{ .group_name }}"`
5. **`--ids` flag** is useful for capture — returns just the ID on stdout.
6. **Steps run sequentially.** Each step gets a fresh Cobra command instance.
7. **No shell execution.** Commands are parsed as quoted strings, not run through a shell.

## Example: Onboard User

```yaml
name: onboard-user
description: Create a new user, add to specified groups, and verify the account
tags: [onboarding, users, automation]

parameters:
  - name: username
    description: User's login username
    required: true
    type: string
  - name: email
    description: User's email address
    required: true
    type: string
  - name: firstname
    description: User's first name
    required: true
    type: string
  - name: lastname
    description: User's last name
    required: true
    type: string
  - name: department
    description: User's department
    required: false
    type: string
  - name: group
    description: User group to add the user to
    required: false
    type: string

steps:
  - name: create-user
    command: 'users create --username {{ .username }} --email {{ .email }} --firstname {{ .firstname }} --lastname {{ .lastname }}{{ if .department }} --department "{{ .department }}"{{ end }} --ids'
    capture: user_id

  - name: add-to-group
    command: 'groups add-member {{ .group }} --user {{ .user_id }}'
    when: "{{ .group }}"

  - name: verify-user
    command: 'users get {{ .user_id }}'

on_success:
  message: "User {{ .username }} ({{ .email }}) onboarded successfully"

on_failure:
  message: "Onboarding failed at step: {{ .failed_step }}"
```

## Example: Security Audit

```yaml
name: security-audit
description: Run a security audit checking MFA, auth failures, and admin access
tags: [security, audit, compliance]

parameters:
  - name: days
    description: Number of days to look back for security events
    required: false
    type: int
    default: "7"

steps:
  - name: check-auth-failures
    command: 'insights query --service sso --event-type sso_auth_failed --last {{ .days }}d -t --limit 50'
    continue_on_error: true

  - name: count-auth-failures
    command: 'insights count --service sso --event-type sso_auth_failed --last {{ .days }}d'
    capture: failure_count
    continue_on_error: true

  - name: list-admins
    command: 'admins list -t'

  - name: check-mfa-status
    command: 'users list --fields username,email,totp_enabled -t'

on_success:
  message: "Security audit complete. Auth failures in last {{ .days }} days: {{ .failure_count }}"
```

## Example: Conditional Cleanup

```yaml
name: stale-device-cleanup
description: Find and optionally remove devices not seen in N days
tags: [devices, cleanup, maintenance]

parameters:
  - name: days
    description: Days since last contact to consider stale
    required: false
    type: int
    default: "90"
  - name: delete
    description: Set to true to actually delete stale devices
    required: false
    type: bool
    default: "false"

steps:
  - name: list-stale-devices
    command: 'devices list --all --query "[?lastContact < ''2024-01-01''].{id:_id, hostname:hostname, lastContact:lastContact}" -t'

  - name: delete-stale-devices
    command: 'devices list --query "[?lastContact < ''2024-01-01'']._id" --ids'
    when: "{{ .delete }}"
    continue_on_error: true

on_success:
  message: "Stale device check complete"
```

## How to Save and Run Recipes

Save recipe YAML files to `~/.config/jc/recipes/`:

```bash
# Save
mkdir -p ~/.config/jc/recipes
# Write the YAML file to ~/.config/jc/recipes/my-recipe.yaml

# Validate
jc recipe validate ~/.config/jc/recipes/my-recipe.yaml

# List available recipes (built-in + user-defined)
jc recipe list -t

# Preview what a recipe would do
jc recipe run my-recipe --param key=value --plan

# Execute
jc recipe run my-recipe --param key=value

# Force (skip confirmations in steps)
jc recipe run my-recipe --param key=value --force
```

## Available jc Commands for Recipe Steps

Any jc command works as a recipe step. Common ones:

| Command | Use in Recipes |
|---------|---------------|
| `users create ... --ids` | Create and capture user ID |
| `users get USERNAME` | Verify user exists |
| `users list --filter ... --ids` | Get filtered user IDs |
| `users lock USERNAME` | Lock account |
| `users unlock USERNAME` | Unlock account |
| `users reset-mfa USERNAME` | Reset MFA |
| `groups add-member GROUP --user USER` | Add to group |
| `groups remove-member GROUP --user USER --force` | Remove from group |
| `commands run "CMD" --device DEVICE` | Execute command on device |
| `policies results POLICY -t` | Check policy status |
| `insights query --service SVC --last Nd -t` | Query audit events |
| `insights count --service SVC --last Nd` | Count events (good for capture) |
| `graph traverse --from TYPE:ID --to TYPE -t` | Traverse associations |
| `graph bind --from TYPE:ID --to TYPE:ID` | Create association |

## Tips

- Use `--ids` on create/list commands when you need to capture an ID for later steps
- Use `continue_on_error: true` on informational steps (like audit queries) that shouldn't stop the recipe
- Use `when` to make steps conditional on parameters or captured values
- User-defined recipes in `~/.config/jc/recipes/` override built-in recipes with the same name
- Test with `--plan` first to see what would execute without making API calls
