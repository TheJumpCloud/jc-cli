---
name: jc
description: Use the jc CLI to manage JumpCloud organizations — users, devices, groups, policies, commands, insights, and 20+ more resource types
---

# jc — JumpCloud CLI

jc is a command-line interface for managing JumpCloud organizations. Single Go binary, full API coverage, six output formats.

## Preflight

1. Check if jc is installed: `jc --version`
2. Check authentication: `jc auth status`
3. If not authenticated: `jc setup` (interactive wizard) or `jc auth login`

## Core Pattern

```
jc <resource> <verb> [identifier] [flags]
```

## Resources

| Resource | Aliases | Key Verbs |
|----------|---------|-----------|
| `users` | `u` | list, get, search, create, update, delete, lock, unlock, reset-mfa, reset-password, ssh-keys |
| `devices` | `d` | list, get, search, update, delete, lock, restart, erase, fde-key |
| `groups` | `g` | user list/get/create/update/delete, device list/get/create/update/delete, add-member, remove-member |
| `commands` | | list, get, create, update, delete, run, results, trigger |
| `policies` | | list, get, create, update, delete, results |
| `apps` | | list, get, create, update, delete |
| `admins` | | list, get, create, update, delete |
| `auth-policies` | | list, get, create, update, delete, enable, disable, simulate, blast-radius |
| `iplists` | | list, get, create, update, delete |
| `insights` | `i` | query, count, distinct, save, saved, run |
| `graph` | | traverse, bind, unbind |
| `identity-providers` | `idp` | list, get, create, update, delete |
| `saas-management` | `saas` | list, get, accounts, usage, licenses |
| `software` | | list, get, create, update, delete, statuses, associations |
| `org` | | list, get, settings, update |
| `system-insights` | | \<table-name\>, tables |
| `ldap` | | list, get, create, update, delete, samba-domains |
| `ad` | | list, get, create, update, delete |
| `radius` | | list, get, create, update, delete |
| `apple-mdm` | | list, get, create, update, delete, enrollment-profiles, devices, payloads (list/show/template/create-policy/compose — Apple schema catalog → JC custom policies) |
| `windows-mdm` | | csp list/show/template/update (Microsoft CSP catalog, ~5,100 settings), oma-uri create-policy, registry create-policy |
| `policy-groups` | | list, get, create, update, delete |
| `bundle` | | list, show, validate, export (security baseline bundles: versioned YAML sets of Apple profiles + Windows OMA-URI/registry policies; builtins embedded, user bundles in ~/.config/jc/bundles/) |
| `policy-templates` | | list, get |
| `assets` | | devices/accessories/locations list/get/create/update/delete |
| `user-states` | | list, get, create, delete |
| `gsuite` | | list, get, translation-rules, import-users |
| `office365` | | list, get, translation-rules, import-users |
| `duo` | | list, get, create, delete, apps |
| `custom-emails` | | templates, get, create, update, delete |
| `app-templates` | | list, get |
| `multi` | | `jc multi --filter 'prod-*' -- <command>` — fan any command across org profiles; destructive inner commands need `--allow-destructive` |
| `bulk` | | users, user-groups, device-groups, devices, admins — CSV batch via `--file` (per-row `operation` column; `--plan` previews; execution needs `--force`) |

## Output Formats

```bash
jc users list                          # JSON (default)
jc users list -t                       # Table (shorthand)
jc users list --output csv             # CSV
jc users list --output yaml            # YAML
jc users list --output ndjson          # Newline-delimited JSON
jc users list --output human           # Human-readable
jc users list --ids                    # One ID per line (for piping)
```

## Filtering

```bash
jc users list --filter "department:eq:Engineering"
jc users list --filter "activated:eq:false"
jc devices list --filter "os:eq:Mac OS X"
jc insights query --service sso --last 24h --event-type sso_auth_failed
```

Operators: `eq`, `ne`, `gt`, `lt`, `ge`, `le`, `contains`, `startsWith`

Multiple filters combine with AND:
```bash
jc users list --filter "department:eq:Engineering" --filter "activated:eq:true"
```

## Field Selection

```bash
jc users list --fields username,email,department -t    # Show only these fields
jc users list --exclude password -t                     # Hide specific fields
jc users list --all -t                                  # Show all fields
```

## JMESPath Queries

```bash
jc users list --query "[?department=='Engineering'].{name:username,email:email}" -t
jc devices list --query "[].{host:hostname,os:os}" -t
```

## Piping Pattern

```bash
# Lock all suspended users
jc users list --filter "suspended:eq:true" --ids | xargs -I{} jc users lock {} --force

# Export filtered users to CSV
jc users list --filter "department:eq:Engineering" --output csv > team.csv
```

## Safety

```bash
jc users delete jdoe --plan      # Preview what would happen (no API call, exit code 10)
jc users delete jdoe              # Prompts for confirmation
jc users delete jdoe --force      # Skip confirmation (for scripts)
```

## Graph Associations

```bash
jc graph traverse --from user:jdoe --to user_group -t        # User's groups
jc graph traverse --from device:JDOE-MBP --to command -t     # Device's commands
jc graph bind --from user_group:Engineering --to application:Slack
jc graph unbind --from user:jdoe --to application:OldApp --force
```

## Natural Language

```bash
jc ask "which users haven't enabled MFA?"
jc explain users delete jdoe
```

## MCP Server (for AI Assistants)

```bash
jc mcp serve                    # stdio (Claude Desktop, Claude Code)
jc mcp serve --transport http   # Streamable HTTP (remote clients)
jc mcp tools                    # List available MCP tools (195)
```

Claude Desktop config:
```json
{
  "mcpServers": {
    "jc": {
      "command": "/usr/local/bin/jc",
      "args": ["mcp", "serve"]
    }
  }
}
```

## Authentication

```bash
jc setup                              # Interactive wizard
jc auth login                         # API key auth
jc auth login --service-account       # OAuth 2.0 service account
jc auth status                        # Check current auth
jc auth switch production             # Switch profiles
```

## Interactive TUI

```bash
jc tui                                # Full-screen terminal browser
```

## Batch Mode

Every single-identifier mutating command (delete/lock/unlock/erase/restart/reset-*) accepts identifier lists:

```bash
jc users delete --from-file offboard.txt --plan   # newline IDs, # comments; preview w/ line numbers
jc users delete --from-file offboard.txt --force  # batch execution requires --force
jc users list --filter "suspended:eq:true" --ids | jc users delete --stdin --force
```
