# Identity Providers Resource Design

## Context

JumpCloud Identity Providers API discovered via API probing:
- **Endpoint**: `GET/POST /api/v2/identity-providers`, `GET/PUT/DELETE /api/v2/identity-providers/{id}`
- **List format**: Wrapped `{"identityProviders": [...], "totalCount": N}` (not bare V2 array)
- **Types**: OIDC, GOOGLE, OKTA, AZURE — all use same `oidc` nested object
- **Structure**: `{id, name, type, oidc: {clientId, clientSecret, url}}`
- **clientSecret**: Write-only (returned empty on GET)
- **No PATCH** (404), no associations sub-endpoint
- **Max 1 per org** (409 on second create)
- **SAML**: Not API-accessible (schema validation fails)
- **Routing policies**: `/identity-provider/policies` exists but schema unclear

## Design Decisions

### Wrapped Response Handling

Add `ResponseKey` field to `V2ListOptions`:
```go
type V2ListOptions struct {
    Limit       int
    Sort        string
    Filter      []string
    Search      string
    ResponseKey string // Extract array from wrapped object (e.g., "identityProviders")
}
```

In `ListAll()`, after existing `{"results": [...]}` fallback, check ResponseKey:
```go
if opts.ResponseKey != "" {
    var obj map[string]json.RawMessage
    json.Unmarshal(body, &obj)
    json.Unmarshal(obj[opts.ResponseKey], &pageItems)
}
```

Also add `ResponseKey` to `ResourceConfig` in resolve package so the V2Resolver can pass it through.

### Flattening

Promote `oidc.clientId` → `clientId` and `oidc.url` → `url` at top level for display. Simple 2-field promotion — no FlattenFunc needed on TUI since we flatten in the command layer before output.

### CLI Commands

```
jc identity-providers list [--limit N]
jc identity-providers get <name-or-id>
jc identity-providers create --name NAME --type TYPE --client-id ID --client-secret SECRET --url URL
jc identity-providers update <name-or-id> [--name] [--type] [--client-id] [--client-secret] [--url]
jc identity-providers delete <name-or-id> [--force]
```

### Default Fields

`id, name, type, clientId, url`

### Schema

27th resource entry. FilterSupport: false, SortSupport: false, SortFields for client-side sort.

### MCP Tools

5 tools: `identity_providers_list/get/create/update/delete`. Total: 178.

### TUI

Add to Access category alongside auth-policies.

## File Changes

| File | Action |
|------|--------|
| `internal/api/v2.go` | Add ResponseKey to V2ListOptions, handle in ListAll |
| `internal/resolve/resolve.go` | Add ResponseKey to ResourceConfig, pass in resolveViaV2API |
| `internal/cmd/identity_providers.go` | New file — CLI commands |
| `internal/cmd/identity_providers_test.go` | New file — tests |
| `internal/schema/schema.go` | Add identity-providers entry + manifest |
| `internal/mcp/tools.go` | Register 5 tools |
| `internal/tui/registry.go` | Add display/endpoint/category entries |
| `internal/cmd/root.go` | Wire command + alias |
| Test count updates | schema_test (26→27), cmd/schema_test (26→27), mcp/tools_test (173→178) |
