# Access Requests Resource Design

## Overview

Add access-requests as a CLI resource for managing temporary elevated device privileges in JumpCloud. Access requests grant users temporary admin/sudo access on specific devices, with automatic revocation at expiry.

## API

V2 at `/accessrequests`. Five operations:

| Method | Endpoint | Purpose |
|--------|----------|---------|
| GET | `/accessrequests` | List all requests |
| POST | `/accessrequests` | Create request |
| GET | `/accessrequests/{accessId}` | Get by accessId |
| PUT | `/accessrequests/{accessId}` | Update (extend expiry) |
| POST | `/accessrequests/{accessId}/revoke` | Revoke early |

### Create Body

```json
{
  "requestorId": "<userId>",
  "resourceId": "<deviceId>",
  "resourceType": "device",
  "remarks": "string",
  "expiry": "2026-03-01T00:00:00Z",
  "additionalAttributes": {
    "sudo": { "enabled": true, "withoutPassword": false }
  }
}
```

### Response Fields

`id`, `accessId`, `requestorId`, `resourceId`, `resourceType`, `accessState`, `expiry`, `remarks`, `operationId`, `additionalAttributes`, `duration`, `createdBy`, `updatedBy`, `version`, `companyId`, `tempGroupId`, `jobId`, `metadata`

## CLI Design

### Commands

```
jc access-requests list    [--filter ...] [--limit N]
jc access-requests get     <accessId>
jc access-requests create  --user <id|name> --device <id|name> --expiry <RFC3339> [--sudo] [--sudo-nopasswd] [--remarks ...]
jc access-requests update  <accessId> [--expiry <RFC3339>] [--remarks ...]
jc access-requests revoke  <accessId> [--force]
```

Alias: `ar`

### Default Fields

`accessId`, `requestorId`, `resourceId`, `accessState`, `expiry`

### Key Decisions

1. **No resolver** — access requests use UUID `accessId`, no human-readable name field
2. **Name resolution on create** — `--user` resolves via `UserConfig`, `--device` via `DeviceConfig`
3. **Revoke = POST action** — not DELETE; confirmation-gated with `--force`
4. **Sudo flags** — `--sudo` sets `additionalAttributes.sudo.enabled=true`; `--sudo-nopasswd` adds `withoutPassword=true`

## MCP Tools

5 tools: `access_requests_list`, `access_requests_get`, `access_requests_create`, `access_requests_update`, `access_requests_revoke`. Plan-first safety on mutations.

## TUI

Promote placeholder to active entry in `CategoryAccess`. Read-only `ListScreen`/`DetailScreen` via schema.

## Counts After

- 29 schema resources (was 28)
- 194 MCP tools (was 189)

## Files to Modify

1. `internal/cmd/access_requests.go` — new, 5 subcommands
2. `internal/cmd/access_requests_test.go` — new, mock server tests
3. `internal/schema/schema.go` — Resources map + BuildCommandManifest
4. `internal/mcp/tools.go` — register 5 tools
5. `internal/cmd/root.go` — AddCommand + builtinCommands alias
6. `internal/tui/registry.go` — promote placeholder
7. Test counts: `schema_test.go` (x2), `tools_test.go` (x1)
