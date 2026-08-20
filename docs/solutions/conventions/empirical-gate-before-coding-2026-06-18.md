---
title: Empirical gate — hit the live tenant before writing the emitter
date: 2026-06-18
category: conventions
module: internal/apple_mdm
tags: [api-integration, jumpcloud, wire-format, debugging]
applies_when:
  - "Implementing a new write path against the JumpCloud API"
  - "Adapting an existing emitter to a new platform / template family / API surface"
  - "The wire shape isn't fully documented and the swagger is incomplete"
---

# Empirical gate — hit the live tenant before writing the emitter

## Context

The JumpCloud Custom MDM Configuration Profile API's wire shape
isn't fully captured by the swagger. The `values` array of
`{configFieldID, value}` pairs is generic; what fields each template
actually carries — and which are required — is a per-template runtime
fact only the tenant knows.

KLA-450 (iOS Custom MDM Profile support) shipped quickly because the
KLA-449 emitter had been written to *omit* a `values` entry whose
configFieldID was empty. That was speculative defensive code at write
time. It turned out to be load-bearing: the iOS Custom MDM template
in production ships with ONLY a `payload` field — no
`redispatchPolicy` field at all.

## Guidance

**Before writing or modifying any JC-API emitter, hit the live tenant
with a one-line `curl` (or `gh api`-like) call to confirm the wire
shape.** A 30-second probe answers questions that an hour of swagger
reading won't:

- Which fields actually appear on this template family today?
- Which are required vs. optional vs. ignored?
- Does the server accept `null`, omit the key, or both?
- Does the wire shape match the documented schema, or does the
  server tolerate (silently or with warnings) drift?

Concretely, for a new write path:

```bash
# 1. List the templates for the family you're targeting.
jc apple-mdm payloads template list --os ios -o json | jq '.[] | {id, name}'

# 2. Pull a single template's configFields.
jc api get /policytemplates/<id> | jq '.configFields[] | {id, name, required}'

# 3. Test the smallest write that succeeds, by hand.
jc api post /policies -d '{...minimal body...}' | jq .
```

Codify the discovery: write a test that hits a stubbed `httptest.Server`
with the *empirically observed* shape, not the documented one. See
`TestResolveCustomMDMTemplate_IOSHasNoRedispatch`
(`internal/apple_mdm/jumpcloud_test.go`) — its docstring records
"Confirmed live against the user's tenant on 2026-06-20: the iOS
Custom MDM template ships with ONLY a `payload` configField — no
`redispatchPolicy`."

## Alternatives considered

- **Write the emitter against the swagger; ship; iterate on bug
  reports.** Rejected — the iOS template shape would have produced
  a 400-class error on every `create-policy` call, no actionable
  error message, and an Apple-MDM-specific debugging tour for the
  first user to try it.

- **Add a `--verify-template` flag that probes the tenant before
  emission.** Rejected for the inverse reason — that's a per-user
  cost paid forever, when a one-time empirical gate during
  development closes the gap permanently.

- **Trust the documented schema, add aggressive defaults.** Tried
  during the KLA-449 work for the `redispatchPolicy` field
  (defaulted to `false`). It worked on macOS by accident; iOS
  would have rejected the entry whose `configFieldID` was empty
  if the original code hadn't preemptively dropped empty entries.

## See also

- KLA-450 (iOS support — landed because KLA-449 had the speculative
  empty-entry drop already in place)
- `TestBuildCustomMDMPolicyBody_OmitsRedispatchWhenFieldMissing`
  (the regression guard)
