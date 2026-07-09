<!-- Logo placeholder -->

# jc — JumpCloud CLI

> A modern, LLM-friendly command-line interface for JumpCloud.

Single Go binary. Full API coverage (V1, V2, Directory Insights, Graph). Six output formats. Built-in MCP server for AI assistants. Interactive TUI browser with full CRUD. Recipe engine for multi-step workflows. Plan mode for safe mutation previews. Natural language interface via `jc ask`.

**Browse the full command catalog:** [thejumpcloud.github.io/jc-cli](https://thejumpcloud.github.io/jc-cli/) — searchable reference for every command and flag, plus [`llms.txt`](https://thejumpcloud.github.io/jc-cli/llms.txt) / [`llms-full.txt`](https://thejumpcloud.github.io/jc-cli/llms-full.txt) for AI agents.

---

## Installation

### Pre-built binaries

Download the latest release from [GitHub Releases](https://github.com/TheJumpCloud/jc-cli/releases):

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

# Linux (ARM64)
curl -L https://github.com/TheJumpCloud/jc-cli/releases/latest/download/jc-linux-arm64.tar.gz | tar xz
sudo mv jc-linux-arm64/jc /usr/local/bin/
```

### Build from source

Requires Go 1.25+.

```bash
git clone https://github.com/TheJumpCloud/jc-cli.git
cd jc-cli
make install    # installs to $GOPATH/bin
```

### Verify installation

```bash
jc --version
```

---

## Quick Start

```bash
# First-time setup (interactive wizard)
jc setup

# Or authenticate manually
jc auth login

# Your first commands
jc users list -t
jc devices list --filter "os:eq:Mac OS X" -t
jc insights query --service sso --last 24h -t
```

See the **[Quick Start Cheat Sheet](docs/QUICKSTART.md)** for a single-page reference covering all commands, filtering, TUI keybindings, AI features, and common workflows.

---

## Why jc?

- **Single binary, zero dependencies** — built in Go, runs anywhere. No Python, no PowerShell, no runtime.
- **Full JumpCloud API surface** — 28 resource types across V1, V2, Directory Insights, and Graph APIs. Users, devices, groups, commands, policies, apps, admins, auth policies, IP lists, identity providers, SaaS management, RADIUS, LDAP, Active Directory, Apple MDM, software apps, assets, policy groups, policy templates, system insights, user states, organizations, G Suite, Office 365, Duo MFA, custom emails, and app templates.
- **AI-native** — built-in [MCP server](#mcp-server) with 210 tools for Claude Desktop and Claude Code. `jc ask` translates natural language to CLI commands. Machine-readable schema for LLM tool use.
- **Safety-first mutations** — `--plan` previews every create, update, and delete before execution. `jc explain` describes what a command does without making API calls. Destructive operations require explicit confirmation.
- **Unix pipeline citizen** — JSON by default, `--table` for humans, CSV/YAML/NDJSON for tooling. `--ids` outputs one ID per line for piping. `--query` applies JMESPath transformations. Stdin batch mode for bulk operations.

---

## Feature Showcase

**List users as a table with default fields:**

```bash
jc users list -t
```
```
USERNAME    EMAIL                 DEPARTMENT    ACTIVATED
jdoe        jdoe@acme.com         Engineering   true
jsmith      jsmith@acme.com       Marketing     true
```

**Filter and reshape output with JMESPath:**

```bash
jc users list --query "[?department=='Engineering'].{name:username,email:email}" -t
```
```
NAME    EMAIL
jdoe    jdoe@acme.com
alee    alee@acme.com
```

**Pipe IDs between commands:**

```bash
# Lock all suspended users
jc users list --filter "suspended:eq:true" --ids | xargs -I{} jc users lock {} --force
```

**Traverse and bind resource associations (Graph API):**

```bash
jc graph traverse --from device:JDOE-MBP --to command -t
jc graph bind --from user_group:Engineering --to application:Slack
jc graph unbind --from user:jdoe --to application:OldApp
```

**Query audit events from Directory Insights:**

```bash
jc insights query --service sso --last 24h --event-type sso_auth_failed -t
```

**Run a multi-step recipe:**

```bash
jc recipe run onboard-user --param username=jdoe --param email=jdoe@acme.com --param groups=Engineering
```

**Ask in natural language:**

```bash
jc ask "which users haven't activated their accounts?"
```
```
Proposed commands:
  [1] jc users list --filter "activated:eq:false" -t

Execute these commands? [y/N]
```

**Preview mutations before executing:**

```bash
jc users delete jdoe --plan
```
```
┌─ Plan ────────────────────────────────────────────┐
│ Action:   DELETE user                              │
│ Target:   jdoe (aa11bb22cc33dd44ee550001)          │
│ Resource: users                                    │
│                                                    │
│ This action is IRREVERSIBLE.                       │
└───────────────────────────────────────────────────┘
```

**Browse resources interactively:**

```bash
jc tui
```

Full-screen terminal UI with keyboard navigation, live filtering, sorting, detail views with associations, inline create/edit/delete, clipboard copy, export to JSON/CSV, and bookmarked resources on the home screen.

---

## Command Reference

| Command | Subcommands | Description |
|---------|-------------|-------------|
| `users` | list, get, search, create, update, delete, lock, unlock, reset-mfa, reset-password, ssh-keys, ssh-key-add, ssh-key-delete | Manage system users and SSH keys |
| `devices` | list, get, search, update, delete, lock, restart, erase, fde-key | Manage devices, MDM commands, and recovery keys |
| `groups` | user (list/get/create/update/delete), device (list/get/create/update/delete), add-member, remove-member | Manage user and device groups |
| `commands` | list, get, create, update, delete, run, results, trigger | Manage and execute commands |
| `policies` | list, get, create, update, delete, results | Manage policies and view status |
| `apps` | list, get, create, update, delete | Manage SSO applications |
| `admins` | list, get, create, update, delete | Manage administrator accounts |
| `auth-policies` | list, get, create, update, delete, enable, disable, simulate, blast-radius | Conditional access policies |
| `iplists` | list, get, create, update, delete | Manage IP lists for auth policies |
| `identity-providers` | list, get, create, update, delete | Manage identity providers for SSO/OIDC |
| `saas-management` | list, get, create, update, delete, accounts, account-get, account-delete, usage, licenses, catalog-get | Manage SaaS applications and licenses |
| `insights` | query, count, distinct, save, saved, run | Query Directory Insights events |
| `graph` | traverse, bind, unbind | Traverse and manage resource associations |
| `org` | list, get, settings, update | View and update organization settings |
| `software` | list, get, create, update, delete, statuses, associations, reclaim-license | Manage software apps (V2) |
| `ldap` | list, get, create, update, delete, samba-domains, samba-domain-get/create/update/delete | Manage LDAP server integrations |
| `ad` | list, get, create, update, delete | Manage Active Directory integrations |
| `radius` | list, get, create, update, delete | Manage RADIUS server integrations |
| `apple-mdm` | list, get, create, update, delete, enrollment-profiles, devices | Manage Apple MDM configurations |
| `policy-templates` | list, get | Browse policy templates |
| `policy-groups` | list, get, create, update, delete | Manage policy groups |
| `system-insights` | \<table\>, tables | Query osquery system insights (62 tables) |
| `user-states` | list, get, create, delete | Schedule bulk user suspend/reactivate |
| `gsuite` | list, get, translation-rules, import-users | Manage G Suite directory integrations |
| `office365` | list, get, translation-rules, import-users | Manage Office 365 directory integrations |
| `duo` | list, get, create, delete, apps, app-get, app-create, app-delete | Manage Duo MFA accounts and applications |
| `custom-emails` | templates, get, create, update, delete | Manage custom email templates |
| `assets` | list, get, create, update, delete | Manage hardware assets |
| `app-templates` | list, get | Browse application templates |
| `bulk` | users | Bulk operations from CSV files |
| `recipe` | list, show, run, create, import, export, validate | Multi-step workflow engine |
| `auth` | login, logout, status, switch | Manage credentials and profiles |
| `config` | view, set | View and modify configuration |
| `ask` | *(direct)* | Natural language to CLI translation |
| `explain` | *(direct)* | Describe what a command does |
| `mcp` | serve, tools | MCP server for AI assistants |
| `schema` | resources, commands | Machine-readable CLI schema |
| `setup` | *(direct)* | Interactive onboarding wizard |
| `tui` | *(direct)* | Interactive terminal UI browser |

### Users

```bash
jc users list -t                                  # List all users
jc users list --filter "department:eq:Engineering" # Filter by department
jc users get jdoe                                  # Get by username or ID
jc users search "john"                             # Full-text search
jc users create --username jnew --email jnew@acme.com --firstname Jane --lastname New --department Engineering
jc users update jdoe --department Marketing        # Update fields
jc users delete jdoe                               # Delete (with confirmation)
jc users lock jdoe                                 # Lock account
jc users unlock jdoe                               # Unlock account
jc users reset-mfa jdoe                            # Reset MFA enrollment
jc users reset-password jdoe                       # Trigger password reset
jc users ssh-keys jdoe                             # List SSH keys
jc users ssh-key-add jdoe --name laptop --public-key "ssh-ed25519 AAAA..."
jc users ssh-key-delete jdoe --key-id abc123...    # Delete an SSH key
```

### Devices

```bash
jc devices list -t                                # List all devices
jc devices list --filter "os:eq:Mac OS X" -t      # Filter by OS
jc devices get JDOE-MBP                           # Get by hostname or ID
jc devices search "macbook"                       # Full-text search
jc devices update JDOE-MBP --displayName "Jane's MacBook"
jc devices delete JDOE-MBP                        # Delete (with confirmation)
jc devices lock JDOE-MBP                          # MDM lock
jc devices restart JDOE-MBP                       # MDM restart
jc devices erase JDOE-MBP --confirm-erase         # MDM erase (requires flag + confirmation)
jc devices fde-key JDOE-MBP                       # Retrieve FileVault/BitLocker recovery key
```

### Groups

```bash
jc groups user list -t                            # List user groups
jc groups device list -t                          # List device groups
jc groups user create --name Engineering           # Create user group
jc groups user update Engineering --name "Eng Team"
jc groups user delete Engineering                  # Delete user group
jc groups add-member Engineering --user jdoe       # Add user to group
jc groups remove-member Engineering --user jdoe    # Remove user from group
jc groups add-member "macOS Fleet" --device JDOE-MBP  # Add device to group
```

### Commands

```bash
jc commands list -t                               # List all commands
jc commands get "Install Agent" -t                # Get by name or ID
jc commands create --name "Patch" --command "apt update" --type linux
jc commands update "Patch" --command "apt upgrade"
jc commands delete "Patch"                        # Delete (with confirmation)
jc commands run "Install Agent" --device JDOE-MBP # Run on a device
jc commands run "Patch All" --device-group "macOS Fleet"  # Run on device group
jc commands results "Install Agent" -t            # View execution results
jc commands trigger my-webhook-trigger            # Fire a command trigger by name
jc commands trigger my-trigger --data '{"key":"value"}'  # With JSON payload
```

### Policies

```bash
jc policies list -t                               # List all policies
jc policies get "FileVault" -t                    # Get by name or ID
jc policies create --name "Screen Lock" --template-id abc123...
jc policies update "Screen Lock" --values-json '{"timeout": 300}'
jc policies delete "Screen Lock"                  # Delete (with confirmation)
jc policies results "FileVault" -t                # View policy application status
```

### Apps

```bash
jc apps list -t                                   # List SSO applications
jc apps get Slack -t                              # Get by name or ID
jc apps create --name "Internal App" --sso-url https://app.example.com
jc apps update "Internal App" --display-label "Our App"
jc apps delete "Internal App"                     # Delete (with confirmation)
```

### Admins

```bash
jc admins list -t                                 # List administrators
jc admins get admin@acme.com                      # Get by email or ID
jc admins create --email newadmin@acme.com
jc admins update admin@acme.com --enable-multi-factor true
jc admins delete admin@acme.com                   # Delete (with confirmation)
```

### Auth Policies & IP Lists

```bash
jc auth-policies list -t                          # List conditional access policies
jc auth-policies get "Require MFA" -t             # Get by name or ID
jc auth-policies create --name "Block Risky IPs" --effect deny --conditions '...'
jc auth-policies enable "Block Risky IPs"         # Enable a policy
jc auth-policies disable "Block Risky IPs"        # Disable a policy
jc auth-policies simulate --user jdoe --ip 1.2.3.4 --device-enrolled true -t
jc auth-policies blast-radius "Require MFA" -t    # Show affected users

jc iplists list -t                                # List IP lists
jc iplists create --name "Office IPs" --ips "10.0.0.0/8,192.168.1.0/24"
jc iplists update "Office IPs" --ips "10.0.0.0/8"
jc iplists delete "Office IPs"
```

### Identity Providers

```bash
jc identity-providers list -t                     # List identity providers
jc idp list                                       # Short alias
jc identity-providers get "Corporate OIDC"        # Get by name
jc identity-providers create --name "Corp IdP" --type OIDC --client-id abc --client-secret xyz --url https://accounts.google.com
```

### SaaS Management

```bash
jc saas-management list -t                       # List discovered SaaS apps
jc saas list -t                                  # Short alias
jc saas-management get <app-id>                  # Get app details
jc saas-management accounts <app-id>             # List user accounts for app
jc saas-management usage <app-id> --day-count 30 # App usage over last 30 days
jc saas-management licenses                      # List all SaaS licenses
```

### Insights (Directory Insights)

```bash
jc insights query --service all --last 24h -t             # All events, last 24h
jc insights query --service sso --last 7d --event-type sso_auth_failed  # SSO failures
jc insights count --service directory --last 30d           # Count events
jc insights distinct --service all --last 7d --field event_type -t  # Unique event types
jc insights save --name "sso-failures" --service sso --last 24h --event-type sso_auth_failed
jc insights run sso-failures -t                            # Re-run saved query
```

### Graph

Traverse and manage associations between JumpCloud resources.

```bash
jc graph traverse --from user:jdoe --to application -t
jc graph traverse --from device:JDOE-MBP --to command -t
jc graph bind --from user_group:Engineering --to application:Slack
jc graph unbind --from user:jdoe --to application:OldApp --force
```

### Organizations

```bash
jc org list -t                                    # List organizations
jc org get <org-id>                               # Get organization details
jc org settings <org-id>                          # View all organization settings
jc org update <org-id> --name "New Org Name"      # Update organization name
jc org update <org-id> --settings-json '{"passwordPolicy":{"minLength":12}}'
```

### Infrastructure Integrations

```bash
# Software Management
jc software list -t                               # List managed software apps
jc software get "Google Chrome"                   # Get by name or ID
jc software create --name "Zoom" --package-id com.zoom.us
jc software delete "Zoom"
jc software statuses "Google Chrome" -t          # View install statuses
jc software associations "Google Chrome" -t      # View device associations
jc software reclaim-license "Zoom"               # Reclaim unused license

# LDAP Servers
jc ldap list -t                                   # List LDAP server integrations
jc ldap get "Corp LDAP"                           # Get by name or ID
jc ldap create --name "New LDAP"
jc ldap delete "Corp LDAP"
jc ldap samba-domains "Corp LDAP" -t             # List Samba domains
jc ldap samba-domain-create "Corp LDAP" --name "CORP" --sid "S-1-5-21-..."

# Active Directory
jc ad list -t                                     # List AD integrations
jc ad get "corp.example.com"                      # Get by domain or ID
jc ad create --domain "new.example.com"
jc ad delete "corp.example.com"

# Asset Management
jc assets list -t                                 # List hardware assets
jc assets get "MacBook Pro #42"                   # Get by name or ID
jc assets create --name "MacBook Pro #43" --serial-number "C02X..." --status "In Stock"
jc assets update "MacBook Pro #43" --status "Assigned" --system-id abc123...
jc assets delete "MacBook Pro #43"                # Delete (with confirmation)

# RADIUS Servers
jc radius list -t                                 # List RADIUS servers
jc radius get "WiFi Auth"                         # Get by name or ID
jc radius create --name "Guest WiFi" --shared-secret "..."
jc radius delete "WiFi Auth"

# Apple MDM
jc apple-mdm list -t                              # List MDM configurations
jc apple-mdm get "Corp MDM"                       # Get by name or ID
jc apple-mdm enrollment-profiles "Corp MDM" -t   # List enrollment profiles
jc apple-mdm devices "Corp MDM" -t               # List managed devices
jc apple-mdm create --name "New MDM"
jc apple-mdm delete "Corp MDM"
```

### Directory Integrations

```bash
# G Suite
jc gsuite list -t                                # List G Suite integrations
jc gsuite get "Acme GSuite"                      # Get by name or ID
jc gsuite translation-rules "Acme GSuite" -t     # View attribute mapping rules
jc gsuite import-users "Acme GSuite"             # Trigger user import

# Office 365
jc office365 list -t                             # List Office 365 integrations
jc office365 get "Acme O365"                     # Get by name or ID
jc office365 translation-rules "Acme O365" -t   # View attribute mapping rules
jc office365 import-users "Acme O365"            # Trigger user import

# Duo MFA
jc duo list -t                                   # List Duo accounts
jc duo get "Acme Duo"                            # Get by name or ID
jc duo create --name "New Duo Account"
jc duo delete "Acme Duo"
jc duo apps "Acme Duo" -t                        # List Duo applications
jc duo app-create "Acme Duo" --name "VPN App"
jc duo app-delete "Acme Duo" --app-id abc123...
```

### Custom Emails & App Templates

```bash
# Custom Email Templates
jc custom-emails templates -t                    # List available email types
jc custom-emails get activate_user_custom        # View email config by type
jc custom-emails create activate_user_custom --subject "Welcome!" --body "<html>..."
jc custom-emails update activate_user_custom --subject "Welcome to Acme!"
jc custom-emails delete activate_user_custom     # Reset to default

# Application Templates (read-only catalog)
jc app-templates list -t                         # Browse SSO app templates
jc app-templates get <template-id>               # View template details
```

### Policy Management

```bash
# Policy Templates (read-only catalog)
jc policy-templates list -t                       # Browse available templates
jc policy-templates get <template-id>             # View template details

# Policy Groups
jc policy-groups list -t                          # List policy groups
jc policy-groups create --name "Security Policies"
jc policy-groups update "Security Policies" --description "..."
jc policy-groups delete "Security Policies"
```

### System Insights

Query osquery data from managed devices. Supports 62 tables including `os_version`, `disk_encryption`, `apps`, `chrome_extensions`, `logged_in_users`, `wifi_networks`, and more.

```bash
jc system-insights tables                         # List all available tables
jc system-insights os_version -t                  # Query a table across all devices
jc system-insights disk_encryption --system-id JDOE-MBP -t   # Query for specific device
jc system-insights chrome_extensions --filter "name:eq:uBlock Origin" -t
jc system-insights apps --limit 50 -t             # Limit results
```

### User States (Scheduled Suspend/Reactivate)

```bash
jc user-states list -t                            # List scheduled state changes
jc user-states create --user jdoe --state suspended --start-date 2026-03-01
jc user-states get <state-id>                     # View a scheduled change
jc user-states delete <state-id>                  # Cancel a scheduled change
```

### Bulk Operations

```bash
# CSV with columns: username, email, firstname, lastname, department, operation
jc bulk users --file new-hires.csv --plan         # Preview what would happen
jc bulk users --file new-hires.csv                # Execute (with confirmation)
```

The `operation` column can be `create`, `update`, or `delete`. If omitted, defaults to `create`.

### Setup Wizard

```bash
jc setup                    # Walk through first-time configuration
```

The wizard guides you through profile selection, authentication (API key or service account), organization ID, output format, color, and list limit. On re-run, existing settings are shown — press Enter to keep current values. Each step saves immediately, so partial completion (Ctrl-C) preserves progress.

### Audit

```bash
jc audit                                  # All checks, grouped by category
jc audit --category security              # Security checks only
jc audit --severity high                  # High/critical findings only
jc audit --output json                    # For skills + CI pipelines
jc audit --exit-code --threshold high     # CI gate: non-zero on high+ findings
```

A composable cross-resource health check registry. Each finding carries a severity (`info` → `critical`), a `resource_ref` like `admin:alice@acme.com`, and a `remediation_hint` that names the exact `jc` command to fix it. Categories: `security`, `compliance`, `hygiene`, `identity`. Backs the `jc-security-audit` and `jc-compliance-check` skills. Full check reference: [docs/AUDIT.md](docs/AUDIT.md).

### Apple MDM payloads catalog

```bash
jc apple-mdm payloads list                          # all 125 Apple Configuration Profile schemas
jc apple-mdm payloads list --os macOS --search wifi # filter
jc apple-mdm payloads show com.apple.wifi.managed   # full key reference for one payload

# Emit a valid .mobileconfig from any payload (offline, plutil -lint clean):
jc apple-mdm payloads template com.apple.wifi.managed \
    --values SSID_STR=CorpWiFi --values AutoJoin=true --values EncryptionType=WPA2 \
    --name "Corp WiFi" --output-file corp-wifi.mobileconfig

# Or supply nested structures via JSON:
jc apple-mdm payloads template com.apple.wifi.managed --values-file wifi.json

# End-to-end: emit + POST to JumpCloud as a Custom MDM Configuration Profile policy
jc apple-mdm payloads create-policy com.apple.security.firewall \
    --name "Firewall — enforce" --values EnableFirewall=true --values EnableStealthMode=true
jc apple-mdm payloads create-policy com.apple.wifi.managed \
    --name "Corp WiFi (MDM)" --values-file wifi.json --plan   # preview without POSTing

# Multi-payload bundles (CIS-style profiles with N inner payloads)
jc apple-mdm payloads compose --config corp-baseline.yaml             # emit to stdout
jc apple-mdm payloads compose --config corp-baseline.yaml \
    --output-file corp-baseline.mobileconfig                          # write to file (atomic)
jc apple-mdm payloads compose --config corp-baseline.yaml --create-policy  # POST to JC
```

A browsable catalog, offline `.mobileconfig` generator (single-payload via `template` and multi-payload bundles via `compose`), AND end-to-end JumpCloud policy creator for Apple's official MDM Configuration Profile schemas, vendored from [apple/device-management](https://github.com/apple/device-management) (MIT-licensed, pinned to `Release-v26.4`) and embedded in the binary at build time. The emitter coerces and validates user values against Apple's schema (range, rangelist, required-keys), writing a `plutil -lint`-clean plist with auto-generated UUIDs and Apple's standard Configuration envelope. `create-policy` resolves the JumpCloud Custom MDM template dynamically per OS family — no hardcoded IDs — so it works on any tenant. Supports `--os macOS` and `--os iOS`; tvOS/visionOS/watchOS are not supported by JumpCloud MDM.

### Windows custom MDM policies

```bash
# Discover: browse Microsoft's Policy CSP catalog (~230 areas, ~3,700 settings)
jc windows-mdm csp list --search camera                  # natural-language → OMA-URI
jc windows-mdm csp show Camera/AllowCamera               # format, allowed values, OS build
jc windows-mdm csp template Camera/AllowCamera \
    Bluetooth/AllowDiscoverableMode --output-file lockdown.json
jc windows-mdm csp update                                # prefetch/refresh the snapshot

# Author + create: the template output feeds create-policy directly
jc windows-mdm oma-uri create-policy --name "Lockdown" --settings-file lockdown.json --plan

# Policy CSP settings via OMA-URI — the Windows analog of apple-mdm payloads create-policy
jc windows-mdm oma-uri create-policy --name "Require BitLocker" \
    --setting 'uri=./Device/Vendor/MSFT/Policy/Config/BitLocker/RequireDeviceEncryption,format=int,value=1'

# Multiple settings per policy; preview with --plan before POSTing
jc windows-mdm oma-uri create-policy --name "Camera + Bluetooth lockdown" \
    --setting 'uri=./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera,format=int,value=0' \
    --setting 'uri=./Device/Vendor/MSFT/Policy/Config/Bluetooth/AllowDiscoverableMode,format=int,value=0' \
    --plan

# HKLM registry keys (HKEY_LOCAL_MACHINE implied — never prefix it)
jc windows-mdm registry create-policy --name "Disable Autorun" \
    --key 'location=SOFTWARE\Policies\Microsoft\Windows\Explorer,name=NoAutorun,type=DWORD,data=1'

# Batch from JSON files
jc windows-mdm oma-uri create-policy --name "Baseline" --settings-file baseline.json
jc windows-mdm registry create-policy --name "Chrome baseline" --keys-file chrome.json
```

Creates JumpCloud "Custom MDM (OMA-URI)" and "Advanced: Custom Registry Keys" policies for Windows devices — for settings JumpCloud has no built-in policy for, exactly like Intune's Custom OMA-URI profile. Values are validated up front (format/type enums, OMA-URI path shape, hive-prefix rejection, numeric checks for int/DWORD/QWORD) with every problem reported in one pass. Templates are resolved dynamically by name — no hardcoded IDs. Both policy shapes are device-scoped.

The `csp` catalog covers every Policy CSP area including ADMX-backed settings (flagged — their values need ADMX-style XML). Catalog data is Microsoft's pinned DDF v2 snapshot, downloaded **on demand** from Microsoft's official URL (SHA-256-verified) into `~/.cache/jc/windows-mdm-ddf/` — never vendored into the binary, since Microsoft's download terms don't permit redistribution. Offline after the one-time fetch; air-gapped hosts can pre-place the zip in the cache dir. Standalone CSPs (BitLocker CSP, Firewall CSP, VPNv2) are not in the catalog, but their OMA-URIs work with `oma-uri create-policy` directly.

### Interactive TUI

```bash
jc tui                      # Launch the interactive browser
```

Full-screen terminal UI for browsing all 26 JumpCloud resource types.

**Home screen** — Three-column grid layout mirroring the JumpCloud Admin Console:

```
 User Management       Device Management      Access
   > Users       (13)    > Devices      (10)    > SSO Apps      (3)
   > User Groups  (5)    > Device Groups (5)    > LDAP Servers   (2)
   > Active Dir   (2)    > Commands      (8)    > RADIUS Servers (1)
   > Cloud Dirs   (>)    > Policies      (5)
                         > Policy Grps   (3)  Insights
 Security                > Software      (5)    > Dir Insights
   > Auth Policies (5)   > Apple MDM     (1)    > Sys Insights
   > IP Lists      (3)   > Assets        (4)
                       Settings
                         > Admins        (3)
                         > Organization
                         > Custom Emails
```

The grid is responsive — three columns at 120+ chars, two at 90-119, single-column below 90. Unimplemented items (HR Directories, Patch Management, etc.) appear grayed out. Cloud Directories is a sub-menu grouping Google Workspace and M365.

**Navigation & features:**
- **Home grid** — arrow keys to navigate rows and columns, Enter to open, `/` to filter, `b` to bookmark, `d` for dashboard
- **List views** — live filtering (`/`), sort cycling (`s`), field toggling (`a`), search (`/` in search mode)
- **Detail views** — associations tab with group membership, graph traversal, and related resources
- **Directory Insights** — query by service, time range, and event type; drill into events; `x` for AI explanation
- **Apple MDM** — browse the vendored payload catalog, author policies in a typed form (or `$EDITOR`), edit existing Custom MDM policies
- **Windows MDM** — browse Microsoft's Policy CSP catalog (~3,700 settings, fetched on demand), add settings to a draft (`a`), author the OMA-URI policy with enum pick-lists and range validation, preview, create; a registry-key policy row editor; and a custom-policies list that decodes existing OMA-URI/registry policies back into the form for editing
- **CRUD** — `n` to create (schema-driven form), `E` to edit, `d` to delete (with confirmation)
- **Form navigation** — `j`/`k` between fields, `h`/`l` to toggle booleans, `Ctrl+S` to save, `Esc` to cancel; sensitive fields are masked
- **Clipboard & export** — `c` to copy ID, `e` then `j`/`c`/`J` for JSON clipboard / CSV file / JSON file
- **Keyboard-driven** — `j`/`k` or arrows, Enter to drill in, Esc to go back, `?` for context-sensitive help

---

## AI & Automation

### MCP Server

jc includes a built-in [Model Context Protocol](https://modelcontextprotocol.io/) server that exposes JumpCloud operations as tools for AI assistants like Claude Desktop and Claude Code.

**Claude Desktop configuration** (`claude_desktop_config.json`):

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

**210 tools available** covering all 28 resource types — user management, device operations, group membership, policy management, insights queries, graph associations, infrastructure integrations (LDAP, AD, RADIUS, Apple MDM, G Suite, Office 365, Duo), SaaS management, asset management, custom emails, app templates, recipe execution, command explanation, and plan-mode previews. Includes a dedicated **Apple MDM payloads catalog** (`apple_mdm_payloads_*`) that lets agents map a natural-language MDM intent to one of Apple's vendored schemas (`com.apple.security.firewall`, `com.apple.applicationaccess`, etc.) and create a JumpCloud Custom MDM Configuration Profile from it in one tool call, plus a **Windows MDM app** (`windows_mdm_*`): a Policy CSP discovery catalog (`csp_search` / `csp_show` / `csp_template` over Microsoft's ~3,700-setting DDF snapshot) feeding OMA-URI and HKLM-registry policy creation. All destructive operations require explicit `execute: true` confirmation.

```bash
jc mcp tools    # List all available MCP tool names
```

Tool access can be restricted via configuration:

```yaml
mcp:
  allowed_tools: ["users_*", "devices_list", "insights_*"]
  blocked_tools: ["users_delete", "devices_erase"]
```

**Resources** are also exposed: resource schemas, CLI command manifest, recipe definitions, config profiles, and server info — giving AI assistants full context about available operations.

### Ask (Natural Language)

Translate natural language questions into jc CLI commands using an LLM. The LLM never has direct API access — it generates command strings that are validated against the CLI schema before execution.

```bash
jc ask "show me all macOS devices"
jc ask "find SSO auth failures in the last 24 hours"
jc ask "list all user groups and their member count"
```

Configure your LLM provider:

```bash
jc config set ask.provider anthropic    # or: openai, ollama
jc config set ask.api_key <your-key>
```

Supports Anthropic, OpenAI, and Ollama (local models). Use `--force` to skip confirmation, or `--output json` to get proposed commands without executing.

### Recipes

Recipes are YAML-defined multi-step workflows that automate common JumpCloud operations. jc ships with 11 built-in recipes:

| Recipe | Description |
|--------|-------------|
| `onboard-user` | Create a new user, add to groups, verify |
| `offboard-user` | Lock account, remove from all groups, reset MFA |
| `security-audit` | Check MFA adoption, auth failures, admin access |
| `compliance-report` | MFA status, user inventory, device inventory |
| `mfa-enforcement-check` | List users and their MFA enrollment status |
| `audit-inactive-users` | Find users who haven't logged in recently |
| `audit-unmanaged-devices` | Identify devices not in any device group |
| `stale-device-cleanup` | Find devices not seen by JumpCloud recently |
| `password-expiry-report` | Identify upcoming password expirations |
| `bulk-create-users` | Create multiple users from CSV |
| `group-sync` | Add a user to a group by name |

```bash
jc recipe list -t                                     # List all recipes
jc recipe show onboard-user                           # View recipe details
jc recipe run onboard-user --param username=jdoe --param email=jdoe@acme.com
jc recipe run onboard-user --plan                     # Preview without executing
```

**Create your own recipes** in `~/.config/jc/recipes/`:

```bash
jc recipe create                    # Interactive builder
jc recipe import ./my-recipe.yaml   # Import from file
jc recipe import https://example.com/recipe.yaml  # Import from URL
jc recipe validate ./my-recipe.yaml # Validate syntax
```

User-defined recipes with the same name as a built-in recipe override it.

### Multi-org fan-out

Run one command across many configured profiles in parallel and merge the results — the MSP view of a fleet of orgs:

```bash
# Aggregate JSON report across all prod orgs: [{profile, status, data|error}, ...]
jc multi --filter 'prod-*' -- policies list

# Explicit profile list
jc multi --profiles acme,globex,initech -- users list --filter 'mfa.configured:eq:false'

# IDs prefixed with the profile for disambiguation
jc multi --filter '*' -- users list --filter 'state:eq:SUSPENDED' --ids
#   acme/66a1..., globex/59f2...

# Table output concatenates per-profile sections
jc multi --filter 'prod-*' -- devices list -t

# Destructive fan-out requires an explicit extra gate
jc multi --profiles staging-a,staging-b --allow-destructive -- users delete bot-account --force
```

Each profile runs the inner command as its own `jc` subprocess with `--org <profile>` — auth, caching, and output stay fully isolated per org. Failures are isolated per profile (one bad org never aborts the rest; `jc multi` exits non-zero if any failed). Safety is annotation-driven: destructive inner commands (per their `jc:class`) are refused without `--allow-destructive`, and `read_only`-role profiles are skipped for write commands. `--concurrency` caps parallelism (default: number of profiles, max 8).

### Claude Code Skills Plugin

jc-cli ships a set of conversational Claude Code skills as a self-hosted marketplace at the repo root. Install:

```
/plugin marketplace add TheJumpCloud/jc-cli
/plugin install jc-cli-skills@jc-cli
```

That makes seven skills available as `/jc-cli-skills:<name>` slash commands:

| Skill | What it does |
|---|---|
| `jc` | Generic JumpCloud Q&A + ad-hoc resource queries |
| `jc-onboard-user` | New-user provisioning runbook (create, group memberships, device assignment, welcome email) |
| `jc-offboard-user` | Offboarding runbook (suspend, lock, remove from groups, audit log capture) |
| `jc-troubleshoot-auth` | Auth-failure investigation across SSO, RADIUS, LDAP, and policy denials |
| `jc-security-audit` | Cross-resource security posture audit (admins-without-MFA, stale devices, FDE coverage, …) |
| `jc-compliance-check` | Compliance posture against a chosen baseline |
| `jc-create-recipe` | Interactive YAML recipe authoring with schema validation |

Skills delegate every JumpCloud operation to the `jc` binary, so the LLM never issues raw API calls — same auth, same audit log, same `--plan` safety rails as a human at the terminal. The plugin only requires `jc` on `$PATH` and an authenticated profile (`jc auth login` followed by `jc doctor`). Skills are defined under [`skills/`](skills/); the marketplace manifest is at [`.claude-plugin/marketplace.json`](.claude-plugin/marketplace.json).

### Explain

Describe what a command would do without executing it or making API calls. Useful for reviewing LLM-generated commands before running them.

```bash
jc explain users delete jdoe
```
```
┌─ Explanation ─────────────────────────────────────┐
│ Command:      users delete jdoe                    │
│ Action:       DELETE                               │
│ Resource:     users                                │
│ Description:  Permanently delete a JumpCloud user  │
│                                                    │
│ *** DESTRUCTIVE OPERATION ***                      │
│                                                    │
│ Reversible:   NO                                   │
│ Side effects:                                      │
│  - User is removed from all groups                 │
│  - User loses access to all bound resources        │
│ Warning:      This action cannot be undone          │
│ Auth required: Yes                                  │
└───────────────────────────────────────────────────┘
```

---

## Output & Filtering

### Output Formats

| Format | Flag | Description |
|--------|------|-------------|
| JSON | `--output json` *(default)* | Pretty-printed JSON array or object |
| Table | `--output table` or `-t` | Aligned columns with headers |
| CSV | `--output csv` | Standard CSV with header row |
| YAML | `--output yaml` | YAML document |
| NDJSON | `--output ndjson` | Newline-delimited JSON (one object per line) |
| Human | `--output human` | Key-value pairs, one per line |

Data always goes to stdout, metadata (footers, progress) to stderr — so piping always works cleanly.

### Field Selection

```bash
jc users list --fields username,email,department -t   # Only these fields
jc users list --exclude password_date,totp_enabled -t # All except these
jc users list --all -t                                 # Every available field
```

Priority: `--fields` > `--exclude` > `--all` > default fields.

### JMESPath Queries

Use `--query` to filter and reshape output with [JMESPath](https://jmespath.org/) expressions:

```bash
# Filter to active users, select specific fields
jc users list --query "[?activated].{name:username,dept:department}" -t

# Sort by field
jc devices list --query "sort_by(@, &hostname)" -t

# Count items
jc users list --query "length(@)"
```

### Piping with --ids

`--ids` outputs one resource ID per line, designed for Unix pipelines:

```bash
# Delete all suspended users
jc users list --filter "suspended:eq:true" --ids | xargs -I{} jc users delete {} --force

# Get details for devices in a group
jc graph traverse --from device_group:Servers --to user --ids | xargs -I{} jc users get {}
```

### Stdin Batch Mode

Pipe IDs from stdin for batch operations:

```bash
echo -e "user1-id\nuser2-id\nuser3-id" | jc users delete --stdin --force
jc users list --filter "suspended:eq:true" --ids | jc users delete --stdin --force
```

### Quiet Mode

`--quiet` suppresses all output. Use exit codes for scripting:

```bash
if jc auth status --quiet; then
  echo "Authenticated"
fi
```

---

## Configuration

### Config File

Location: `~/.config/jc/config.yaml` (XDG-compliant). Override with `JC_CONFIG` environment variable.

```yaml
active_profile: production
defaults:
  output: json
  color: true
  confirm_destructive: true
cache:
  enabled: true
  ttl: 3600
profiles:
  production:
    api_key: keychain://jc/production
    org_id: 5f1234567890abcdef123456
  staging:
    api_key: keychain://jc/staging
aliases:
  inactive: "users list --filter 'activated:eq:false' -t"
  macos: "devices list --filter 'os:eq:Mac OS X' -t"
```

### Profiles

Manage multiple JumpCloud organizations:

```bash
jc auth login                         # Login to default profile
jc auth login --profile staging       # Login to named profile
jc auth switch production             # Switch active profile
jc auth status                        # Show current auth state
jc users list --org staging           # One-off profile override
```

### Authentication Methods

**Service Account (OAuth 2.0) — recommended**:

```bash
jc auth login --service-account       # Interactive client ID + secret entry
```

Service accounts issue short-lived bearer tokens that refresh automatically against an upstream identity. They are easier to rotate, revoke, and scope than personal API keys, so this is the recommended path for new deployments. The setup wizard (`jc setup`) presents service accounts as the default option.

**API Key** (alternative; legacy):

```bash
jc auth login                         # Interactive, stores in OS keychain
export JC_API_KEY=your-key            # Or set via environment
```

API keys are long-lived bearer secrets. They still work for backwards compatibility, but new deployments should prefer service accounts.

Both methods store credentials in the OS keychain (macOS Keychain / Linux secret-tool) by default. The config file stores only a `keychain://jc/<profile>` reference, never the plaintext credential. If the keychain is unavailable, login fails rather than silently storing credentials as plaintext — pass `--allow-plaintext` to explicitly opt in to file storage. **Plaintext storage is a security risk** (backups, sync clients, and other process readers all recover the credential); prefer fixing the keychain.

For the full authentication and authorization model — credential storage, MCP server trust model, audit log shape and redaction, threat model, and what jc does *not* enforce — see [`docs/AUTH.md`](docs/AUTH.md).

### Environment Variables

| Variable | Description |
|----------|-------------|
| `JC_API_KEY` | API key (overrides config and keychain) |
| `JC_ORG_ID` | Organization ID |
| `JC_PROFILE` | Active profile name |
| `JC_OUTPUT` | Default output format |
| `JC_CONFIG` | Config file path override |
| `JC_NO_COLOR` | Disable color output |
| `JC_ASK_API_KEY` | LLM provider API key for `jc ask` |
| `NO_COLOR` | Standard [no-color.org](https://no-color.org/) support |

Priority: flags > environment variables > config file > built-in defaults.

### Aliases

Define shortcuts for common commands:

```bash
jc config set aliases.inactive "users list --filter 'activated:eq:false' -t"
jc config set aliases.macos "devices list --filter 'os:eq:Mac OS X' -t"

# Then use them directly:
jc inactive
jc macos --limit 10
```

---

## Diagnostics

`jc doctor` is the no-auth diagnostic command. Run it first when any other `jc` command fails with an auth or connectivity error.

```bash
jc doctor --output human       # grouped human-readable sections
jc doctor                      # JSON (script-friendly default)
jc doctor --no-probe           # offline triage; skip the API HEAD request
jc doctor --probe-timeout 2s   # tighter timeout for fast-fail
```

What it reports:

- **Build** — version, Go runtime, OS/arch
- **Profile** — active profile and where it came from (`JC_PROFILE` env vs config)
- **Config** — resolved config file path + file/dir permissions
- **Auth** — API key source (`--api-key` flag / `JC_API_KEY` env / profile config / keychain reference) with a `****abcd` fingerprint, plus org ID source
- **API** — V1 and V2 base URLs, plus a probe that distinguishes `ok` / `auth_failed` / `unreachable` so you can tell "the key is wrong" from "the host is unreachable"
- **LLM** — `jc ask` provider, model, API key source + fingerprint
- **MCP** — step-up auth setting, signing setting, webhook URL, signing pubkey fingerprint

Secrets are **never** printed in full — only the last 4 chars (`****abcd`), matching the TTY step-up convention. JSON output exits 0 even when probes fail so callers parse the result rather than the exit code.

---

## Plan Mode & Safety

### Plan Mode

Add `--plan` to any mutation to preview what would happen without executing:

```bash
jc users create --username jdoe --email jdoe@acme.com --plan
jc users delete jdoe --plan
jc bulk users --file changes.csv --plan
jc recipe run offboard-user --param username=jdoe --plan
```

Plan mode makes no changes. It returns exit code 10 to distinguish from success (0) and errors (1).

### Safety Guardrails

- All destructive operations (delete, erase) require confirmation prompts
- `jc devices erase` requires both `--confirm-erase` flag AND interactive confirmation
- `--force` skips confirmations (for scripting — use responsibly)
- Service account tokens auto-refresh with a 30-second pre-expiry buffer
- API keys are redacted in `--verbose` and `--debug` output

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Usage error (invalid flags/args) |
| 3 | Authentication failure |
| 4 | Permission denied |
| 5 | Rate limited |
| 10 | Plan mode (no changes made) |
| 130 | Interrupted (Ctrl+C) |

---

## Development

Requires Go 1.25+.

```bash
git clone https://github.com/TheJumpCloud/jc-cli.git
cd jc-cli

make build                    # Build binary → ./jc
make test                     # Run all tests (unit)
make lint                     # Run go vet
make install                  # Install to $GOPATH/bin
make integration-test          # Full integration test (requires auth)
make integration-test-readonly # Read-only probes only (no create/delete)
make site                     # Regenerate docs/site/ from the schema manifest
make verify-site              # Fail if docs/site/ drifted from the schema
```

### Showcase site

The public command catalog at [thejumpcloud.github.io/jc-cli](https://thejumpcloud.github.io/jc-cli/) is generated from `internal/schema/schema.go` (the same source the `jc schema` command uses). After adding or renaming a command, run `make site` and commit the regenerated files under `docs/site/`. The `verify-site` CI gate fails the PR if you forget.

### Shell Completion

```bash
# Bash
jc completion bash > /etc/bash_completion.d/jc

# Zsh
jc completion zsh > "${fpath[1]}/_jc"

# Fish
jc completion fish > ~/.config/fish/completions/jc.fish
```

---

## Architecture

```
cmd/jc/main.go          Entry point
internal/
  cmd/                  CLI commands (Cobra) — 28 resource types + utilities
  api/                  HTTP clients — Client (base), V1Client, V2Client, InsightsClient
  output/               Format-agnostic output engine (JSON, table, CSV, YAML, NDJSON)
  config/               Viper-based configuration, profiles, env var bindings
  resolve/              Name-to-ID resolution with file-based caching
  filter/               Filter expression parser (field:op:value)
  recipe/               YAML recipe engine with Go templates
  tui/                  Interactive terminal UI (Bubbletea) — 28 resource views
  mcp/                  MCP server (official Go SDK) — 210 tools
  ask/                  LLM integration (Anthropic, OpenAI, Ollama)
  keychain/             OS keychain wrapper (macOS Keychain, Linux secret-tool)
  schema/               Machine-readable CLI schema (27 resource schemas)
  simulator/            Auth policy simulator (three-valued logic)
  plan/                 Plan mode rendering
  version/              Build-time version injection
```

---

## License

This project is a Community Software Tool initially developed by JumpCloud. It is offered as an open source project under the MIT License "as is" without warranty of any kind. JumpCloud does not commit to maintaining, updating, or providing support for this project. Please use the [GitHub issue tracker](https://github.com/TheJumpCloud/jc-cli/issues) for any issues.

See [LICENSE](LICENSE) for the full MIT License text.
