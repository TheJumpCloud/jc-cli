# Tier 1 API Coverage Gaps — Design

## Context

The JC CLI covers 12 resource types but several have incomplete CRUD. This design addresses the 5 highest-value gaps — all extending existing files with established patterns.

## 1. Policies — create/update/delete

**Current**: list, get, results (read-only V2 at `/policies`)

- `jc policies create --name <name> --template-id <id> [--values <json>]` — templateID required; values for template-specific config as raw JSON
- `jc policies update <name-or-id> [--name <name>] [--values <json>]` — partial update via PUT
- `jc policies delete <name-or-id> [--force]` — confirmation prompt, plan mode

Follows iplists.go V2 CRUD pattern.

## 2. Graph — bind/unbind

**Current**: traverse (read-only GET)

V2 graph POST body: `{"op": "add|remove", "type": "<target_type>", "id": "<target_id>"}`

- `jc graph bind --from <type>:<name-or-id> --to <type>:<name-or-id>` — creates association
- `jc graph unbind --from <type>:<name-or-id> --to <type>:<name-or-id>` — removes association, confirmation unless --force

Both sides need type:identifier since we resolve both IDs. Same source/target validation as traverse.

## 3. Admins — get/create/update/delete

**Current**: list only (V1 `/users` endpoint)

- `jc admins get <email-or-id>` — resolve via email (AdminConfig in resolve package)
- `jc admins create --email <email> [--role <role>] [--enable-mfa]` — sends activation email
- `jc admins update <email-or-id> [--role <role>] [--enable-mfa/--disable-mfa]`
- `jc admins delete <email-or-id> [--force]` — confirmation prompt

New AdminConfig: CacheKey `admins`, ListEndpoint `/users`, NameField `email`, IDField `_id`.

## 4. Devices — update/search

**Current**: list, get, delete, lock, restart, erase

- `jc devices update <hostname-or-id> [--displayName <name>] [--allowSshPasswordAuthentication] [--allowMultiFactorAuthentication] [--allowPublicKeyAuthentication]` — V1 PUT
- `jc devices search <query>` — V1 POST `/search/systems`, mirrors user search

## 5. Apps — create/update/delete

**Current**: list, get (V1 `/applications`)

- `jc apps create --name <name> --sso-type <type> [--config <json>]` — ssoType required
- `jc apps update <name-or-id> [--name <name>] [--config <json>]` — partial update via PUT
- `jc apps delete <name-or-id> [--force]` — confirmation prompt

## Files Changed

All modifications to existing files — no new files.

| File | Change |
|------|--------|
| `internal/cmd/policies.go` | create/update/delete |
| `internal/cmd/graph.go` | bind/unbind |
| `internal/cmd/admins.go` | get/create/update/delete |
| `internal/cmd/devices.go` | update/search |
| `internal/cmd/apps.go` | create/update/delete |
| `internal/cmd/*_test.go` | Tests for all new commands |
| `internal/resolve/resolve.go` | AdminConfig |
| `internal/schema/schema.go` | Update verbs |
| `internal/cmd/cli_error.go` | ErrCodeAdminNotFound |
