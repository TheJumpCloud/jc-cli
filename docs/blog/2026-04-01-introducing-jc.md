# Introducing jc: A Modern, AI-Ready CLI for JumpCloud

> Single Go binary. 28 resource types. 195 MCP tools. Six output formats. Interactive TUI. Recipe engine. Natural language interface. Open source.

---

If you manage a JumpCloud organization, you know the admin console well. It's polished, it's visual, and it works great for one-off tasks. But when you need to lock 50 suspended users, audit MFA adoption across your org, pipe device lists into a script, or automate onboarding from a CI pipeline, the console can turn into a bottleneck.

**jc** is an open-source command-line interface that gives you the full JumpCloud API surface in a single, dependency-free Go binary. It's built for the way power users and automation actually work: piping, filtering, scripting, and — increasingly — talking to AI.

```
https://github.com/TheJumpCloud/jc-cli
```

---

## What You Can Do With It

### Manage everything from the terminal

28 resource types, full CRUD, one consistent interface:

```bash
jc users list -t                                    # Table view of all users
jc devices list --filter "os:eq:Mac OS X" -t        # Filter by OS
jc groups add-member Engineering --user jdoe         # Add user to group
jc commands run "Install Agent" --device JDOE-MBP    # Execute command on device
jc policies results "FileVault" -t                   # Check policy compliance
jc insights query --service sso --last 24h -t        # Audit SSO events
```

Users, devices, groups, commands, policies, apps, admins, auth policies, IP lists, identity providers, RADIUS, LDAP, Active Directory, Apple MDM, software management, SaaS management, assets, policy groups, G Suite, Office 365, Duo MFA, custom emails — it's all there.

### Pipe, filter, and transform

jc is a Unix pipeline citizen. JSON by default, table/CSV/YAML/NDJSON for humans and tooling:

```bash
# Lock all suspended users in one line
jc users list --filter "suspended:eq:true" --ids | xargs -I{} jc users lock {} --force

# Export Engineering team to CSV
jc users list --filter "department:eq:Engineering" --output csv > team.csv

# JMESPath for complex reshaping
jc users list --query "[?activated==\`false\`].{user:username,email:email}" -t
```

### Preview before you break things

Every mutation supports `--plan` mode — see exactly what will happen before it happens:

```bash
$ jc users delete jdoe --plan
┌─ Plan ────────────────────────────────────────────┐
│ Action:   DELETE user                              │
│ Target:   jdoe (aa11bb22cc33dd44ee550001)          │
│ Resource: users                                    │
│                                                    │
│ This action is IRREVERSIBLE.                       │
└───────────────────────────────────────────────────┘
```

No API call is made. Exit code 10 means "plan only" — useful for CI gates.

### Automate multi-step workflows

The built-in recipe engine runs YAML-defined workflows with template variables, conditional steps, and dry-run support:

```bash
jc recipe run onboard-user \
  --param username=jnew \
  --param email=jnew@acme.com \
  --param groups=Engineering

# Creates the user, adds to groups, verifies setup — all in one command.
# Use --plan to preview without executing.
```

### Browse interactively

`jc tui` opens a full-screen terminal UI with keyboard navigation, live filtering, detail views, inline CRUD, and a dashboard with org health metrics:

```bash
jc tui
```

Navigate resources with arrow keys, `/` to search, `Enter` for details, `e` to edit, `d` to delete, `Tab` between dashboard widgets. Every resource in the CLI is also browsable in the TUI.

---

## Designed for AI From the Ground Up

This is where jc is different from most CLIs. It was built with AI assistants in mind — not as an afterthought, but as a core design principle.

### Built-in MCP Server (195 tools)

jc includes a [Model Context Protocol](https://modelcontextprotocol.io/) server that exposes 195 tools to any MCP-compatible AI assistant — Claude Desktop, Claude Code, VS Code Copilot with MCP, Goose, and others.

```json
// claude_desktop_config.json
{
  "mcpServers": {
    "jc": {
      "command": "/usr/local/bin/jc",
      "args": ["mcp", "serve"]
    }
  }
}
```

Once connected, your AI assistant can manage your JumpCloud org directly:

> *"List all users who haven't enabled MFA"*
> *"Add jdoe to the Engineering group"*
> *"Show me failed SSO events from the last 24 hours"*
> *"Simulate what would happen if we enabled the new auth policy"*

The MCP server includes safety guardrails: destructive operations require explicit confirmation, rate limiting is built in, and audit logging tracks every tool call.

### MCP Apps: Interactive Dashboards in the Conversation

jc supports [MCP Apps](https://modelcontextprotocol.io/extensions/apps/overview) — a new MCP extension that renders interactive HTML UIs inside AI assistants. Ask for "the JumpCloud dashboard" and get a live, visual dashboard with user status, MFA adoption, device inventory, and event activity right in the chat.

For hosts that don't support MCP Apps yet, the same tool returns structured JSON that the AI interprets as a text summary. No functionality is lost.

### Natural Language Interface

Don't remember the exact flag syntax? Just ask:

```bash
$ jc ask "which users haven't activated their accounts?"

Proposed commands:
  [1] jc users list --filter "activated:eq:false" -t

Execute these commands? [y/N]
```

`jc ask` translates natural language to CLI commands using your configured LLM provider (Anthropic, OpenAI, or Ollama). It shows you the commands before executing — never runs anything without your approval.

### Machine-Readable Schema

`jc schema resources` and `jc schema commands` output structured JSON that LLMs and tools can consume to understand the CLI's full capabilities:

```bash
jc schema commands | jq '.commands | length'
# 195

jc schema resources | jq '.resources | keys'
# ["admins", "apps", "auth-policies", "commands", "devices", ...]
```

This schema drives the TUI, MCP tool registration, and `jc ask` — all from a single source of truth.

---

## Architecture Highlights

For those who care about what's under the hood:

- **Single binary, zero runtime dependencies.** Built in Go. Cross-compiled for macOS (Intel + Apple Silicon), Linux (x86_64 + ARM64), and Windows.
- **Resource-agnostic output engine.** All data flows as `[]json.RawMessage` — the output pipeline has no resource-specific types. Six formats (JSON, table, CSV, YAML, NDJSON, human-readable) work uniformly across all 28 resources.
- **Data to stdout, metadata to stderr.** List footers, progress indicators, and confirmations go to stderr. Stdout is always clean, pipeable data.
- **Transport chain: auth -> logging -> retry -> base.** Every API call goes through automatic retry with exponential backoff (429, 5xx), debug logging (with credential redaction), and TLS enforcement.
- **Service Account (OAuth 2.0) and API key auth.** Service accounts use bearer tokens with automatic refresh. The client factory transparently selects the right auth method based on your profile configuration.
- **OS keychain integration.** Credentials are stored in macOS Keychain or Linux secret-tool by default. Config files store only keychain references, never plaintext keys.
- **Auth policy simulator.** `jc auth-policies simulate` evaluates conditional access policies using pure three-valued logic — no API calls needed. `blast-radius` shows which users would be affected.

---

## Getting Started

### Install

```bash
# macOS (Apple Silicon)
curl -L https://github.com/TheJumpCloud/jc-cli/releases/latest/download/jc-darwin-arm64.tar.gz | tar xz
sudo mv jc-darwin-arm64/jc /usr/local/bin/

# macOS (Intel)
curl -L https://github.com/TheJumpCloud/jc-cli/releases/latest/download/jc-darwin-amd64.tar.gz | tar xz
sudo mv jc-darwin-amd64/jc /usr/local/bin/

# Linux (x86_64)
curl -L https://github.com/TheJumpCloud/jc-cli/releases/latest/download/jc-linux-amd64.tar.gz | tar xz
sudo mv jc-linux-amd64/jc /usr/local/bin/
```

### Setup

```bash
jc setup    # Interactive wizard — prompts for API key or service account, org ID, preferences
```

### First commands

```bash
jc users list -t                     # See your users
jc devices list -t                   # See your devices
jc insights count --service all --last 24h  # How active is your org?
jc tui                               # Browse everything interactively
```

---

## Open Source

jc is open source under the MIT license. The codebase is on GitHub:

```
https://github.com/TheJumpCloud/jc-cli
```

Feedback, bug reports, and contributions are welcome. If you manage a JumpCloud org and find yourself doing anything repetitive in the console, there's probably a faster way to do it with jc.

---

**Note:** This project is a Community Software Tool initially developed by JumpCloud. It is offered as an open source project under the MIT License "as is" without warranty of any kind. JumpCloud does not commit to maintaining, updating, or providing support for this project. Please use the [GitHub issue tracker](https://github.com/TheJumpCloud/jc-cli/issues) for any issues.
