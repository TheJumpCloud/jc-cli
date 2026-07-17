---
title: Step-up auth as a single chokepoint via reflection on Execute bool
date: 2026-05-08
category: design-patterns
module: internal/mcp
tags: [mcp, auth, step-up, destructive-ops, reflection]
applies_when:
  - "Adding a new destructive MCP tool"
  - "Designing a single-point authorization gate that needs to cover dozens of handler types"
  - "Considering whether to add per-handler step-up calls"
---

# Step-up auth as a single chokepoint via reflection on `Execute bool`

## Context

The MCP server exposes ~30 destructive tools (`users_delete`,
`devices_erase`, `policies_delete`, …). Each one needs the same
authorization gate when the operator opted into
`mcp.require_step_up_for_destructive`. The naive design — call
`stepUp.authorize(...)` inside each handler — produces 30 chokepoints
to keep consistent. The next time the gate's contract changes
(KLA-413 added webhook authenticators with channel-aware remediation
text), 30 sites need synchronous updates.

## Guidance

**One chokepoint in `addTypedTool`** (`internal/mcp/tools.go:5285`).
The wrapper reflects on the tool's input struct: if it carries a bool
field literally named `Execute` and that field is `true`, the gate
fires before the handler runs.

```go
if isExecutingDestructive(args) {
    if err := s.stepUp.authorize(ctx, name, destructiveTarget(args)); err != nil {
        return errorResult(...), nil, nil
    }
}
```

`isExecutingDestructive` is defined in `internal/mcp/stepup.go:18`.
It accepts `any`, uses `reflect.FieldByName("Execute")`, and returns
`false` for any input type that doesn't carry the field — so
read-only tools are unaffected and pay no reflection cost when
parsing args.

To make a new tool destructive: add `Execute bool` to its input
struct (and a corresponding `"Set to true to actually …"` schema
description). The wrapper does the rest.

## Alternatives considered

- **Per-handler `s.stepUp.authorize(...)` calls.** Rejected — the
  PR #34 regression where a webhook denial served TTY remediation
  text was caught by a single test that found the bug at the
  chokepoint. With 30 call sites the regression would have been
  per-tool and per-channel, much harder to find.

- **Cobra annotations on the underlying CLI command** (the
  KLA-444 approach for the *CLI* destructive-op gate). Rejected
  for MCP because MCP tools are hand-written RPC handlers, not
  1:1 with Cobra commands — see
  [parallel-mcp-and-cli-taxonomies](../conventions/parallel-mcp-and-cli-taxonomies-2026-06-20.md).

- **A `Destructive() bool` interface method on every input.**
  Rejected because it forces every input struct to implement an
  interface even when most are no-op pass-throughs. The `Execute`
  field is already meaningful at the JSON-schema layer (the agent
  needs to know the field exists to call the destructive path);
  piggybacking the reflection check on that field is zero added
  surface area.

## See also

- [pre-flight before step-up](pre-flight-before-step-up-2026-06-20.md)
  — runs validation BEFORE the gate so input errors don't cost the
  operator a Touch ID
- KLA-408 trilogy (KLA-408 → KLA-413 → KLA-419) for the historical
  build-out
