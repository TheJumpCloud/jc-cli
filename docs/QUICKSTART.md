# jc Quick Start

## Install & Setup

```bash
# Option 1: Download binary (macOS Apple Silicon example)
curl -L https://github.com/TheJumpCloud/jc-cli/releases/latest/download/jc-darwin-arm64 -o jc
chmod +x jc && sudo mv jc /usr/local/bin/

# Option 2: Build from source
git clone https://github.com/TheJumpCloud/jc-cli.git && cd jc-cli && make install

# Run the setup wizard (walks you through auth, org, output prefs)
# The wizard recommends service-account (OAuth 2.0) auth — short-lived
# tokens, easy to rotate. API key auth is the fallback.
jc setup

# Or authenticate manually:
jc auth login --service-account     # recommended for new deployments
jc auth login                       # API key (legacy, still works)
```

Verify: `jc users list -t` should show your org's users.

---

## Everyday Commands

```bash
# List resources (-t = table format)
jc users list -t
jc devices list -t
jc groups user list -t

# Search
jc users list --filter "email:eq:jane@acme.com" -t
jc devices list --filter "os:eq:Mac OS X" --filter "active:eq:true" -t
jc users search "jane" -t

# Get a single resource (by name or ID)
jc users get jane.doe
jc devices get my-macbook

# Recent security events
jc insights query --service all --last 24h -t
jc insights query --service sso --last 7d --event-type user_login_attempt -t
```

---

## Cheat Sheet

### Output

| Flag | Effect |
|------|--------|
| `-t` | Table output (default is JSON) |
| `--output csv` | CSV |
| `--output yaml` | YAML |
| `--fields username,email` | Only these fields |
| `--exclude password_date` | All fields except these |
| `--all` | Show every field |
| `--ids` | One ID per line (for piping) |
| `--query "JMESPATH"` | Post-process with JMESPath |

### Filtering

```
--filter "field:op:value"

Operators: eq, ne, gt, lt, gte, lte
Examples:  --filter "activated:eq:true"
           --filter "os:eq:Mac OS X"
           --filter "created:gt:2024-01-01"
Multiple:  --filter "os:eq:Linux" --filter "active:eq:true"  (AND)
```

### Key Resources

| Command | What it does |
|---------|-------------|
| `jc users list/get/create/update/delete` | User management |
| `jc devices list/get/update/delete` | Device management |
| `jc groups user list/get/create/update/delete` | User groups |
| `jc groups device list/get/create/update/delete` | Device groups |
| `jc commands list/get/create/run` | Scheduled commands |
| `jc policies list/get/create/update/delete` | Configuration policies |
| `jc apps list/get/create/update/delete` | SSO applications |
| `jc admins list/get/create/update/delete` | Admin accounts |
| `jc auth-policies list/get/create/update/delete` | Conditional access |
| `jc iplists list/get/create/update/delete` | IP allow/block lists |
| `jc insights query/count/distinct` | Directory Insights events |
| `jc graph traverse --from TYPE:NAME --to TYPE` | Association graph |

### Interactive TUI

```bash
jc tui
```

| Key | Action |
|-----|--------|
| `j` / `k` | Move down / up |
| `Enter` | Open selected item |
| `Esc` | Go back |
| `/` | Filter or search |
| `s` / `S` | Cycle sort field / toggle direction |
| `a` | Toggle all fields |
| `c` | Copy ID to clipboard |
| `e` | Export (then `j`=JSON clipboard, `c`=CSV file, `J`=JSON file) |
| `r` | Refresh |
| `n` | Create new resource (list screen) |
| `d` | Delete resource (detail screen) |
| `E` | Edit resource (detail screen) |
| `d` | Open dashboard (home screen) |
| `b` | Toggle bookmark |
| `Tab` | Switch to associations (detail screen) |
| `Ctrl+S` | Save form (create/edit screen) |
| `x` | AI explanation (event detail) |
| `?` | Help overlay |
| `q` | Quit |

### AI Features

```bash
# Natural language → CLI commands
jc ask "show me all macOS devices"
jc ask "find SSO failures in the last week"

# Configure LLM provider (one-time)
jc config set ask.provider anthropic
jc config set ask.api_key sk-ant-...

# Explain what a command would do (no API calls made)
jc explain users delete john.doe
```

### Recipes (Multi-step Workflows)

```bash
jc recipe list -t                          # See all recipes
jc recipe show onboard-user                # View steps
jc recipe run onboard-user \
  --param username=jdoe \
  --param email=jdoe@acme.com              # Run it
jc recipe run onboard-user --plan          # Preview only
```

### MCP Server (for AI Assistants)

Add to Claude Desktop config (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "jc": {
      "command": "jc",
      "args": ["mcp", "serve"]
    }
  }
}
```

158 tools available. All destructive operations require explicit confirmation.

### Plan Mode (Dry Run)

```bash
jc users delete john.doe --plan    # Shows what would happen, no changes made
jc iplists create --name test --plan
```

---

## Profiles (Multi-Org)

```bash
jc auth login --profile staging    # Add a profile
jc auth switch staging             # Switch active profile
jc users list --org production     # One-off override
jc auth status                     # Show current auth
```

## Common Pipes

```bash
# Delete all suspended users
jc users list --filter "suspended:eq:true" --ids | xargs -I{} jc users delete {} --force

# Export all devices to CSV
jc devices list --all --output csv > devices.csv

# Count users by department
jc users list --query "[].department" | jq 'group_by(.) | map({dept: .[0], count: length})'
```

## Getting Help

```bash
jc --help                  # Top-level commands
jc users --help            # Resource subcommands
jc users create --help     # Flag details for a specific command
jc tui                     # Then press ? for TUI keybindings
```
