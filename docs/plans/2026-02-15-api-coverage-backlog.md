# JumpCloud API Coverage Backlog

> **Current state:** 23 resource schemas, 145 MCP tools, ~85% API surface coverage.
> **Goal:** Close remaining gaps by priority tier.

---

## Tier 1 — High Value (Daily IT Admin Use)

| # | Resource | API | Endpoints | Status |
|---|----------|-----|-----------|--------|
| 1 | Google Workspace (G Suite) | V2 | `/gsuites`, `/gsuites/{id}`, associations, translation rules, user import | DONE |
| 2 | Microsoft 365 (Office 365) | V2 | `/office365s`, `/office365s/{id}`, associations, translation rules, user import | DONE |
| 3 | Duo MFA | V2 | `/duo/accounts`, `/duo/accounts/{id}/applications` | DONE |
| 4 | Software App Status & Associations | V2 | `/softwareapps/{id}/statuses`, `/softwareapps/{id}/associations`, license reclaim | DONE |

**Impact:** Covers directory integrations that mid-to-large orgs use daily. Completing Tier 1 brings coverage to ~85%.

---

## Tier 2 — Medium Value (Periodic Use)

| # | Resource | API | Endpoints | Status |
|---|----------|-----|-----------|--------|
| 5 | Workday Import | V2 | `/workdays`, auth, import, workers, results | NOT STARTED |
| 6 | Custom Email Templates | V2 | `/custom-email-templates`, `/custom-email-configurations` | NOT STARTED |
| 7 | Command Triggers (Webhooks) | V1 | `/command/trigger/{triggername}` | NOT STARTED |
| 8 | Samba Domains | V2 | `/ldapservers/{id}/sambadomains` | NOT STARTED |
| 9 | Application Templates | V1 | `/application-templates` | NOT STARTED |

**Impact:** Automation, branding, and specialized integrations. Completing Tier 2 brings coverage to ~92%.

---

## Tier 3 — Lower Value (Niche/Specialized)

| # | Resource | API | Endpoints | Status |
|---|----------|-----|-----------|--------|
| 10 | MSP/Provider Management | V2 | `/providers/{id}/administrators`, `/providers/{id}/organizations` | NOT STARTED |
| 11 | Directories Listing | V2 | `/directories` | NOT STARTED |
| 12 | Enrollment Profiles | V2 | `/enrollment-profiles` | NOT STARTED |
| 13 | Org Crypto Settings | V1 | `/organizations/{id}/crypto` | NOT STARTED |

**Impact:** Only relevant to MSPs or very specific configurations. Completing Tier 3 brings coverage to ~98%.

---

## Design Notes

### Item 1: Google Workspace (G Suite)
```
jc gsuite list
jc gsuite get <name-or-id>
jc gsuite update <name-or-id> [--name ...]
jc gsuite translation-rules <name-or-id>          # list translation rules
jc gsuite translation-rule-add <name-or-id> ...    # add translation rule
jc gsuite translation-rule-delete <name-or-id> --rule-id <id>
jc gsuite import-users <name-or-id>                # list importable users
```
- V2 endpoints: `/gsuites`, `/gsuites/{id}`, `/gsuites/{id}/translationrules`, `/gsuites/{id}/import/users`
- Associations via existing graph command
- Read-heavy (most config done in JumpCloud console); CLI value is in listing, auditing, and import
- Resolver: `GsuiteConfig{CacheKey:"gsuites", ListEndpoint:"/gsuites", NameField:"name", IDField:"id"}`
- Default fields: id, name, defaultDomain

### Item 2: Microsoft 365 (Office 365)
```
jc office365 list
jc office365 get <name-or-id>
jc office365 update <name-or-id> [--name ...]
jc office365 translation-rules <name-or-id>
jc office365 translation-rule-add <name-or-id> ...
jc office365 translation-rule-delete <name-or-id> --rule-id <id>
jc office365 import-users <name-or-id>
```
- V2 endpoints: `/office365s`, `/office365s/{id}`, `/office365s/{id}/translationrules`, `/office365s/{id}/import/users`
- Nearly identical pattern to G Suite — can share translation rule helpers
- Resolver: `Office365Config{CacheKey:"office365", ListEndpoint:"/office365s", NameField:"name", IDField:"id"}`
- Default fields: id, name, defaultDomain

### Item 3: Duo MFA
```
jc duo list                                         # list Duo accounts
jc duo get <id>                                     # get Duo account
jc duo create --name <name>                         # create Duo account
jc duo delete <id> [--force]                        # delete Duo account
jc duo apps <account-id>                            # list Duo applications
jc duo app-get <account-id> --app-id <id>           # get Duo application
jc duo app-create <account-id> --name <name> ...    # create Duo application
jc duo app-delete <account-id> --app-id <id> [--force]
```
- V2 endpoints: `/duo/accounts`, `/duo/accounts/{id}`, `/duo/accounts/{id}/applications`, `/duo/accounts/{account_id}/applications/{id}`
- Two-level resource: accounts → applications
- Resolver: `DuoAccountConfig{CacheKey:"duo", ListEndpoint:"/duo/accounts", NameField:"name", IDField:"id"}`
- Default fields (accounts): id, name; (applications): id, name, apiHost

### Item 4: Software App Status & Associations
```
jc software statuses <name-or-id>                   # list deployment statuses per device
jc software associations <name-or-id>               # list associations (systems, groups)
jc software reclaim-license <name-or-id> --device <hostname-or-id>
```
- V2 endpoints: `/softwareapps/{id}/statuses`, `/softwareapps/{id}/associations`, license reclaim
- Extends existing `software.go` — no new command file needed
- Uses existing `SoftwareConfig` resolver
- Default fields (statuses): deviceId, status, lastUpdate; (associations): to.id, to.type

---

## Completion Log

| Date | Items Completed | Tools Added | Coverage |
|------|----------------|-------------|----------|
| 2026-02-15 | Backlog created | — | ~65-70% |
| 2026-02-15 | Tier 1: G Suite, Office 365, Duo, Software extensions | +19 (126→145) | ~85% |
