# jc — JumpCloud CLI Agent Context

## What this tool does

`jc` is a CLI for managing JumpCloud organizations. It covers the full JumpCloud API surface (V1, V2, Directory Insights, Graph) with 40+ resource types. Single Go binary, no dependencies.

## Authentication

```bash
jc auth login              # Interactive setup (API key)
jc auth login --profile prod  # Named profile
jc auth status             # Check current auth
```

Use `--org <profile>` to override the active profile per command.

## Core resources and common operations

| Resource | List | Get | Create | Update | Delete |
|----------|------|-----|--------|--------|--------|
| users | `jc users list` | `jc users get <id>` | `jc users create` | `jc users update` | `jc users delete` |
| devices | `jc devices list` | `jc devices get <id>` | — | `jc devices update` | `jc devices delete` |
| groups | `jc groups user list` / `jc groups device list` | `jc groups user get` | `jc groups user create` | `jc groups user update` | `jc groups user delete` |
| apps | `jc apps list` | `jc apps get` | `jc apps create` | `jc apps update` | `jc apps delete` |
| policies | `jc policies list` | `jc policies get` | `jc policies create` | `jc policies update` | `jc policies delete` |
| admins | `jc admins list` | `jc admins get` | `jc admins create` | — | `jc admins delete` |
| commands | `jc commands list` | `jc commands get` | `jc commands create` | `jc commands update` | `jc commands delete` |

Other resources: `ad`, `apple-mdm`, `windows-mdm`, `auth-policies`, `custom-emails`, `duo`, `gsuite`, `identity-providers`, `insights`, `iplists`, `ldap`, `office365`, `org`, `policy-groups`, `radius`, `saas-management`, `software`, `user-states`, `assets`, `graph`, `system-insights`.

## Output control

```bash
# Formats
jc users list -o json          # Default — pretty JSON
jc users list -o table         # ASCII table (or -t shorthand)
jc users list -o csv           # CSV
jc users list -o ndjson        # One JSON object per line
jc users list -o yaml          # YAML
jc users list -o human         # Key-value pairs

# Field selection
jc users list --fields username,email
jc users list --exclude password_date,totp_enabled
jc users list --all            # All fields

# Agent-optimized
jc users list --brief          # First 2 default fields, compact ndjson
jc users list --ids            # One ID per line (for piping)
jc users list -q               # Quiet — exit code only

# Filtering and querying
jc users list --filter 'department:eq:Engineering'
jc users list --query "[?activated==true].{name:username,email:email}"
```

## Safety flags for destructive operations

```bash
jc users delete alice --dry-run    # Preview what would happen (no changes)
jc users delete alice --plan       # Same as --dry-run
jc users delete alice --force      # Skip confirmation prompt
jc users delete alice --timeout 30s  # Abort if it takes too long
```

All delete commands support `--dry-run`/`--plan`, `--force`, and confirmation prompts.

## Piping and batch operations

```bash
# Pipe IDs between commands
jc users list --filter 'suspended=true' --ids | jc users delete --force

# Stdin batch mode
cat user-ids.txt | jc users delete --stdin --force

# File batch mode — the runbook-friendly variant (newline identifiers,
# blank lines + # comments ignored; works on every single-identifier
# mutating command: delete/lock/unlock/erase/restart/reset-*)
jc users delete --from-file users-to-offboard.txt --plan   # preview with line numbers
jc users delete --from-file users-to-offboard.txt --force  # batch requires --force
jc devices lock --from-file lost-devices.txt --force

# Bulk CSV operations (per-row create/update/delete via the `operation` column;
# unknown columns are errors; --plan previews; execution requires --force)
jc bulk users --file new-users.csv
jc bulk user-groups --file groups.csv --force
jc bulk device-groups --file groups.csv --force
jc bulk devices --file devices.csv --force        # update/delete only
jc bulk admins --file admins.csv --force

# Cross-profile fan-out (MSP): run any command across org profiles.
# Destructive inner commands (per jc:class) require --allow-destructive;
# read_only-role profiles are skipped for writes.
jc multi --filter 'prod-*' -- users list --ids    # IDs prefixed <profile>/
jc multi --profiles a,b,c -- policies list        # JSON aggregate [{profile,status,data|error}]
```

Batch rules: exactly one identifier source (inline arg, `--from-file`, or `--stdin`); batch execution requires `--force`/`--non-interactive`; failures are collected per row and reported with original file line numbers; `--plan` renders one aggregated preview.

## Graph associations

```bash
jc graph list-associations --type user --id <user-id>
jc graph add --type user --id <user-id> --target-type user_group --target-id <group-id>
jc graph remove --type user --id <user-id> --target-type user_group --target-id <group-id>
```

## Directory Insights (audit logs)

```bash
jc insights query --service sso --last 24h
jc insights query --service all --last 7d --filter 'event_type:eq:user_login_attempt'
jc insights saved-searches list
```

## Machine-readable schema

```bash
jc schema commands          # Full command manifest (JSON)
jc schema flags             # All global flags
jc schema resources         # Resource types and their operations
```

## Recipes (multi-step workflows)

```bash
jc recipe list              # List available recipes
jc recipe run <name>        # Run a recipe
jc recipe show <name>       # Show recipe steps
```

## MDM custom policies

```bash
# Apple: browse Apple's vendored schema catalog, emit .mobileconfig, create JC policies
jc apple-mdm payloads list --os macOS --search wifi
jc apple-mdm payloads show com.apple.wifi.managed
jc apple-mdm payloads create-policy com.apple.security.firewall \
    --name "Firewall" --values EnableFirewall=true --plan

# Windows: browse Microsoft's CSP catalog (~5,100 settings incl. standalone CSPs
# like BitLocker CSP; fetched on demand, cached), then create OMA-URI/registry policies
jc windows-mdm csp list --search bitlocker --kind csp
jc windows-mdm csp show BitLocker/RequireDeviceEncryption
jc windows-mdm csp template Camera/AllowCamera --output-file s.json
jc windows-mdm oma-uri create-policy --name "Lockdown" --settings-file s.json --plan
jc windows-mdm registry create-policy --name "Autorun off" \
    --key 'location=SOFTWARE\Policies\Microsoft\Windows\Explorer,name=NoAutorun,type=DWORD,data=1' --plan
```

## Global flags reference

| Flag | Short | Description |
|------|-------|-------------|
| `--output` | `-o` | Output format: json, table, csv, human, yaml, ndjson |
| `--table` | `-t` | Shorthand for --output table |
| `--brief` | | Compact output, minimal fields (agent-optimized) |
| `--fields` | | Include only these fields |
| `--exclude` | | Exclude these fields |
| `--all` | | Show all fields |
| `--ids` | | Output one ID per line |
| `--query` | | JMESPath expression |
| `--quiet` | `-q` | Suppress output |
| `--force` | `-f` | Skip confirmation prompts |
| `--dry-run` | | Preview changes without executing |
| `--plan` | | Same as --dry-run |
| `--timeout` | | Max execution time (e.g. 30s, 2m) |
| `--non-interactive` | | Disable all prompts |
| `--org` | | Override active profile |
| `--api-key` | | Override API key |
| `--verbose` | `-v` | HTTP request logging |
| `--debug` | | Debug logging |
| `--no-cache` | | Bypass name-to-ID cache |
| `--no-color` | | Disable color output |

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Usage/validation error |
| 3 | Authentication failed |
| 4 | Permission denied |
| 5 | Rate limited |
| 10 | Plan mode (no changes made) |
| 130 | Interrupted (Ctrl+C) |

## Command annotations (jc:class)

Every leaf command carries a single `jc:class` Cobra annotation declaring its
mutation class. Four values exist:

| Class | Meaning | Example |
|---|---|---|
| `read-only` | GETs only, never writes to the JC API | `jc users list`, `jc auth-policies blast-radius` |
| `mutating` | Writes that are reversible or low-impact | `jc users create`, `jc groups add-member` |
| `destructive` | Hard or impossible to reverse; classed by worst-case capability, so wrappers like `jc recipe run` / `jc bulk users` / `jc commands run` count even if a given invocation only reads | `jc users delete`, `jc devices erase`, `jc access-requests revoke` |
| `internal` | Never touches the JC API (local file ops, credential mgmt, schema introspection) | `jc explain`, `jc audit verify`, `jc auth login` |

The classification map lives at `internal/cmd/classifications.go`. A CI lint
test (`TestEveryLeafIsClassified`) fails the build when a new leaf lands
without an entry, and a sibling test refuses stale entries — so the map
stays exact.

Today the annotation is informational and lint-only. Follow-ups (tracked
separately) will use it to drive MCP filtering by capability
(`mcp.blocked_tools: ["tag:destructive"]`) and to deprecate the
reflection-based destructive-op gate in `internal/mcp/`. The annotation
is the single source of truth those callers will read.

## Tips for agents

- Use `--brief` for token-efficient list output.
- Use `--ids` to get IDs for piping into other commands.
- Use `--dry-run` before any destructive operation.
- Use `--force --non-interactive` for unattended execution.
- Use `--timeout 30s` to prevent hanging on slow API calls.
- Use `jc schema commands` to discover all available commands programmatically.
- Name resolution works everywhere: use `username` instead of raw IDs.
- Use `jc explain <command>` to understand what a command does before running it.
