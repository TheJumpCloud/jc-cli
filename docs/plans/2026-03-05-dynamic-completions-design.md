# Dynamic Shell Completions Design

## Overview

Add tab-completion for resource name/ID arguments across all commands that accept a `<name-or-id>` positional arg. Uses the existing resolver cache for zero-latency, offline-capable completions.

## Approach

Cache-based completions. When a user types `jc users get <TAB>`, read the resolver cache file (e.g., `~/.cache/jc/users.json`) and offer cached name→ID pairs as completion candidates. No API calls during completion.

### Why Cache-Based

- Shell completions must be fast (<50ms). API calls take 500ms-2s.
- Cache is already populated by normal CLI usage (`list`, `get`, `resolve`).
- Works offline, no auth required at completion time.
- Empty cache = no completions (not errors). Cache fills naturally with usage.

## Architecture

### New Shared Function

A single `completeResourceNames(cfg resolve.ResourceConfig)` function in `internal/cmd/completions.go` returns a `cobra.ValidArgsFunction` that:

1. Reads the resolver cache file for the given `ResourceConfig`
2. Returns all cached names AND IDs as completion candidates
3. Names show ID as description; IDs show name as description
4. Uses `cobra.ShellCompDirectiveNoFileComp` to suppress filesystem completions
5. Returns empty list if cache file is missing or empty (no errors)
6. Returns nothing if positional arg already provided (`len(args) > 0`)

### Cache Format

Existing cache files at `~/.cache/jc/<cacheKey>.json`:

```json
{
  "jdoe": { "id": "507f1f77bcf86cd799439011", "timestamp": "2026-03-05T15:07:16Z" },
  "admin": { "id": "bbb222bbb222bbb222bbb222", "timestamp": "2026-03-05T15:07:16Z" }
}
```

### Completion Output

```
jdoe    507f1f77bcf86cd799439011
admin   bbb222bbb222bbb222bbb222
507f1f77bcf86cd799439011   jdoe
bbb222bbb222bbb222bbb222   admin
```

### Public API Addition

Expose `resolve.ReadCacheEntries(cacheKey string) map[string]CacheEntry` as a standalone function (no `Resolver` or client needed). `CacheEntry` exported with `ID` and `Timestamp` fields.

## Scope

### Resources with completions (20 resources, ~80 commands)

| Resource | Config | Commands |
|----------|--------|----------|
| users | `UserConfig` | get, update, delete, lock, unlock, reset-mfa, reset-password, ssh-keys |
| devices | `DeviceConfig` | get, update, delete, lock, restart, erase, fde-key |
| groups user | `UserGroupConfig` | get, update, delete, add-member, remove-member |
| groups device | `DeviceGroupConfig` | get, update, delete, add-member, remove-member |
| commands | `CommandConfig` | get, update, delete, results |
| policies | `PolicyConfig` | get, update, delete, results |
| apps | `ApplicationConfig` | get, update, delete |
| auth-policies | `AuthPolicyConfig` | get, update, delete, enable, disable |
| iplists | `IPListConfig` | get, update, delete |
| software | `SoftwareAppConfig` | get, update, delete, statuses, associations, reclaim-license |
| ldap | `LDAPServerConfig` | get, update, delete, samba-domains |
| radius | `RADIUSServerConfig` | get, update, delete |
| ad | `ActiveDirectoryConfig` | get, update, delete |
| gsuite | `GsuiteConfig` | get, update, delete |
| office365 | `Office365Config` | get, update, delete |
| duo | `DuoAccountConfig` | get, update, delete |
| apple-mdm | `AppleMDMConfig` | get, update, delete, enrollment-profiles, devices |
| policy-groups | `PolicyGroupConfig` | get, update, delete |
| identity-providers | `IdentityProviderConfig` | get, update, delete |
| saas-management | `SaaSManagementConfig` | get, delete, accounts, usage, licenses |

### Resources without completions

- `access-requests` — uses UUID `accessId`, no human-readable name
- `admins` — could add but low usage
- `org` — singleton, no need
- `graph` — takes source type, not name
- `insights` — query-based, not name-based (saved searches already have completions)
- `custom-emails` — keyed by type enum (already validated)
- `system-insights` — table name as arg (already validated)
- `assets` — nested field names, non-standard resolver
- `user-states` — no name field

## Files to Modify

1. `internal/resolve/resolve.go` — export `CacheEntry` type and `ReadCacheEntries()` function
2. `internal/cmd/completions.go` — new file, shared `completeResourceNames()` function
3. `internal/cmd/completions_test.go` — new file, tests with temp cache
4. 20 resource command files — add `ValidArgsFunction` to each subcommand that takes `<name-or-id>`

## Testing

- Unit test `completeResourceNames` with mock cache directory
- Verify both names and IDs appear in completions
- Verify `ShellCompDirectiveNoFileComp` is always returned
- Verify no completions when arg already provided
- Verify empty/missing cache returns empty list
- Verify `ReadCacheEntries` handles missing file, corrupt file, empty file

## Counts After

- No new schema resources, MCP tools, or TUI changes
- ~80 commands gain tab completion
