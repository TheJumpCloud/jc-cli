# Product Requirements Document (PRD)
## `jc` — A Modern, LLM-Friendly CLI for JumpCloud

**Version:** 1.0  
**Last Updated:** February 2026  
**Author:** Klaassen Consulting  
**License:** MIT  
**Status:** Draft  
**Platforms:** macOS, Linux  

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Problem Statement](#2-problem-statement)
3. [Goals & Objectives](#3-goals--objectives)
4. [Target Users](#4-target-users)
5. [Competitive Analysis](#5-competitive-analysis)
6. [Design Philosophy](#6-design-philosophy)
7. [Technical Architecture](#7-technical-architecture)
8. [Authentication & Security](#8-authentication--security)
9. [API Coverage](#9-api-coverage)
10. [Command Grammar & Structure](#10-command-grammar--structure)
11. [Core Features — Output & Formatting](#11-core-features--output--formatting)
12. [Core Features — Plan Mode](#12-core-features--plan-mode)
13. [Core Features — Conversational Mode](#13-core-features--conversational-mode)
14. [Core Features — Recipes](#14-core-features--recipes)
15. [Core Features — Pipe & Compose](#15-core-features--pipe--compose)
16. [Core Features — Explain & Help](#16-core-features--explain--help)
17. [Core Features — Directory Insights](#17-core-features--directory-insights)
18. [MCP Server](#18-mcp-server)
19. [LLM-Friendliness Specification](#19-llm-friendliness-specification)
20. [Configuration & Context](#20-configuration--context)
21. [Error Handling & Diagnostics](#21-error-handling--diagnostics)
22. [Non-Functional Requirements](#22-non-functional-requirements)
23. [Development Phases](#23-development-phases)
24. [Open-Source Strategy](#24-open-source-strategy)
25. [Success Metrics](#25-success-metrics)
26. [Appendices](#26-appendices)

---

## 1. Executive Summary

`jc` is a modern, open-source command-line interface for JumpCloud that covers the full API surface (v1, v2, and Directory Insights). Unlike existing community CLIs — which are outdated, narrowly scoped, and abandoned — `jc` is designed from the ground up with three guiding principles: **intuitive for humans**, **innovative in workflow**, and **first-class LLM-friendly**.

The CLI bridges a significant gap in the JumpCloud ecosystem. JumpCloud's official tooling is limited to a PowerShell module (Windows-centric by heritage) and raw API documentation. IT administrators on macOS and Linux — a growing majority — lack a native, ergonomic tool for day-to-day JumpCloud management. `jc` solves this by providing a fast, composable, Unix-native CLI with structured output, a built-in MCP (Model Context Protocol) server for AI agent integration, a conversational AI mode, and a recipe system inspired by the PowerShell module's gallery.

---

## 2. Problem Statement

### For IT Administrators

JumpCloud administrators working on macOS and Linux face a fragmented tooling experience. The PowerShell module, while comprehensive, requires installing PowerShell Core on non-Windows systems — an awkward dependency for Unix-native admins. The JumpCloud Admin Portal is feature-rich but slow for repetitive tasks. Direct `curl` calls against the API are powerful but verbose and error-prone.

Existing community CLI tools (Sage-Bionetworks/jccli, justmiles/jumpcloud-cli, Macgyverops/jcm) are:

- Unmaintained (last commits 2–5+ years ago)
- Narrowly scoped (user management only, or read-only)
- Missing modern CLI conventions (no structured output, no shell completions, no config management)
- Not designed for automation or LLM integration

### For LLM-Assisted Workflows

AI coding assistants (Claude Code, Cursor, GitHub Copilot CLI) are increasingly used in IT operations. These tools can generate CLI commands — but only if the CLI is predictable, well-documented, and produces machine-parseable output. Most existing JumpCloud tools fail this test:

- Output is human-formatted text that LLMs struggle to parse
- Command grammar is inconsistent
- No self-describing schema or discoverability mechanism
- No dry-run/plan mode for safe AI-generated execution

### For MSPs and Automation

Managed Service Providers managing dozens of JumpCloud tenants need scriptable multi-org workflows. The current options are raw API scripting or PowerShell — neither of which integrates cleanly into modern CI/CD pipelines, shell scripts, or infrastructure-as-code workflows on Unix systems.

---

## 3. Goals & Objectives

### Primary Goals

| # | Goal | Measurable Outcome |
|---|------|-------------------|
| G1 | Full JumpCloud API coverage | 100% of v1, v2, and Directory Insights endpoints exposed as CLI commands |
| G2 | Intuitive command grammar | New users can perform basic operations without reading docs (discoverability via `--help` and tab completions) |
| G3 | LLM-first design | An LLM can discover commands, compose pipelines, and verify actions without human intervention |
| G4 | Unix-native composability | Works seamlessly with `jq`, `grep`, `awk`, `xargs`, and standard Unix pipes |
| G5 | Recipe system | Pre-built and user-defined recipes for common multi-step workflows |
| G6 | Native MCP server | AI agents (Claude Desktop, Claude Code) can manage JumpCloud via MCP protocol |
| G7 | Safe by default | Destructive operations require confirmation; `--plan` mode shows what would happen |

### Non-Goals (Explicit Exclusions)

- **Windows support** — out of scope; PowerShell module serves this audience
- **GUI or TUI** — `jc` is a pure CLI; complementary TUI may come later as a separate project
- **JumpCloud Agent management** — `jc` manages the directory via API, not the on-device agent
- **Real-time event streaming** — Directory Insights queries are poll-based per the API; WebSocket/SSE streaming is not supported by JumpCloud's API

---

## 4. Target Users

### Primary Personas

| Persona | Description | Key Needs |
|---------|-------------|-----------|
| **Solo IT Admin** | Manages a single JumpCloud org on macOS/Linux | Fast user/device lookups, bulk operations, audit queries |
| **MSP Operator** | Manages 10–100+ JumpCloud tenants | Multi-org context switching, scriptable workflows, consistent output for reporting |
| **DevOps Engineer** | Integrates JumpCloud into CI/CD and IaC | JSON output, idempotent operations, exit codes, pipeline composability |
| **AI-Assisted Admin** | Uses Claude Code, Copilot, or similar tools | Schema discovery, predictable grammar, plan mode, structured output |

### Secondary Personas

| Persona | Description | Key Needs |
|---------|-------------|-----------|
| **Security Analyst** | Audits access, reviews auth events | Directory Insights queries, saved searches, export to SIEM-compatible formats |
| **Onboarding Specialist** | Handles user lifecycle | Recipes for onboarding/offboarding, bulk user creation from CSV |

---

## 5. Competitive Analysis

### Existing JumpCloud CLI Tools

| Tool | Language | Last Updated | API Coverage | Structured Output | LLM-Friendly | Maintained |
|------|----------|-------------|-------------|-------------------|--------------|------------|
| Sage-Bionetworks/jccli | Python | 2021 | V1 (users, groups) | Partial (JSON) | No | No |
| justmiles/jumpcloud-cli | Go | 2020 | V1 (users, commands) | Partial (JMESPath) | No | No |
| Macgyverops/jcm | Python | 2019 | V1 (read-only) | No | No | No |
| **JC PowerShell Module** | PowerShell | Active | V1 + V2 + SDK | PowerShell objects | No | Yes (JumpCloud) |
| **`jc` (this project)** | Go | New | V1 + V2 + DI | JSON-first | Yes | Yes |

### Lessons from the PowerShell Module

The JumpCloud PowerShell Module is the closest analogue and the most important reference:

**What it does well:**
- Comprehensive API coverage via both hand-written functions and auto-generated SDK
- Verb-Noun naming convention (`Get-JCUser`, `New-JCUser`, `Set-JCUser`, `Remove-JCUser`) is intuitive
- Pipeline composability (`Get-JCUser | Remove-JCUser -force`)
- Commands Gallery — curated, importable command recipes on GitHub
- Built-in `Connect-JCOnline` for org context management
- `-force` flag to skip confirmation on destructive operations

**What `jc` can improve:**
- Native Unix experience (no PowerShell dependency)
- JSON-first output (PowerShell objects don't pipe well outside PowerShell)
- LLM discoverability (schema endpoint, consistent grammar)
- Plan/dry-run mode (PowerShell module has no equivalent)
- Conversational mode (AI-powered natural language → commands)
- Speed (compiled Go binary vs. PowerShell interpreter)
- Shell completions (Bash, Zsh, Fish)

---

## 6. Design Philosophy

### 6.1 The Three Pillars

#### Intuitive

- Commands read like English: `jc users list`, `jc devices lock`, `jc groups add-member`
- Consistent grammar across all resources: `<resource> <verb> [args] [flags]`
- Smart defaults: listing shows the most useful fields; `--all` for everything
- Progressive disclosure: simple usage is simple; power features are available but not in the way
- Interactive prompts for missing required arguments (unless `--non-interactive` is set)

#### Innovative

- **Conversational mode** (`jc ask`): Natural language queries translated to API calls
- **Recipe system** (`jc recipe`): Composable, shareable, version-controlled workflow templates
- **Plan mode** (`--plan`): Preview any command's effects before execution
- **Smart context**: Remembers your org, last-used filters, and common patterns
- **Cross-resource intelligence**: `jc users show jdoe` automatically resolves username → ID and shows related groups, devices, and recent auth events

#### LLM-Friendly

- **Structured output by default**: JSON output, with `--human` or `--table` for human-readable formatting
- **Self-describing schema**: `jc schema <resource>` returns the full schema for any resource
- **Predictable grammar**: Consistent `resource verb` pattern across all commands
- **Machine-readable errors**: Errors include structured JSON with error codes, not just strings
- **Discoverability endpoint**: `jc commands list` returns all available commands with their arguments
- **Idempotent where possible**: Running the same command twice produces the same result

### 6.2 Output Modes

| Mode | Flag | When to Use | Example |
|------|------|-------------|---------|
| JSON (default) | `--output json` or none | Piping, scripting, LLM consumption | `{"id":"5f...","username":"jdoe"}` |
| Table | `--output table` or `-t` | Quick human scanning | Formatted ASCII table |
| Human | `--output human` or `-h` | Detailed single-resource view | Key-value pairs with labels |
| CSV | `--output csv` | Spreadsheet export, reporting | Standard CSV with headers |
| YAML | `--output yaml` | Config files, IaC integration | YAML document |
| Quiet | `--quiet` or `-q` | Scripts needing only exit codes | No output, exit code only |
| IDs-only | `--ids` | Piping to other `jc` commands | One ID per line |

---

## 7. Technical Architecture

### 7.1 Language Choice: Go

| Criterion | Go | Rust | Python |
|-----------|-----|------|--------|
| Compile to single binary | ✅ | ✅ | ❌ (runtime needed) |
| Cross-compile macOS/Linux | ✅ Trivial | ✅ Good | N/A |
| IT admin familiarity | ✅ High (Terraform, k8s) | ⚠️ Low | ✅ High |
| CLI ecosystem | ✅ Cobra, Viper, excellent | ✅ Clap, good | ⚠️ Click, adequate |
| Contribution barrier | ✅ Low | ⚠️ High (borrow checker) | ✅ Low |
| Build speed | ✅ Fast | ⚠️ Slow | N/A |
| LLM code generation quality | ✅ Excellent | ✅ Good | ✅ Excellent |

**Recommendation: Go** — The IT admin / DevOps audience is deeply familiar with Go (Terraform, Kubernetes, Docker, Packer). The contribution barrier is low, the CLI ecosystem (Cobra + Viper) is best-in-class, and cross-compilation is trivial. Compiled binaries mean zero runtime dependencies.

### 7.2 Core Dependencies

| Dependency | Purpose | Version |
|-----------|---------|---------|
| `cobra` | Command framework, shell completions, help generation | Latest |
| `viper` | Configuration management (config files, env vars, flags) | Latest |
| `go-resty` or `net/http` | HTTP client for JumpCloud API calls | Latest |
| `lipgloss` / `glamour` | Terminal styling for human-readable output | Latest |
| `tablewriter` | ASCII table output | Latest |
| `survey` / `huh` | Interactive prompts (confirmations, selections) | Latest |
| `zerolog` | Structured logging | Latest |

### 7.3 Project Structure

```
jc/
├── cmd/                    # Cobra command definitions
│   ├── root.go             # Root command, global flags
│   ├── users/              # jc users [verb]
│   ├── devices/            # jc devices [verb]  (alias: systems)
│   ├── groups/             # jc groups [verb]
│   ├── commands/           # jc commands [verb]
│   ├── policies/           # jc policies [verb]
│   ├── apps/               # jc apps [verb]
│   ├── insights/           # jc insights [verb]
│   ├── auth/               # jc auth [verb]  (authentication flows)
│   ├── recipe/             # jc recipe [verb]
│   ├── ask/                # jc ask "..."  (conversational mode)
│   ├── mcp/                # jc mcp serve  (MCP server)
│   ├── schema/             # jc schema [resource]
│   └── config/             # jc config [verb]
├── internal/
│   ├── api/                # JumpCloud API client
│   │   ├── v1/             # V1 API wrapper
│   │   ├── v2/             # V2 API wrapper
│   │   └── insights/       # Directory Insights API wrapper
│   ├── auth/               # Authentication (API key, OAuth, keychain)
│   ├── config/             # Config file management
│   ├── output/             # Output formatters (JSON, table, CSV, YAML, human)
│   ├── plan/               # Plan/dry-run engine
│   ├── recipe/             # Recipe engine (parse, validate, execute)
│   ├── resolve/            # Name-to-ID resolution and caching
│   ├── ask/                # Conversational mode / LLM integration
│   ├── mcp/                # MCP server (tools, resources, prompts, transports)
│   └── errors/             # Structured error types
├── recipes/                # Built-in recipe library
│   ├── onboard-user.yaml
│   ├── offboard-user.yaml
│   ├── rotate-api-key.yaml
│   ├── audit-inactive-users.yaml
│   ├── compliance-report.yaml
│   └── ...
├── schema/                 # API schema definitions (generated from OpenAPI)
├── completions/            # Shell completion scripts
├── docs/                   # Documentation
│   ├── commands/           # Auto-generated command reference
│   └── recipes/            # Recipe documentation
├── scripts/                # Build, release, code generation scripts
├── Makefile
├── go.mod
├── go.sum
├── LICENSE                 # MIT
└── README.md
```

### 7.4 API Client Architecture

The API client is a layered wrapper around JumpCloud's three API surfaces:

```
┌─────────────────────────────────────────────────┐
│                   CLI Layer                       │
│        Cobra commands → business logic            │
├─────────────────────────────────────────────────┤
│               Resolution Layer                    │
│    Name → ID resolution, caching, validation      │
├─────────────────────────────────────────────────┤
│              API Client Layer                     │
│   Pagination, rate limiting, retry, error mapping │
├────────────────┬───────────────┬────────────────┤
│    V1 Client   │  V2 Client    │  DI Client     │
│  /api/*        │  /api/v2/*    │  /insights/*   │
├────────────────┴───────────────┴────────────────┤
│              HTTP Transport Layer                 │
│      Auth injection, logging, tracing             │
└─────────────────────────────────────────────────┘
```

**Key design decisions:**

- **Name-to-ID resolution** is automatic: `jc users get jdoe` resolves `jdoe` to the user ID transparently. Resolution results are cached in-memory for the session and optionally on disk.
- **Pagination is handled automatically**: List commands iterate through all pages by default. `--limit N` stops after N results. `--page-size` controls the API page size.
- **Rate limiting** respects JumpCloud's rate limits with exponential backoff and jitter.
- **The V1/V2 boundary is hidden from the user**: Commands route to the correct API version internally. Users never need to know which API version a command uses.

---

## 8. Authentication & Security

### 8.1 Authentication Methods

Authentication is implemented incrementally: API key support ships in Phase 1; OAuth 2.0 Service Account support is added in Phase 2.

| Method | Use Case | Storage | Phase |
|--------|----------|---------|-------|
| **API Key** (primary) | Interactive use, scripts | macOS Keychain / Linux secret-tool (freedesktop.org Secret Service) | 1 |
| **Environment Variable** | Quick scripting, CI/CD | `JC_API_KEY` env var | 1 |
| **Config File** | Multi-org / profile management | `~/.config/jc/config.yaml` (key references keychain, not stored in plaintext) | 1 |
| **Service Account (OAuth 2.0)** | CI/CD, automation, scoped access | Client ID + Client Secret → Bearer token exchange | 2 |

### 8.2 Authentication Flow

```
jc auth login
  → Prompts for API key (masked input)
  → Validates key against JumpCloud API (GET /api/organizations)
  → Stores key in OS keychain (macOS Keychain / Linux secret-tool)
  → Saves org metadata to config file
  → Sets as active profile

jc auth login --service-account
  → Prompts for Client ID and Client Secret
  → Exchanges credentials for Bearer token via OAuth 2.0 token endpoint
  → Caches token with TTL
  → Auto-refreshes on expiry

jc auth status
  → Shows current authentication state, active org, key validity

jc auth switch <profile>
  → Switches active org/profile for multi-org management
```

### 8.3 Security Requirements

| Requirement | Implementation |
|-------------|---------------|
| No plaintext API keys on disk | Keys stored in OS keychain; config file stores references only |
| Credential masking in logs | API keys redacted in `--verbose` / debug output |
| Minimal permission principle | Document and encourage use of scoped Service Accounts |
| Secure transport only | HTTPS enforced; certificate pinning optional via config |
| Config file permissions | `0600` on config file; warn if permissions are too open |
| Session timeout | Configurable auto-lock after inactivity (default: none) |

### 8.4 Multi-Org / MSP Support

```yaml
# ~/.config/jc/config.yaml
active_profile: acme-corp

profiles:
  acme-corp:
    org_id: "5f4d..."
    api_key_ref: "keychain://jc/acme-corp"    # macOS Keychain reference
    default_output: table

  client-alpha:
    org_id: "6a3b..."
    auth_method: service_account
    client_id: "sa_68d1..."
    client_secret_ref: "keychain://jc/client-alpha-secret"

  client-beta:
    org_id: "7c2e..."
    api_key_ref: "keychain://jc/client-beta"
```

Global org override on any command: `jc --org client-alpha users list`

---

## 9. API Coverage

### 9.1 JumpCloud API v1

The v1 API manages core directory objects. All endpoints will be exposed as CLI commands.

| Resource | Endpoints | CLI Command Group |
|----------|-----------|-------------------|
| System Users | CRUD, search, lock/unlock, MFA reset, SSH keys | `jc users` |
| Systems (Devices) | List, get, delete, command execution | `jc devices` |
| Commands | CRUD, trigger, results, file upload | `jc commands` |
| Command Results | List, get by command | `jc commands results` |
| RADIUS Servers | CRUD | `jc radius` |
| Organizations | Get, list (MSP) | `jc orgs` |
| Search | Users, systems (POST-based search) | Integrated into `list --search` |
| Applications (SSO) | List, get | `jc apps` |

### 9.2 JumpCloud API v2

The v2 API manages the Graph (associations), groups, policies, and newer objects.

| Resource | Endpoints | CLI Command Group |
|----------|-----------|-------------------|
| User Groups | CRUD, membership, associations | `jc groups user` |
| System Groups | CRUD, membership, associations | `jc groups device` |
| Policies | List, get, create, update, delete | `jc policies` |
| Policy Results | List by policy | `jc policies results` |
| Active Directory | Instances, agents | `jc ad` |
| LDAP | Servers, configuration | `jc ldap` |
| G Suite / Google Workspace | Directory integration | `jc directories google` |
| Office 365 / M365 | Directory integration | `jc directories m365` |
| Software Apps | CRUD, associations | `jc apps software` |
| System Insights | Tables, queries | `jc insights system` |
| Custom Email Templates | CRUD | `jc email-templates` |
| IP Lists | CRUD | `jc ip-lists` |
| Authentication Policies | CRUD (Conditional Access) | `jc auth-policies` |
| Administrators | List, CRUD | `jc admins` |
| Bulk Operations (v2) | Users, job status | `jc bulk` |
| Graph Associations | Traverse, list, manage | `jc graph` |

### 9.3 Directory Insights API

| Feature | Endpoint | CLI Command |
|---------|----------|-------------|
| Query Events | `POST /insights/directory/v1/events` | `jc insights query` |
| Event Count | `POST /insights/directory/v1/events/count` | `jc insights count` |
| Event Distinct | `POST /insights/directory/v1/events/distinct` | `jc insights distinct` |

**Service categories supported:**

| Category | Services |
|----------|----------|
| Authentication | `sso`, `radius`, `ldap`, `user_portal`, `admin`, `mdm` |
| Directory | `user`, `group`, `system`, `application`, `policy`, `command` |
| Integrations | `activedirectory`, `gsuite`, `o365`, `workday`, `scim` |
| Security | `password`, `mfa`, `lockout`, `admin_login` |
| All | `all` (catch-all) |

---

## 10. Command Grammar & Structure

### 10.1 Grammar Pattern

```
jc <resource> <verb> [positional-args] [flags]
```

**Resources** are plural nouns: `users`, `devices`, `groups`, `commands`, `policies`, `apps`, `insights`

**Verbs** are consistent across all resources:

| Verb | Action | Example |
|------|--------|---------|
| `list` | List resources (with filtering and pagination) | `jc users list` |
| `get` | Get a single resource by ID or name | `jc users get jdoe` |
| `create` | Create a new resource | `jc users create --username jdoe --email j@co.com` |
| `update` | Update an existing resource | `jc users update jdoe --department "Engineering"` |
| `delete` | Delete a resource (with confirmation) | `jc users delete jdoe` |
| `search` | Full-text search across resources | `jc users search "john"` |

**Resource-specific verbs** extend the base set:

| Command | Action |
|---------|--------|
| `jc users lock <user>` | Lock a user account |
| `jc users unlock <user>` | Unlock a user account |
| `jc users reset-mfa <user>` | Reset a user's MFA enrollment |
| `jc devices lock <device>` | Send MDM lock command |
| `jc devices restart <device>` | Send MDM restart command |
| `jc devices erase <device>` | Send MDM erase command (requires `--confirm-erase`) |
| `jc groups add-member <group> <user/device>` | Add a member to a group |
| `jc groups remove-member <group> <user/device>` | Remove a member from a group |
| `jc commands run <command> --on <device/group>` | Trigger a command |
| `jc commands results <command>` | Get command results |

### 10.2 Global Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--output <format>` | `-o` | Output format: `json` (default), `table`, `human`, `csv`, `yaml` |
| `--quiet` | `-q` | Suppress output; exit code only |
| `--ids` | | Output IDs only (one per line) |
| `--plan` | | Show what would happen without executing |
| `--force` | `-f` | Skip confirmation prompts |
| `--non-interactive` | | Disable all interactive prompts (for scripts) |
| `--verbose` | `-v` | Show HTTP requests/responses (redacted) |
| `--debug` | | Full debug logging |
| `--org <profile>` | | Override active org/profile |
| `--api-key <key>` | | Override API key (use env var `JC_API_KEY` preferably) |
| `--fields <f1,f2,...>` | | Select specific output fields |
| `--filter <expr>` | | Filter results (see Filtering section) |
| `--limit <n>` | | Maximum results to return |
| `--sort <field>` | | Sort results by field (`-field` for descending) |
| `--no-cache` | | Bypass name-to-ID cache |
| `--version` | `-V` | Print version and exit |
| `--help` | | Show help |

### 10.3 Filtering

Filters use a simple, readable syntax inspired by JumpCloud's own filter format:

```bash
# Equality
jc users list --filter "department=Engineering"

# Comparison
jc users list --filter "created>=2025-01-01"

# Multiple conditions (AND)
jc users list --filter "department=Engineering" --filter "activated=true"

# Search text (substring match)
jc users list --search "john"

# JMESPath post-processing (for complex queries)
jc users list --query "[?department=='Engineering'].{name:username,email:email}"
```

### 10.4 Aliases and Shortcuts

Convenience aliases for common patterns:

| Alias | Expands To |
|-------|-----------|
| `jc u` | `jc users` |
| `jc d` | `jc devices` |
| `jc g` | `jc groups` |
| `jc i` | `jc insights` |
| `jc users ls` | `jc users list` |
| `jc users rm` | `jc users delete` |
| `jc devices ls` | `jc devices list` |

---

## 11. Core Features — Output & Formatting

### 11.1 JSON Output (Default)

All commands output valid JSON by default. List commands output JSON arrays; single-resource commands output JSON objects.

```bash
$ jc users get jdoe
{
  "id": "5f4d7a8b9c...",
  "username": "jdoe",
  "firstname": "John",
  "lastname": "Doe",
  "email": "jdoe@acme.com",
  "department": "Engineering",
  "activated": true,
  "mfa": { "configured": true, "exclusion": false },
  "created": "2024-03-15T10:30:00Z",
  "last_login": "2026-02-12T14:22:00Z"
}
```

### 11.2 Table Output

```bash
$ jc users list -t --fields username,email,department,activated
USERNAME   EMAIL              DEPARTMENT     ACTIVATED
jdoe       jdoe@acme.com      Engineering    true
asmith     asmith@acme.com    Marketing      true
bwilson    bwilson@acme.com   Engineering    false
── 3 of 142 users (use --limit to show more) ──
```

### 11.3 Field Selection

```bash
# Select specific fields
$ jc users get jdoe --fields username,email,department

# Exclude fields
$ jc users get jdoe --exclude password_date,totp_enabled

# All fields (including nested)
$ jc users get jdoe --all
```

### 11.4 IDs-Only Mode

```bash
# For piping into other commands
$ jc users list --filter "department=Engineering" --ids
5f4d7a8b9c...
6a3bef12d4...
7c2e8f45a6...

# Compose with other commands
$ jc users list --filter "department=Engineering" --ids | xargs -I{} jc groups add-member eng-group {}
```

---

## 12. Core Features — Plan Mode

Plan mode is a first-class feature that allows any mutating command to preview its effects without executing. This is critical for LLM-generated commands and for admin confidence.

### 12.1 Usage

```bash
$ jc users delete jdoe --plan

╭─ Plan: Delete User ─────────────────────────────╮
│                                                   │
│  Action:   DELETE user                            │
│  Target:   jdoe (5f4d7a8b9c...)                  │
│  Name:     John Doe <jdoe@acme.com>              │
│                                                   │
│  Effects:                                         │
│  ✗ User account will be permanently deleted       │
│  ✗ Removed from 3 groups:                        │
│    - Engineering (user group)                     │
│    - macOS Devices (system group)                 │
│    - Okta SSO (application)                       │
│  ✗ Unbound from 1 device:                        │
│    - JDOE-MBP (MacBook Pro, macOS 15.3)          │
│                                                   │
│  This action cannot be undone.                    │
╰──────────────────────────────────────────────────╯

$ jc users update jdoe --department "Sales" --plan

╭─ Plan: Update User ─────────────────────────────╮
│                                                   │
│  Action:   UPDATE user                            │
│  Target:   jdoe (5f4d7a8b9c...)                  │
│                                                   │
│  Changes:                                         │
│  ~ department: "Engineering" → "Sales"            │
│                                                   │
│  No side effects.                                 │
╰──────────────────────────────────────────────────╯
```

### 12.2 Plan as JSON

For LLM consumption, plan output can be structured:

```bash
$ jc users delete jdoe --plan --output json
{
  "action": "delete",
  "resource": "user",
  "target": { "id": "5f4d...", "username": "jdoe", "name": "John Doe" },
  "effects": [
    { "type": "remove_membership", "group": "Engineering", "group_type": "user_group" },
    { "type": "remove_membership", "group": "macOS Devices", "group_type": "system_group" },
    { "type": "remove_binding", "device": "JDOE-MBP", "device_id": "8a1b..." }
  ],
  "reversible": false
}
```

### 12.3 Plan for Recipes

```bash
$ jc recipe run offboard-user --user jdoe --plan

╭─ Plan: Recipe "offboard-user" ───────────────────╮
│                                                    │
│  Step 1: Lock user account                         │
│    jc users lock jdoe                              │
│                                                    │
│  Step 2: Remove from all groups (3 groups)         │
│    jc groups remove-member Engineering jdoe        │
│    jc groups remove-member "macOS Devices" jdoe    │
│    jc groups remove-member "Okta SSO" jdoe         │
│                                                    │
│  Step 3: Unbind from all devices (1 device)        │
│    jc devices unbind JDOE-MBP --user jdoe          │
│                                                    │
│  Step 4: Reset MFA                                 │
│    jc users reset-mfa jdoe                         │
│                                                    │
│  Total: 6 API calls                                │
╰────────────────────────────────────────────────────╯
```

---

## 13. Core Features — Conversational Mode

### 13.1 Overview

`jc ask` provides a natural language interface to the JumpCloud CLI. Users describe what they want in plain English, and the CLI translates it into the appropriate command(s), shows the plan, and executes upon confirmation.

### 13.2 Architecture

Conversational mode integrates with an LLM (configurable: Anthropic Claude, OpenAI, local Ollama) to translate natural language into `jc` commands. The LLM receives:

1. The full `jc` command schema (auto-generated)
2. The user's natural language query
3. Context about the current org (org name, user count, etc.)

The LLM returns structured command(s), which are validated locally before execution.

```
User Input → LLM (with schema context) → Proposed Commands → Plan Mode → User Confirmation → Execution
```

### 13.3 Usage Examples

```bash
$ jc ask "which users haven't logged in for 90 days?"

Translating...
→ jc users list --filter "last_login<=2025-11-15" --fields username,email,last_login -t

Execute this command? [Y/n]: y

USERNAME    EMAIL                 LAST_LOGIN
olduser1    old1@acme.com         2025-08-03T...
olduser2    old2@acme.com         2025-09-14T...
── 2 users found ──
```

```bash
$ jc ask "offboard Sarah Chen - remove all access but keep the account"

Translating...
→ Recipe: offboard-user (partial)
  1. jc users lock schen
  2. jc groups remove-member --all --user schen
  3. jc users reset-mfa schen

Show full plan? [Y/n]: y
[displays plan mode output]
Execute? [Y/n]:
```

### 13.4 Configuration

```yaml
# ~/.config/jc/config.yaml
ask:
  provider: anthropic       # anthropic | openai | ollama | disabled
  model: claude-sonnet-4-20250514
  api_key_ref: "keychain://jc/anthropic-key"
  # For Ollama (local, no API key needed):
  # provider: ollama
  # model: llama3.2
  # endpoint: http://localhost:11434
  confirm_before_execute: true    # Always show plan first
  max_commands_per_query: 10      # Safety limit
```

### 13.5 Safety Guarantees

- Conversational mode **always** shows the translated commands before execution
- Destructive operations require explicit confirmation (even with `--force`)
- A configurable maximum command limit prevents runaway operations
- The LLM never has direct API access — it only generates command strings that are parsed and validated by the CLI
- All commands generated by `jc ask` are logged to `~/.config/jc/ask-history.log`

---

## 14. Core Features — Recipes

### 14.1 Overview

Recipes are composable, declarative workflow templates for common multi-step operations. They are inspired by the JumpCloud PowerShell Module's Commands Gallery and adapted for a YAML-based, Unix-native experience.

### 14.2 Recipe Format

```yaml
# recipes/offboard-user.yaml
name: offboard-user
description: "Complete user offboarding: lock account, remove group memberships, unbind devices, reset MFA"
author: Klaassen Consulting
version: "1.0"
tags: [lifecycle, offboarding, security]

parameters:
  - name: user
    description: "Username or user ID to offboard"
    required: true
    type: string
  - name: keep_account
    description: "Keep the user account (locked) instead of deleting"
    type: bool
    default: true
  - name: notify
    description: "Email address to notify upon completion"
    type: string
    required: false

steps:
  - name: lock-account
    description: "Lock the user account to prevent further access"
    command: "jc users lock {{ .user }}"
    
  - name: remove-groups
    description: "Remove user from all group memberships"
    command: "jc groups remove-member --all --user {{ .user }}"
    
  - name: unbind-devices
    description: "Unbind user from all associated devices"
    command: "jc devices unbind --all --user {{ .user }}"
    
  - name: reset-mfa
    description: "Reset MFA enrollment"
    command: "jc users reset-mfa {{ .user }}"
    
  - name: delete-account
    description: "Delete the user account (if not keeping)"
    command: "jc users delete {{ .user }} --force"
    when: "{{ not .keep_account }}"

  - name: verify
    description: "Verify offboarding completed"
    command: "jc users get {{ .user }} --fields username,account_locked,suspended"
    capture: result
    
on_success:
  message: "User {{ .user }} has been offboarded successfully."
  
on_failure:
  message: "Offboarding failed at step '{{ .failed_step }}'. Manual intervention may be required."
  rollback: false    # Offboarding should not auto-rollback
```

### 14.3 Recipe Commands

| Command | Description |
|---------|-------------|
| `jc recipe list` | List all available recipes (built-in + user) |
| `jc recipe show <name>` | Display recipe details, parameters, and steps |
| `jc recipe run <name> [--param value]` | Execute a recipe |
| `jc recipe run <name> --plan` | Preview recipe execution plan |
| `jc recipe validate <file>` | Validate a recipe YAML file |
| `jc recipe create` | Interactive recipe builder |
| `jc recipe import <url/path>` | Import a recipe from URL or file |
| `jc recipe export <name>` | Export a recipe as YAML |

### 14.4 Built-In Recipe Library

| Recipe | Description |
|--------|-------------|
| `onboard-user` | Create user, add to groups, bind to device, send welcome email |
| `offboard-user` | Lock/delete user, remove memberships, unbind devices, reset MFA |
| `rotate-api-key` | Generate new API key and update stored credentials |
| `audit-inactive-users` | Find users inactive for N days, export report |
| `audit-unmanaged-devices` | Find devices not in any group |
| `compliance-report` | Generate compliance snapshot (users, devices, policies, MFA status) |
| `bulk-create-users` | Create users from CSV file |
| `mfa-enforcement-check` | List users without MFA configured |
| `stale-device-cleanup` | Find and optionally remove devices not seen in N days |
| `group-sync` | Sync group membership from external source (CSV, LDAP export) |
| `password-expiry-report` | List users with passwords expiring in N days |

### 14.5 User Recipe Location

```
~/.config/jc/recipes/          # User-defined recipes
/usr/local/share/jc/recipes/   # System-wide shared recipes
```

---

## 15. Core Features — Pipe & Compose

### 15.1 Unix Pipeline Integration

`jc` is designed to be a first-class Unix citizen. Every command can participate in pipelines.

```bash
# Find inactive users and remove them from a group
jc users list --filter "last_login<=2025-11-15" --ids | \
  xargs -I{} jc groups remove-member contractors {}

# Export all Engineering users to CSV
jc users list --filter "department=Engineering" -o csv > eng-users.csv

# Count devices by OS
jc devices list --fields os -o json | jq 'group_by(.os) | map({os: .[0].os, count: length})'

# Find users without MFA and lock them
jc users list --filter "mfa.configured=false" --filter "activated=true" --ids | \
  xargs -I{} jc users lock {}

# Pipe insights to jq for analysis
jc insights query --service sso --start "2026-02-01" --end "2026-02-13" -o json | \
  jq '[.[] | select(.event_type == "sso_auth_failed")] | length'
```

### 15.2 Stdin Support

Commands accept input from stdin for batch operations:

```bash
# Bulk delete from a file of usernames
cat users-to-delete.txt | jc users delete --stdin --force

# Pipe user IDs between commands
jc users list --filter "suspended=true" --ids | jc users delete --stdin

# Read recipe parameters from a JSON file
echo '{"user": "jdoe", "keep_account": false}' | jc recipe run offboard-user --params-stdin
```

### 15.3 jq-Style Built-In Query

For users without `jq` installed, a lightweight built-in query (JMESPath) is available:

```bash
jc users list --query "[?department=='Engineering'].{name:username, email:email}"
```

---

## 16. Core Features — Explain & Help

### 16.1 Contextual Help

Every command has comprehensive `--help` that serves dual purposes: human-readable documentation and LLM-parseable context.

```bash
$ jc users --help

Manage JumpCloud users (system users).

Usage:
  jc users <command> [flags]

Available Commands:
  list          List users with optional filtering
  get           Get a single user by username or ID
  create        Create a new user
  update        Update an existing user
  delete        Delete a user
  search        Search users by text
  lock          Lock a user account
  unlock        Unlock a user account
  reset-mfa     Reset a user's MFA enrollment
  reset-password Trigger a password reset email

Flags:
  -h, --help    help for users

Use "jc users <command> --help" for more information about a command.
```

### 16.2 Explain Mode

`jc explain` describes what a command does, what API calls it makes, and what side effects to expect — without executing anything.

```bash
$ jc explain "users delete jdoe"

Command: jc users delete jdoe

What it does:
  Permanently deletes the user account for 'jdoe' from your JumpCloud org.

API calls:
  1. GET  /api/systemusers?filter=username:$eq:jdoe    (resolve username → ID)
  2. GET  /api/v2/users/{id}/memberof                  (check group memberships)
  3. GET  /api/v2/users/{id}/systems                   (check device bindings)
  4. DELETE /api/systemusers/{id}                       (delete user)

Side effects:
  - User is removed from all groups automatically by JumpCloud
  - User is unbound from all devices automatically
  - User's SSH keys are deleted
  - This action is irreversible

Related commands:
  jc users lock jdoe         # Lock instead of delete (reversible)
  jc users delete jdoe --plan  # Preview effects first
```

### 16.3 Examples in Help

Every command includes practical examples:

```bash
$ jc users list --help

# ... standard help ...

Examples:
  # List all active users
  jc users list --filter "activated=true"

  # List Engineering users as a table
  jc users list --filter "department=Engineering" -t

  # Find users created in the last 30 days
  jc users list --filter "created>=2026-01-14" --sort "-created"

  # Export all users to CSV
  jc users list --all -o csv > all-users.csv

  # Get just usernames
  jc users list --fields username --quiet
```

---

## 17. Core Features — Directory Insights

### 17.1 Query Interface

```bash
# Basic event query
jc insights query --service sso --start "2026-02-01"

# Multiple services
jc insights query --service sso,ldap,radius --start "2026-02-01" --end "2026-02-13"

# Shorthand for common time ranges
jc insights query --service all --last 24h
jc insights query --service admin --last 7d
jc insights query --service user --last 30d

# Filter by event type
jc insights query --service sso --event-type sso_auth_failed --last 24h

# Count events
jc insights count --service sso --last 7d

# Distinct values
jc insights distinct --service sso --field initiated_by.username --last 30d
```

### 17.2 Saved Searches

```bash
# Save a search
jc insights save "failed-sso-24h" --service sso --event-type sso_auth_failed --last 24h

# Run a saved search
jc insights run "failed-sso-24h"

# List saved searches
jc insights saved
```

### 17.3 Output for SIEM Integration

```bash
# Export events as NDJSON (newline-delimited JSON) for SIEM ingestion
jc insights query --service all --last 24h -o ndjson > events.ndjson

# Export with specific fields for Splunk
jc insights query --service sso --last 7d --fields timestamp,event_type,initiated_by,client_ip -o csv
```

---

## 18. MCP Server

### 18.1 Overview

`jc` ships with a built-in Model Context Protocol (MCP) server, enabling AI assistants like Claude Desktop, Claude Code, and other MCP-compatible clients to interact with JumpCloud directly. The MCP server wraps the same command engine that powers the CLI — meaning every command, recipe, and plan mode feature is available to AI agents out of the box.

This is a core differentiator. No other JumpCloud tool offers native AI agent integration. By shipping MCP alongside the CLI, `jc` becomes the bridge between natural language IT operations and the JumpCloud API.

### 18.2 Architecture

```
┌──────────────────────────────────────────────┐
│         MCP Client (Claude Desktop,          │
│         Claude Code, Cursor, etc.)           │
└──────────────┬───────────────────────────────┘
               │ MCP Protocol (stdio / SSE)
┌──────────────▼───────────────────────────────┐
│            jc mcp serve                       │
│                                               │
│  ┌─────────────┐  ┌───────────────────────┐  │
│  │   Tools      │  │   Resources           │  │
│  │              │  │                       │  │
│  │  users.*     │  │  jc://schema/users    │  │
│  │  devices.*   │  │  jc://schema/devices  │  │
│  │  groups.*    │  │  jc://org/info        │  │
│  │  insights.*  │  │  jc://recipes/list    │  │
│  │  recipe.*    │  │                       │  │
│  │  plan.*      │  │                       │  │
│  └─────────────┘  └───────────────────────┘  │
│                                               │
│  ┌──────────────────────────────────────┐    │
│  │     Same API client, auth, and       │    │
│  │     resolution engine as CLI         │    │
│  └──────────────────────────────────────┘    │
└──────────────────────────────────────────────┘
```

The MCP server runs as `jc mcp serve` and communicates via stdio (for Claude Desktop / Claude Code) or HTTP+SSE (for remote / multi-client setups). It reuses the CLI's authentication, API client, caching, and output layers — there is no separate codebase.

### 18.3 MCP Tools

Each CLI command group is exposed as a set of MCP tools. Tools always return structured JSON and support the `--plan` flag natively.

| Tool | Description | Parameters |
|------|-------------|------------|
| `users_list` | List users with filtering | `filter`, `fields`, `limit`, `sort` |
| `users_get` | Get a user by username or ID | `identifier` |
| `users_create` | Create a new user | `username`, `email`, `firstname`, `lastname`, ... |
| `users_update` | Update a user | `identifier`, fields to update |
| `users_delete` | Delete a user | `identifier`, `plan` (bool) |
| `users_lock` | Lock a user account | `identifier` |
| `users_unlock` | Unlock a user account | `identifier` |
| `users_reset_mfa` | Reset MFA enrollment | `identifier` |
| `devices_list` | List devices | `filter`, `fields`, `limit`, `sort` |
| `devices_get` | Get a device | `identifier` |
| `devices_lock` | MDM lock | `identifier`, `plan` (bool) |
| `groups_list` | List groups | `type` (user/device), `filter` |
| `groups_add_member` | Add member to group | `group`, `member` |
| `groups_remove_member` | Remove member from group | `group`, `member` |
| `insights_query` | Query Directory Insights | `service`, `start`, `end`, `event_type`, `limit` |
| `insights_count` | Count events | `service`, `start`, `end` |
| `commands_list` | List commands | `filter` |
| `commands_run` | Trigger a command | `command`, `target`, `plan` (bool) |
| `policies_list` | List policies | `filter` |
| `recipe_run` | Execute a recipe | `name`, `parameters`, `plan` (bool) |
| `plan` | Preview any command without executing | `command` (full command string) |
| `explain` | Explain what a command does | `command` |

### 18.4 MCP Resources

Resources provide read-only context that the AI agent can reference to make better decisions.

| Resource URI | Description |
|-------------|-------------|
| `jc://schema/users` | User resource schema (fields, types, filters) |
| `jc://schema/devices` | Device resource schema |
| `jc://schema/groups` | Group resource schema |
| `jc://schema/commands` | Full CLI command manifest |
| `jc://schema/{resource}` | Schema for any resource |
| `jc://org/info` | Current org metadata (name, user count, device count) |
| `jc://org/stats` | Quick org statistics (active users, MFA adoption, device breakdown) |
| `jc://recipes/list` | Available recipes with descriptions and parameters |
| `jc://recipes/{name}` | Full recipe definition |
| `jc://config/profiles` | Available org profiles (names only, no secrets) |

### 18.5 MCP Prompts

Pre-built prompts guide the AI agent toward effective JumpCloud operations.

| Prompt | Description |
|--------|-------------|
| `onboard_user` | Guided onboarding flow: collects user details, suggests groups, creates user |
| `offboard_user` | Guided offboarding: confirms scope, runs offboard recipe with plan preview |
| `security_audit` | Reviews MFA adoption, inactive users, stale devices, recent auth failures |
| `find_user_info` | Deep user lookup: profile, groups, devices, recent auth events |
| `troubleshoot_auth` | Diagnoses authentication issues for a specific user |
| `compliance_check` | Runs compliance-oriented queries across users, devices, and policies |

### 18.6 Safety Model

The MCP server inherits the CLI's safety guarantees, plus additional protections for autonomous operation:

| Safeguard | Implementation |
|-----------|---------------|
| **Plan-first for destructive ops** | All mutating tools (`delete`, `lock`, `erase`, `remove-member`) return a plan by default; the agent must call the tool again with `execute: true` to proceed |
| **Confirmation annotations** | Destructive tools are annotated with MCP's `destructiveHint` so clients can show confirmation UIs |
| **Rate limiting** | MCP server enforces its own rate limit (configurable, default 60 tool calls/minute) to prevent runaway agents |
| **Audit log** | All MCP tool calls are logged to `~/.config/jc/mcp-audit.log` with timestamps, tool names, parameters, and results |
| **Scoped auth** | MCP server can be configured with a restricted API key or Service Account with limited permissions |
| **Read-only mode** | `jc mcp serve --read-only` disables all mutating tools |

### 18.7 Configuration

```yaml
# ~/.config/jc/config.yaml
mcp:
  transport: stdio              # stdio | sse
  sse_port: 8080                # Only used with sse transport
  rate_limit: 60                # Max tool calls per minute
  read_only: false              # Disable mutating tools
  audit_log: true               # Log all tool calls
  plan_first: true              # Require plan before execution on destructive ops
  allowed_tools:                # Whitelist specific tools (empty = all)
    - users_*
    - devices_list
    - devices_get
    - insights_*
    - recipe_run
  blocked_tools:                # Blacklist specific tools
    - devices_erase
```

### 18.8 Claude Desktop Integration

```json
// ~/Library/Application Support/Claude/claude_desktop_config.json
{
  "mcpServers": {
    "jumpcloud": {
      "command": "jc",
      "args": ["mcp", "serve"],
      "env": {
        "JC_PROFILE": "acme-corp"
      }
    }
  }
}
```

### 18.9 Claude Code Integration

```bash
# Add as MCP server in Claude Code
claude mcp add jumpcloud -- jc mcp serve

# Or with specific profile
claude mcp add jumpcloud -- jc mcp serve --org client-alpha

# Verify
claude mcp list
```

Once configured, Claude Code can execute JumpCloud operations naturally:

```
> Which users in the Engineering department haven't logged in this month?

I'll check that for you using JumpCloud.

[calls users_list with filter: department=Engineering, last_login<=2026-02-01]

Found 3 inactive Engineering users:
- schen (Sarah Chen) — last login Jan 15
- mpark (Michael Park) — last login Jan 8  
- rjones (Rachel Jones) — last login Dec 29

Would you like me to send them a reminder, or flag them for review?
```

---

## 19. LLM-Friendliness Specification

This section defines the contract that makes `jc` a first-class tool for LLM agents.

### 18.1 Schema Discovery

```bash
# List all available resources
$ jc schema resources
["users", "devices", "groups", "commands", "policies", "apps", "insights", ...]

# Get schema for a resource
$ jc schema users
{
  "resource": "users",
  "api_versions": ["v1"],
  "verbs": ["list", "get", "create", "update", "delete", "search", "lock", "unlock", "reset-mfa", "reset-password"],
  "fields": {
    "id": { "type": "string", "description": "Unique identifier", "read_only": true },
    "username": { "type": "string", "description": "Login username", "required_on_create": true },
    "email": { "type": "string", "description": "Email address", "required_on_create": true },
    "firstname": { "type": "string", "description": "First name" },
    "lastname": { "type": "string", "description": "Last name" },
    "department": { "type": "string", "description": "Department name" },
    "activated": { "type": "boolean", "description": "Whether the user has activated their account", "read_only": true },
    "account_locked": { "type": "boolean", "description": "Whether the account is locked" },
    ...
  },
  "filters": ["username", "email", "department", "activated", "created", "last_login", ...],
  "sort_fields": ["username", "email", "created", "last_login"]
}

# Get full CLI command manifest (all commands, all args)
$ jc schema commands
{
  "commands": [
    {
      "path": "users list",
      "description": "List JumpCloud users with optional filtering",
      "flags": [
        { "name": "--filter", "type": "string", "repeatable": true, "description": "Filter expression (field=value)" },
        { "name": "--search", "type": "string", "description": "Full-text search" },
        { "name": "--limit", "type": "int", "default": 100, "description": "Maximum results" },
        ...
      ],
      "examples": [
        "jc users list --filter department=Engineering",
        "jc users list --filter activated=false --limit 10"
      ]
    },
    ...
  ]
}
```

### 18.2 Machine-Readable Errors

```bash
$ jc users get nonexistent-user
{
  "error": {
    "code": "USER_NOT_FOUND",
    "message": "No user found with username or ID 'nonexistent-user'",
    "suggestion": "Use 'jc users search nonexistent' to find similar usernames",
    "http_status": 404,
    "api_endpoint": "GET /api/systemusers"
  }
}
# Exit code: 1
```

### 18.3 Exit Codes

| Exit Code | Meaning |
|-----------|---------|
| 0 | Success |
| 1 | General error (API error, not found, validation) |
| 2 | Usage error (invalid flags, missing arguments) |
| 3 | Authentication error (invalid/expired key) |
| 4 | Permission error (insufficient role) |
| 5 | Rate limit exceeded |
| 10 | Plan mode — no changes made (informational) |
| 130 | Interrupted (Ctrl+C) |

### 18.4 Deterministic Output

- List commands always output arrays, even for single results
- Field order is consistent and alphabetical within objects
- Timestamps are always RFC 3339 UTC
- Boolean values are always `true`/`false` (never `yes`/`no`)
- Null values are explicitly `null` (never omitted)

---

## 20. Configuration & Context

### 19.1 Config File

```yaml
# ~/.config/jc/config.yaml
active_profile: acme-corp

defaults:
  output: json                  # Default output format
  limit: 100                    # Default list limit
  confirm_destructive: true     # Require confirmation for delete/lock/erase
  color: auto                   # auto | always | never
  pager: auto                   # auto | always | never (pipes to $PAGER)

cache:
  enabled: true
  ttl: 300                      # Name-to-ID cache TTL in seconds
  directory: ~/.cache/jc

profiles:
  acme-corp:
    org_id: "5f4d..."
    api_key_ref: "keychain://jc/acme-corp"
    default_output: table       # Per-profile output override
  
ask:
  provider: anthropic
  model: claude-sonnet-4-20250514
  api_key_ref: "keychain://jc/anthropic-key"
  confirm_before_execute: true

aliases:
  inactive: "users list --filter 'last_login<=2025-11-15' -t"
  eng-team: "users list --filter 'department=Engineering' -t"
```

### 19.2 Environment Variables

| Variable | Description | Priority |
|----------|-------------|----------|
| `JC_API_KEY` | API key (overrides config/keychain) | Highest |
| `JC_ORG_ID` | Organization ID | Overrides profile |
| `JC_PROFILE` | Active profile name | Overrides config |
| `JC_OUTPUT` | Default output format | Overrides config |
| `JC_CONFIG` | Config file path | Overrides default |
| `JC_NO_COLOR` | Disable color output | Boolean |
| `JC_ASK_PROVIDER` | LLM provider for `jc ask` | Overrides config |
| `JC_ASK_API_KEY` | LLM API key | Overrides config |

### 19.3 Shell Completions

Generated via Cobra's built-in completion system:

```bash
# Bash
jc completion bash > /etc/bash_completion.d/jc

# Zsh
jc completion zsh > "${fpath[1]}/_jc"

# Fish
jc completion fish > ~/.config/fish/completions/jc.fish
```

Completions include dynamic suggestions where feasible (e.g., completing usernames for `jc users get <TAB>`).

---

## 21. Error Handling & Diagnostics

### 20.1 Error Categories

| Category | Example | User Guidance |
|----------|---------|---------------|
| Authentication | Invalid API key, expired token | `jc auth login` to re-authenticate |
| Permission | Insufficient role for operation | Contact org admin for role upgrade |
| Not Found | User/device doesn't exist | Suggest `jc <resource> search` |
| Validation | Invalid email format, missing required field | Show which field failed and expected format |
| Rate Limit | Too many requests | Auto-retry with backoff; show remaining wait |
| Network | DNS failure, timeout | Check connectivity; retry with `--retry` |
| API Error | Upstream JumpCloud error | Show JumpCloud error message + HTTP status |

### 20.2 Verbose/Debug Mode

```bash
# Show HTTP requests (redacted)
$ jc users get jdoe -v
→ GET https://console.jumpcloud.com/api/systemusers?filter=username:$eq:jdoe
← 200 OK (234ms)
{...}

# Full debug (headers, response bodies, cache hits)
$ jc users get jdoe --debug
[DEBUG] Config loaded from ~/.config/jc/config.yaml
[DEBUG] Using profile: acme-corp
[DEBUG] API key loaded from keychain
[DEBUG] Cache miss for username:jdoe
[DEBUG] → GET /api/systemusers?filter=username:$eq:jdoe
[DEBUG] ← 200 OK (234ms, 1 result)
{...}
```

---

## 22. Non-Functional Requirements

### 21.1 Performance

| Metric | Target |
|--------|--------|
| Cold start time | < 50ms (binary launch to first output) |
| Simple list command (cached) | < 300ms (including API round-trip) |
| Name-to-ID resolution (cached) | < 1ms |
| Binary size | < 20MB |
| Memory usage | < 50MB for typical operations |

### 21.2 Compatibility

| Requirement | Specification |
|-------------|---------------|
| macOS versions | macOS 13 (Ventura) and later |
| Linux distributions | Ubuntu 22.04+, Debian 12+, Fedora 38+, RHEL 9+, Arch (rolling) |
| Architectures | amd64, arm64 (Apple Silicon native) |
| Shell support | Bash 4+, Zsh 5+, Fish 3+ |
| Terminal emulators | iTerm2, Terminal.app, Alacritty, Kitty, Wezterm, GNOME Terminal |

### 21.3 Installation Methods

| Method | Command |
|--------|---------|
| Homebrew (macOS/Linux) | `brew install klaassen-consulting/tap/jc` |
| Go install | `go install github.com/klaassen-consulting/jc@latest` |
| Binary download | GitHub Releases (`.tar.gz` for each OS/arch) |
| apt (Debian/Ubuntu) | Via packagecloud or PPA |
| AUR (Arch) | `yay -S jc-jumpcloud` |
| Nix | `nix profile install nixpkgs#jc-jumpcloud` |

---

## 23. Development Phases

### Phase 1 — Foundation (Weeks 1–4)

**Goal:** Functional CLI with core user/device management, JSON output, API key auth.

| Deliverable | Description |
|-------------|-------------|
| Project scaffolding | Go module, Cobra setup, CI/CD (GitHub Actions) |
| Authentication | API key via env var, config file, and OS keychain |
| API client (v1) | HTTP client with auth injection, pagination, rate limiting |
| `jc users` | list, get, create, update, delete, search, lock, unlock |
| `jc devices` | list, get, delete |
| Output engine | JSON (default), table, CSV |
| Global flags | `--output`, `--filter`, `--limit`, `--sort`, `--fields`, `--verbose` |
| Shell completions | Bash, Zsh, Fish |
| Homebrew formula | macOS/Linux installation |
| `jc auth login` | API key authentication flow with keychain storage |
| `jc config` | Basic config file management |

### Phase 2 — Full API Coverage & OAuth (Weeks 5–8)

**Goal:** Complete v1 + v2 API coverage, plan mode, multi-org support, OAuth 2.0.

| Deliverable | Description |
|-------------|-------------|
| API client (v2) | V2 endpoints for groups, policies, associations, graph |
| `jc groups` | Full CRUD + membership management |
| `jc commands` | CRUD, trigger, results |
| `jc policies` | List, get, results |
| `jc apps` | List, get, associations |
| `jc graph` | Traverse associations |
| `jc admins` | List administrators |
| Plan mode | `--plan` flag on all mutating commands |
| Multi-org profiles | Config-based profile switching, `jc auth switch` |
| OAuth 2.0 Service Account auth | `jc auth login --service-account`, token exchange, auto-refresh |
| Name-to-ID caching | In-memory + disk cache with TTL |

### Phase 3 — Directory Insights, Recipes & MCP Server (Weeks 9–14)

**Goal:** Directory Insights, recipe engine, and MCP server — making `jc` the AI-native JumpCloud tool.

| Deliverable | Description |
|-------------|-------------|
| API client (DI) | Directory Insights API wrapper |
| `jc insights` | query, count, distinct, saved searches |
| Recipe engine | YAML parser, template engine, step execution |
| Built-in recipes | 10+ recipes (onboard, offboard, audit, compliance) |
| `jc recipe` | list, show, run, validate, create, import, export |
| Explain mode | `jc explain <command>` |
| NDJSON output | For SIEM integration |
| YAML output | For IaC workflows |
| **MCP server (stdio)** | `jc mcp serve` with tools for all command groups |
| **MCP resources** | Schema, org info, recipe definitions exposed as resources |
| **MCP prompts** | Pre-built prompts for onboarding, offboarding, auditing |
| **MCP safety model** | Plan-first destructive ops, audit logging, rate limiting, read-only mode |
| Claude Desktop config | Documentation and example config for `claude_desktop_config.json` |
| Claude Code integration | `claude mcp add` setup and documentation |

### Phase 4 — Conversational Mode & Polish (Weeks 15–18)

**Goal:** LLM-powered conversational mode via CLI, schema discovery, full documentation, v1.0 release.

| Deliverable | Description |
|-------------|-------------|
| Schema system | `jc schema resources`, `jc schema <resource>`, `jc schema commands` |
| Conversational mode | `jc ask` with Anthropic/OpenAI/Ollama support |
| Structured errors | Machine-readable error objects with codes and suggestions |
| Aliases | User-defined command aliases in config |
| Stdin support | `--stdin` for batch operations |
| Pipe optimization | Detect piped output, auto-disable color/pager |
| **MCP SSE transport** | HTTP+SSE transport for remote/multi-client MCP access |
| **MCP tool allow/block lists** | Configurable tool whitelisting and blacklisting |
| Documentation site | Auto-generated command reference |
| Man pages | Generated from Cobra |
| Release automation | GoReleaser for cross-platform binaries |
| **v1.0 release** | First stable release |

### Phase 5 — Community & Ecosystem (Ongoing)

| Deliverable | Description |
|-------------|-------------|
| Community recipe repository | GitHub repo for shared recipes |
| Plugin system (future) | User-extensible commands via plugins |
| Telemetry (opt-in) | Anonymous usage stats for prioritizing development |
| Integration tests | Against live JumpCloud sandbox org |
| MCP ecosystem | Published to MCP server registries, example integrations with other MCP clients |

---

## 24. Open-Source Strategy

### 23.1 License

**MIT License** — Maximum adoption and community contribution with minimal friction.

### 23.2 Repository Structure

| Item | Description |
|------|-------------|
| `LICENSE` | MIT license |
| `CONTRIBUTING.md` | Contribution guidelines, code style, PR process |
| `CODE_OF_CONDUCT.md` | Contributor Covenant |
| `SECURITY.md` | Security policy, vulnerability reporting |
| `.github/ISSUE_TEMPLATE/` | Bug report, feature request, recipe request templates |
| `.github/workflows/` | CI (test, lint, build), release automation |

### 23.3 Contribution Guidelines

- All changes via PRs with required review
- Conventional Commits for changelog generation
- Go standard formatting (`gofmt`, `golint`, `go vet`)
- Tests required for new commands and API client methods
- Recipe contributions welcome via separate PR template

### 23.4 Community Channels

| Channel | Purpose |
|---------|---------|
| GitHub Issues | Bug reports, feature requests |
| GitHub Discussions | Q&A, recipe sharing, design discussions |
| README badges | CI status, Go version, license, Homebrew |

---

## 25. Success Metrics

### Adoption

| Metric | Target (6 months) | Target (12 months) |
|--------|-------------------|---------------------|
| GitHub stars | 200+ | 1,000+ |
| Homebrew installs | 100+ monthly | 500+ monthly |
| Active contributors | 5+ | 15+ |
| Community recipes | 10+ | 50+ |

### Quality

| Metric | Target |
|--------|--------|
| Test coverage | > 80% |
| API endpoint coverage | 100% |
| Open bug count | < 20 |
| Average issue response time | < 48 hours |
| Release cadence | Bi-weekly |

### User Satisfaction

| Metric | Method |
|--------|--------|
| Command completion rate | Telemetry (opt-in): % of commands that succeed |
| `jc ask` accuracy | % of conversational queries that produce correct commands |
| Recipe usage | Most-used recipes, recipe creation rate |
| GitHub issues (feature requests vs. bugs) | Ratio indicates maturity |

---

## 26. Appendices

### A. Command Quick Reference

```
jc auth login                    # Authenticate with JumpCloud
jc auth status                   # Show current auth state
jc auth switch <profile>         # Switch org/profile

jc users list                    # List all users
jc users get <user>              # Get user by username or ID
jc users create                  # Create a new user
jc users update <user>           # Update a user
jc users delete <user>           # Delete a user
jc users lock <user>             # Lock a user account
jc users unlock <user>           # Unlock a user account
jc users reset-mfa <user>        # Reset MFA
jc users reset-password <user>   # Send password reset

jc devices list                  # List all devices
jc devices get <device>          # Get device by hostname or ID
jc devices lock <device>         # MDM lock
jc devices restart <device>      # MDM restart
jc devices erase <device>        # MDM erase (requires --confirm-erase)

jc groups list                   # List all groups
jc groups user list              # List user groups
jc groups device list            # List device groups
jc groups add-member <g> <m>     # Add member to group
jc groups remove-member <g> <m>  # Remove member from group

jc commands list                 # List commands
jc commands run <cmd> --on <d>   # Run command on device/group
jc commands results <cmd>        # Get command results

jc policies list                 # List policies
jc policies get <policy>         # Get policy details
jc policies results <policy>     # Get policy results

jc insights query                # Query Directory Insights events
jc insights count                # Count events
jc insights distinct             # Get distinct values

jc recipe list                   # List available recipes
jc recipe run <name>             # Run a recipe
jc recipe run <name> --plan      # Preview recipe execution

jc ask "<question>"              # Natural language query

jc schema resources              # List all resources
jc schema <resource>             # Get resource schema
jc schema commands               # Get full command manifest

jc explain "<command>"           # Explain what a command does

jc mcp serve                     # Start MCP server (stdio, for Claude Desktop / Code)
jc mcp serve --transport sse     # Start MCP server (HTTP+SSE, for remote clients)
jc mcp serve --read-only         # Start MCP server with mutations disabled
jc mcp tools                     # List all MCP tools
jc mcp resources                 # List all MCP resources

jc config view                   # View current config
jc config set <key> <value>      # Set a config value
jc completion <shell>            # Generate shell completions
```

### B. Naming Decision: `jc`

The binary name `jc` is short, memorable, and follows the pattern of popular CLI tools (`gh` for GitHub, `aws` for AWS, `gcloud` for Google Cloud, `az` for Azure).

**Potential conflict:** There exists a [jc](https://github.com/kellyjonbrazil/jc) utility that converts command output to JSON. If namespace conflict is a concern, alternatives include `jcl` (JumpCloud CLI) or `jctl` (JumpCloud Control). The PRD uses `jc` throughout but this can be adjusted.

### C. JumpCloud API Rate Limits

JumpCloud enforces rate limits that `jc` must respect:

- **Standard rate limit:** 200 requests per minute per API key
- **Burst:** Short bursts above the limit may be tolerated
- **Directory Insights:** Separate rate limit pool
- **Handling:** Exponential backoff with jitter, retry up to 3 times, surface remaining limit via `--verbose`

### D. Relationship to CloudPilot (iOS App)

`jc` and CloudPilot (the open-source iOS app for JumpCloud administrators) share the same JumpCloud API surface and the same author. They are complementary tools:

- **CloudPilot** is for mobile, on-the-go administration with a visual UI
- **`jc`** is for terminal-native, scriptable, LLM-assisted administration

The two projects may share an API client library in the future (via JumpCloudKit Swift Package for iOS, and a separate Go client for the CLI). Documentation and recipes can cross-reference each other. The MCP server also opens the door for CloudPilot to act as an MCP client in the future, allowing mobile AI-assisted JumpCloud administration.

---

*— End of Document —*
