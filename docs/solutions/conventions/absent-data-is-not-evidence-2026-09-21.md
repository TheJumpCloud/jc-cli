---
title: Absent data is not evidence of a negative
date: 2026-09-21
category: conventions
module: internal/audit
tags: [api-integration, jumpcloud, error-handling, parsing, debugging]
applies_when:
  - "Parsing a list response into typed records"
  - "Writing a check, audit, or health verdict that can report 'nothing found'"
  - "An API returns an empty collection for an identifier that does not exist"
---

# Absent data is not evidence of a negative

## Context

This failure has now appeared five times in this codebase, in five
unrelated subsystems, and every instance produced a **confident wrong
answer rather than an error**. That is what makes it worth a convention
rather than five separate fixes.

The shape is always the same: something cannot be read, the code treats
"cannot read" as "nothing there", and a caller downstream reports the
absence as a finding.

| Where | The silent skip | What the operator saw |
|---|---|---|
| `internal/pwm` | `Groups` typed `[]string`, arrived as objects, every record failed to decode | "0 users are enrolled" on a tenant with one |
| `internal/audit` | 11 checks did `if err != nil { continue }` per record | `OK — checks ran clean, no findings` — a **security** all-clear |
| `internal/resolve` | a record that matched by name but whose id would not decode | `not found` for a user sitting in the list |
| `internal/workflow` | event-type buckets skipped on decode failure | the catalog-omission detector reported **no omissions** |
| Google EMM (the API itself) | `/enterprises/{any 24-hex}/devices` returns `{"count":0,"devices":[]}` | "no devices enrolled" for an enterprise that does not exist |

The last row matters most: **the API does this too**, so a contract
package has to defend against it, not just our own parsers.

## The rule

A record that will not decode is an **error**, never a `continue`.

```go
// Wrong — a schema change becomes a shorter list, and a shorter list
// becomes a confident wrong answer.
for _, raw := range rows {
    var u User
    if err := json.Unmarshal(raw, &u); err != nil {
        continue
    }
    out = append(out, u)
}

// Right — say which record, and say what the count would otherwise imply.
for i, raw := range rows {
    var u User
    if err := json.Unmarshal(raw, &u); err != nil {
        return nil, fmt.Errorf("user %d of %d did not decode: %w — the record "+
            "shape has changed and jc would otherwise report fewer users than exist",
            i+1, len(rows), err)
    }
    out = append(out, u)
}
```

Three qualifications that keep this from becoming noise:

- **An empty or absent FIELD is not drift.** A record with no timestamp,
  or no id, is a legitimate skip — only a value that will not *parse* is
  a schema surprise. Conflating the two (`err != nil || g.ID == ""`) was
  itself one of the bugs.
- **Degrade where the missing thing is cosmetic.** `internal/usergroups`
  still returns group memberships by id when the *name* catalog cannot be
  read, and warns. A missing name is cosmetic; a missing group is not.
- **When the API is the offender, confirm before you count.** Every
  Google EMM device call resolves the enterprise against the binding list
  first, so a count is only ever reported for an enterprise that was
  actually seen.

## Why the framework usually already knows

In `internal/audit` the fix needed no new plumbing. `CheckResult.Error`
already rendered as `[ERR]`, already suppressed the "ran clean" line, and
already failed `--exit-code` via `AnyCheckError` — whose own doc comment
read *"a degraded audit shouldn't look like a clean one"*. The contract
was right and the checks were swallowing it one level down.

**Look for the existing error path before adding one.**

## Scanning for it

The pattern hides in more than one syntactic form, and a scan that misses
a form will under-report:

```bash
# The two-line form — MISSED by a one-line lookahead.
#   w, err := Parse(row)
#   if err != nil {
#       continue
# A 3-line window is the minimum.

# And the conjunction form, which drops records just as silently:
grep -rn "err == nil &&" --include='*.go' internal/ | grep -i unmarshal
```

A scan reported 48 sites; a correct 3-line scan found 51, and the
conjunction form added 18 more it never looked for. **Measure, then
subtract — never subtract instead of measuring.**
