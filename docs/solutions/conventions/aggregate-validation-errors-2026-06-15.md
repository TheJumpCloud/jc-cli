---
title: Aggregate validation errors — every problem in one pass
date: 2026-06-15
category: conventions
module: internal/apple_mdm
tags: [validation, error-handling, ux]
applies_when:
  - "Validating a multi-payload / multi-row / multi-section config file"
  - "Considering whether to fail fast on the first error or collect all errors"
  - "Designing a CLI command that processes a user-supplied YAML / CSV / JSON"
---

# Aggregate validation errors — every problem in one pass

## Context

When an operator opens `corp-baseline.yaml` to author a multi-payload
Apple MDM bundle, they don't want a fix-one-discover-the-next iteration
cycle. A validator that fails on the first broken payload (then makes
the operator re-run to learn the second) trains the operator to
hand-walk their file before each run, which is exactly the cost the
validator was supposed to eliminate.

## Guidance

**Walk the entire input and aggregate every error into a single
returned message.** A typical validation loop:

```go
var problems []string
for i, payload := range cfg.Payloads {
    if err := validateOne(payload); err != nil {
        problems = append(problems,
            fmt.Sprintf("payloads[%d] (%s): %v", i, payload.Type, err))
    }
}
if len(problems) > 0 {
    return fmt.Errorf("validation failed:\n  - %s",
        strings.Join(problems, "\n  - "))
}
```

The error message must include:

- **Where in the file** — index, line number, or named section
- **What was being validated** — `payloads[2].values` not just
  "validation failed"
- **What went wrong** — the underlying error, not a wrapper that
  hides it

See `apple_mdm.ComposeConfig.BuildPayloadInstances` for the canonical
implementation (`internal/apple_mdm/compose.go`). The test
`TestBuildPayloadInstances_AggregatesValidationErrors` enforces the
contract: an input with two broken payloads must produce a message
mentioning *both*, not just the first.

This convention applies to:

- Compose YAML / config files (multi-payload, multi-section)
- CSV bulk operations (`jc bulk users`) — collect per-row failures
- Recipe validation (`jc recipe validate`) — collect per-step issues

## Alternatives considered

- **Fail fast on the first error.** Default Go idiom; rejected
  because the user-facing impact dominates the implementation
  simplicity. A multi-payload bundle with three errors costs the
  operator three save-and-rerun cycles instead of one.

- **Surface all errors as separate log lines, exit 1.** Rejected —
  the structured-output contract (`--output json`) wants ONE error
  envelope. Multiple `log.Error()` lines aren't programmatically
  consumable.

- **Validate during emission so each payload's emit-error surfaces
  alone.** Tried during the original implementation. Rejected —
  the implicit ordering ("first error wins") was hard to reason
  about, and `--plan` previews didn't fire until after the first
  payload validated. Explicit two-phase (validate-all, then
  emit-all-or-nothing) is clearer.

## See also

- KLA-451 multi-payload compose (the work that established the
  pattern)
