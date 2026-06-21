---
title: Pre-flight input validation runs before the step-up gate
date: 2026-06-20
category: design-patterns
module: internal/mcp
tags: [mcp, auth, step-up, validation, ux]
applies_when:
  - "Adding a destructive MCP tool whose input has shape constraints (required fields, OS matrix, allowlists)"
  - "An operator complains they completed Touch ID only to get a 'missing field X' error"
  - "Considering whether to do input validation inside vs. before the handler"
---

# Pre-flight input validation runs before the step-up gate

## Context

`addTypedTool` runs the [step-up gate](step-up-auth-chokepoint-2026-05-08.md)
BEFORE the handler. So any input validation that lives inside the
handler — required-field checks, payload/OS-matrix checks, allowlist
enforcement — fires *after* the operator has already completed
Touch ID, TTY, or webhook approval.

Cursor Bugbot caught this on PR #60 for the
`apple_mdm_payloads_create_policy` tool: an iOS-only payload shipped
as a macOS policy got rejected, but only after the operator had
already approved the destructive prompt.

## Guidance

Use **`addTypedToolWithPreFlight`** (`internal/mcp/tools.go`) for any
destructive tool whose input can be invalid in ways the JSON-RPC
layer doesn't catch:

```go
addTypedToolWithPreFlight(s, "tool_name",
    "tool description",
    // preFlight: returns nil on success, non-nil error if the input
    // would fail anyway. Runs BEFORE step-up.
    func(args toolInput) error {
        if args.RequiredField == "" {
            return fmt.Errorf("'required_field' is required")
        }
        if !validOSMatrix(args.OS, args.PayloadType) {
            return fmt.Errorf("payload %q is not supported on %s", args.PayloadType, args.OS)
        }
        return nil
    },
    // handler — same shape as the addTypedTool handler.
    func(ctx, req, args) (*mcp.CallToolResult, any, error) { ... },
)
```

What goes in preFlight:

- **required fields not enforceable by JSON Schema `required`** —
  empty-string detection, semantic mismatch
- **cross-field consistency** — OS/payload matrix, mode/target
  combinations
- **read-only mode refusal** when `Execute: true` (because the
  operator getting "read-only mode" *after* approval is the same
  surprise as any other validation error)
- **lookup failures the agent can correct** — payload type doesn't
  exist, named resource not found

What stays in the handler:

- **API calls** — the gate's whole job is to authorize these
- **emission of the actual response shape**
- **anything whose failure mode is "the agent already did everything
  right; the network had a hiccup"** (retries, partial-success
  rollback)

## Alternatives considered

- **Do nothing — let it fail after the gate.** Rejected. For
  noop-step-up (the default) it's purely cosmetic, but for operators
  with `mcp.require_step_up_for_destructive=true` the UX paper cut
  is real: approve Touch ID, get "missing field". Once seen, it
  conditions agents to *not* trust the gate ("maybe I'd better do
  three rounds of preview first"), defeating the chokepoint's
  purpose.

- **Run validation inside the wrapper unconditionally on every
  tool.** Rejected — the wrapper has no way to know what each tool
  considers valid. The preFlight callback puts that knowledge where
  it belongs (next to the handler) without forcing every tool to
  carry one.

- **Lift validation to a `Validate() error` method on every input
  struct.** Rejected — couples the input type to the validation
  contract. Most tools' inputs are dumb data; a method forces every
  one of them to implement it. The optional preFlight callback is
  additive.

## See also

- [step-up auth chokepoint](step-up-auth-chokepoint-2026-05-08.md)
- PR #60 Bugbot finding 2 (the catch that motivated the helper)
