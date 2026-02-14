# Tier 2 API Coverage Design

**Goal:** Add CLI commands for Software Management, LDAP Servers, Active Directory, and Organization Settings.

**Architecture:** All resources follow established V2 CRUD patterns (iplists.go). Organizations use V1 read-only pattern. Each resource gets its own file with standard list/get/create/update/delete subcommands, resolver config, schema entry, MCP tools, and tests.

**API shapes** (probed against live org):
- Software Apps (`/v2/softwareapps`): V2, fields: id, displayName, settings (nested array), createdAt, updatedAt
- LDAP Servers (`/v2/ldapservers`): V2, fields: id, name, userLockoutAction, userPasswordExpirationAction
- Active Directory (`/v2/activedirectories`): V2, fields: id, domain, useCase, groupsEnabled, delegationState, permission, primaryAgent, primaryImportAgent, updatedAt
- Organizations (`/v1/organizations`): V1, fields: _id, id, displayName, created, logoUrl

---

## 1. Software Management (`/v2/softwareapps`)

Full V2 CRUD. Command name: `jc software`.

**Default fields**: `id, displayName, createdAt, updatedAt`

**Commands**:
- `jc software list [--limit N] [--sort field] [--filter expr]`
- `jc software get <name-or-id>`
- `jc software create --name <name> --settings <json>`
- `jc software update <name-or-id> [--name <name>] [--settings <json>]`
- `jc software delete <name-or-id> [--force]`

`settings` is a complex nested array of package configs (packageId, packageManager, desiredState, location, etc.). Accept as raw JSON via `--settings` flag, same pattern as `--config` on apps and `--conditions` on auth-policies.

**Resolver**: `SoftwareAppConfig` with `displayName` as NameField, `id` as IDField, cache key `softwareapps`, endpoint `/softwareapps`.

**Files**: `internal/cmd/software.go` (~300 lines), `internal/cmd/software_test.go` (~350 lines)

## 2. LDAP Servers (`/v2/ldapservers`)

Full V2 CRUD. Command name: `jc ldap`.

**Default fields**: `id, name, userLockoutAction, userPasswordExpirationAction`

**Commands**:
- `jc ldap list [--limit N] [--sort field] [--filter expr]`
- `jc ldap get <name-or-id>`
- `jc ldap create --name <name> [--user-lockout-action <action>] [--user-password-expiration-action <action>]`
- `jc ldap update <name-or-id> [--name <name>] [--user-lockout-action <action>] [--user-password-expiration-action <action>]`
- `jc ldap delete <name-or-id> [--force]`

Simple resource with only 4 fields. Straightforward V2 CRUD.

**Resolver**: `LDAPServerConfig` with `name` as NameField, `id` as IDField, cache key `ldapservers`, endpoint `/ldapservers`.

**Files**: `internal/cmd/ldap.go` (~250 lines), `internal/cmd/ldap_test.go` (~300 lines)

## 3. Active Directory (`/v2/activedirectories`)

Full V2 CRUD. Command name: `jc ad`.

**Default fields**: `id, domain, useCase, groupsEnabled, delegationState`

**Commands**:
- `jc ad list [--limit N] [--sort field] [--filter expr]`
- `jc ad get <domain-or-id>`
- `jc ad create --domain <domain> [--use-case <case>]`
- `jc ad update <domain-or-id> [--use-case <case>] [--groups-enabled]`
- `jc ad delete <domain-or-id> [--force]`

AD uses `domain` as the human-readable identifier (not `name`).

**Resolver**: `ActiveDirectoryConfig` with `domain` as NameField, `id` as IDField, cache key `activedirectories`, endpoint `/activedirectories`.

**Files**: `internal/cmd/ad.go` (~280 lines), `internal/cmd/ad_test.go` (~320 lines)

## 4. Organizations (`/v1/organizations`)

Read-only (list + get). Command name: `jc org`.

**Default fields**: `_id, displayName, created`

**Commands**:
- `jc org list`
- `jc org get [org-id]`

No create/update/delete — organizations are account-level resources. Single-tenant (typically 1 org). No resolver needed; `get` takes an optional ID argument.

**Files**: `internal/cmd/org.go` (~120 lines), `internal/cmd/org_test.go` (~150 lines)

## 5. Supporting Changes

| File | Change |
|------|--------|
| `internal/resolve/resolve.go` | Add `SoftwareAppConfig`, `LDAPServerConfig`, `ActiveDirectoryConfig` |
| `internal/cmd/cli_error.go` | Add `SOFTWARE_NOT_FOUND`, `LDAP_NOT_FOUND`, `AD_NOT_FOUND` error codes |
| `internal/schema/schema.go` | Add `software`, `ldap`, `ad`, `org` resource and command entries |
| `internal/mcp/tools.go` | Register MCP tools for all 4 resources |
| `internal/cmd/root.go` | Wire `newSoftwareCmd()`, `newLDAPCmd()`, `newADCmd()`, `newOrgCmd()` |

## Implementation Order

1. **Software Management** — most complex (nested settings), good to tackle first
2. **LDAP Servers** — simplest V2 CRUD
3. **Active Directory** — uses `domain` instead of `name`, slight pattern variation
4. **Organizations** — V1 read-only, simplest overall
5. **Schema + MCP + wiring** — final integration

Each resource is independently testable. Resources 1-3 can be built in parallel (all V2, no shared state).

## Estimated Scope

~2,500 lines across 8 new files + 5 modified files. Similar scale to Tier 1 (2,200 lines).
