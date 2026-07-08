---
title: JumpCloud custom-policy templates are OS-agnostic — OS-specificity lives in the catalog + value shape
date: 2026-07-08
category: design-patterns
module: internal/windows_mdm
tags: [jumpcloud-api, policies, mdm, windows, apple, templates]
applies_when:
  - "Adding policy-creation support for a new OS family or JumpCloud custom template"
  - "Deciding whether a new MDM feature needs internal/api changes"
  - "Wondering why internal/apple_mdm and internal/windows_mdm share no code but look structurally identical"
---

# JumpCloud custom-policy templates are OS-agnostic — OS-specificity lives in the catalog + value shape

## Context

The Apple MDM arc (KLA-449..452) and the Windows MDM passthrough
(KLA-459) both create JumpCloud policies from "custom" policy
templates. The load-bearing discovery, confirmed twice against a live
tenant (2026-06-18 for Apple, 2026-07-08 for Windows):

**JumpCloud's policy-creation path is completely OS-agnostic.** Every
policy — built-in or custom, macOS or Windows — is
`POST /api/v2/policies` with:

```json
{"name": "...", "template": {"id": "<policytemplate ObjectID>"}, "values": [
  {"configFieldID": "...", "configFieldName": "...", "value": ...}
]}
```

The only OS-specific parts are (a) *which* template you resolve and
(b) the *shape of `value`* inside each configField entry:

| Template | configField | value shape |
|---|---|---|
| `custom_mdm_profile_darwin` / `_ios` | `payload` | **scalar** — base64 of the `.mobileconfig` XML |
| `custom_oma_uri_mdm_windows` | `uriList` | **array** of `{uri, format, value}` triples |
| `custom_registry_keys_policy_windows` | `customRegTable` | **array** of `{customLocation, customValueName, customRegType, customData}` rows |

## Guidance

**Adding a new OS family / custom template requires zero
`internal/api` changes.** The recipe (see `internal/apple_mdm/jumpcloud.go`
and `internal/windows_mdm/jumpcloud.go` for the two implementations):

1. Resolve the template **by name** via
   `ListAll("/policytemplates", filter name:eq:<name>)`, then
   `Get("/policytemplates/<id>")` for the configFields (the list
   response omits them). **Never hardcode configField IDs** — match by
   `configFields[].name`. IDs could rotate; names are the contract.
2. Return an actionable error when the template is missing — that's
   what a tenant without the relevant MDM enablement looks like
   ("no policy template named X — is Windows MDM enabled for this org?").
3. Build the body with the generic `{name, template.id, values[]}`
   shape; put the OS-specific artifact in `value`.
4. Check the `value` shape empirically (`jc policy-templates get <id>`
   → `configFields[].defaultValue`) before coding — the Apple side is
   a scalar, the Windows side is an array; guessing wrong produces a
   policy the Admin Portal can't render.

**Where the effort actually goes:** the discovery/authoring surface.
Apple needed a vendored schema catalog + plist emitter before a valid
payload could exist at all. Windows needed neither — arbitrary
OMA-URIs are accepted, so the passthrough shipped without a catalog
(KLA-459) and the catalog is an additive follow-up (KLA-460). When
scoping a new family, ask "can the operator hand-author a valid value
entry?" — if yes, ship the passthrough first.

## Alternatives considered

- **A shared `internal/mdm_policy` package abstracting both.**
  Rejected for now — the resolve-and-build code is ~150 lines per
  family and the two `value` shapes (scalar vs array) plus the Apple
  side's redispatch-field special case would force the abstraction to
  carry more conditional surface than the duplication it removes.
  Revisit if a third family lands.

- **Hardcoding the template/configField IDs from the empirical gate.**
  Rejected in KLA-449 and re-rejected in KLA-459 — IDs are per-catalog
  MongoDB ObjectIDs that JumpCloud could refresh, and name-resolution
  gives the "is this enabled on your tenant?" error for free.
  `TestNoHardcodedConfigFieldIDs` (internal/windows_mdm) guards it.

- **Extending `jc policies create --template-id --values` instead of
  dedicated commands.** The generic path already works (it's the same
  wire shape), but it pushes the entire validation burden (format
  enums, OMA-URI path shape, hive-prefix trap) onto the operator.
  The dedicated commands exist to validate *before* the POST.

## See also

- [MCP App registration shapes](mcp-app-registration-shapes-2026-05-30.md)
- [pre-flight before step-up](pre-flight-before-step-up-2026-06-20.md)
- KLA-449 (Apple), KLA-459 (Windows passthrough), KLA-460 (Windows CSP catalog follow-up)
- `internal/apple_mdm/jumpcloud.go`, `internal/windows_mdm/jumpcloud.go`
