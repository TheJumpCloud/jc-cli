---
title: MCP App registration — appSpec (no-args) vs typed (parameterized)
date: 2026-05-30
category: design-patterns
module: internal/mcp
tags: [mcp, mcp-apps, registration, types]
applies_when:
  - "Adding a new MCP App to internal/mcp/"
  - "Deciding whether a new App takes parameters or is a fixed view"
  - "Wondering why insights_view / user_view / device_view live in apps_*.go while dashboard_view lives in appSpecs"
---

# MCP App registration — `appSpec` (no-args) vs typed (parameterized)

## Context

MCP Apps are tool+resource pairs that render HTML inline in MCP hosts
that support the `io.modelcontextprotocol/ui` extension. Two
registration shapes exist for historical and pragmatic reasons:

- **`appSpec`** entries (in `internal/mcp/apps.go`'s `appSpecs` slice)
  — for apps that take **no arguments**. The handler returns a
  fixed shape (e.g. a dashboard snapshot).
- **`registerXView()` calls** (in `internal/mcp/apps_*.go`,
  one file per App) — for apps that take **typed parameters**
  via `addToolWithMetaTyped[In any]`.

The split isn't arbitrary; the typed apps need a generic input
parameter that the no-args wrapper can't provide.

## Guidance

**No parameters?** Add an entry to `appSpecs` in
`internal/mcp/apps.go`. The wrapper handles MCP-tool registration,
resource registration, and JSON-marshaling.

```go
{Name: "dashboard_view", Description: "...", ResourceURI: dashboardResourceURI,
 Handler: func(ctx context.Context) (any, error) { ... }},
```

**Has parameters?** Create `internal/mcp/apps_<name>.go` and an
`internal/mcp/apps_<name>_test.go`. Define an input struct (e.g.
`userViewArgs{User string}`), a fetch function, and a registration
function:

```go
func (s *Server) registerUserView() {
    addToolWithMetaTyped(s, "user_view", "...",
        mcp.Meta{"ui": map[string]any{"resourceUri": userViewResourceURI}},
        func(ctx context.Context, req *mcp.CallToolRequest, args userViewArgs) (*mcp.CallToolResult, any, error) {
            data, err := fetchUserViewData(ctx, args)
            ...
        },
    )
    s.registerAppResource(appSpec{Name: "user_view", ResourceURI: userViewResourceURI})
}
```

Then call it from `registerAppTools()` in `apps.go`:

```go
s.registerInsightsView()
s.registerUserView()
s.registerDeviceView()
s.registerAppleMDMPayloadsTools()  // KLA-452 — multi-tool app
```

**Multi-tool app variant** (KLA-452 `apple_mdm_payloads_*` is the
canonical example): a single `registerXTools()` function registers
multiple related tools that share fetch/validate helpers. Use this
when one logical "app" surfaces 3-4 tools that the agent picks
between based on intent (search → show → template → create_policy).

## Alternatives considered

- **One shape only — all apps go through the typed path.** Considered
  during KLA-419. Rejected because the no-args dashboard / compliance
  / recipe-runner views became significantly noisier (a `struct{}`
  input, a generic instantiation, an extra registration function each)
  with no offsetting benefit.

- **Auto-generate apps from a YAML config.** Rejected — half the
  apps want bespoke fetch logic (insights querying, MCX variant
  disambiguation), which the YAML would have to either encode poorly
  or escape-hatch into Go callbacks. The escape hatch is the typed
  path; we'd just have re-invented it.

- **Skip the `appSpec` slice and just hand-register every app.**
  The fixed-shape apps benefit from the centralized loop in
  `registerAppTools()` — it's where the resource side gets registered,
  where audit metadata gets attached, etc. Removing the slice would
  duplicate that loop into every fixed-shape app.

## See also

- KLA-419 typed-app helper (the work that established `addToolWithMetaTyped`)
- `internal/mcp/apps.go` (registration entry points)
- `internal/mcp/apps_user.go`, `apps_device.go`, `apps_insights.go`
  (typed examples)
- `internal/mcp/apps_apple_mdm_payloads.go` (multi-tool app example
  from KLA-452)
