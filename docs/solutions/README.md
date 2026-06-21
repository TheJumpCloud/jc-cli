# docs/solutions/ — design-decision + convention library

A grep-able library of non-obvious decisions made in `jc-cli`. The
target reader is a future contributor (human or agent) who has just
landed in an unfamiliar package and is about to redo a debate that's
already been settled.

The library exists because the alternatives weren't durable:

- **AGENTS.md** is one big file and grows linearly.
- **Inline code comments** are co-located with the decision but
  invisible to grep if the reader doesn't already know which file to
  look at.
- **PR descriptions and Linear tickets** capture *why* at the time
  but agents don't search them, and they decay as the codebase moves
  on.

A solution file pins the decision *next to the system it constrains*,
behind a stable filename and YAML-frontmatter tags. When you're about
to design a new MCP App, you `grep -l "module: internal/mcp" docs/solutions/`
and read the four entries that come back before writing a line of Go.

## Layout

```
docs/solutions/
├── design-patterns/    architectural / structural decisions
├── conventions/        coding conventions and style rules
├── postmortems/        what-went-wrong and how-we-recovered
└── README.md           this file
```

A file lives in one category. When unsure, prefer `design-patterns/`
for forward-looking guidance, `postmortems/` only when the entry
exists to explain a *past* incident (so future readers know what
specifically not to redo).

## Filename convention

`<short-kebab-slug>-<YYYY-MM-DD>.md`

The date is the date the decision was made (not the date the file was
written). The slug should read as a noun-phrase headline — the reader
should know whether the file applies to their problem from the
filename alone.

Good: `step-up-auth-chokepoint-2026-05-08.md`  
Bad:  `step-up.md` (no date, no scope hint), `decisions-mcp.md`
(plural, no specific decision)

## Frontmatter

Every solution file starts with YAML frontmatter:

```yaml
---
title: One-line headline matching the filename slug
date: 2026-05-08
category: design-patterns
module: internal/mcp
tags: [mcp, auth, destructive-ops]
applies_when:
  - "Adding a new MCP tool that mutates state"
  - "Designing a destructive-confirmation flow elsewhere in the codebase"
superseded_by:  # optional — link to a newer solution that obsoletes this one
---
```

| Field | Required | Purpose |
|---|---|---|
| `title` | yes | Human headline. Matches the H1 below the frontmatter. |
| `date` | yes | Decision date, ISO 8601. |
| `category` | yes | `design-patterns`, `conventions`, or `postmortems`. |
| `module` | yes | The directory (or two) the solution principally affects. `multiple` is fine for cross-cutting. |
| `tags` | yes | At least one. Used by `grep -l "tags:.*<tag>"` discovery. Reuse existing tags before inventing new ones. |
| `applies_when` | yes | Bullet list of situations where this entry should be consulted. The agent's primary signal. |
| `superseded_by` | no | Path to a newer solution. Set this rather than deleting — keeps the original decision searchable. |

`docs/solutions/lint_test.go` enforces frontmatter presence and the
required fields.

## Body shape

Three sections is the typical case — keep it scannable:

```markdown
## Context
What problem prompted the decision? Two or three sentences.

## Guidance
What to do. Imperative, specific, with at least one code or filename
reference where applicable.

## Alternatives considered
Briefly: what else we looked at, what specifically ruled it out.
```

A solution that only says "do X" without saying *why X over Y* doesn't
help the reader when their constraints look different. Always include
the alternative-comparison.

## When to write one

Add a solution file when you find yourself writing the same
explanation more than once — in a PR description, in a code review
comment, in a Slack DM — and the rationale isn't obvious from the
code alone. The threshold is *non-obviousness* + *recurrence*, not
"every PR gets a solution file".

When in doubt: would a contributor in six months, with no memory of
the original conversation, derive this decision from the code? If
yes, skip it. If no, write it.
