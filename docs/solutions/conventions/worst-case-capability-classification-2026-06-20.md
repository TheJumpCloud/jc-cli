---
title: Worst-case capability — classify wrappers by what they can do, not what they do today
date: 2026-06-20
category: conventions
module: internal/cmd
tags: [annotations, policy, destructive-ops, classification]
applies_when:
  - "Authoring a `jc:class` entry in internal/cmd/classifications.go"
  - "A new command wraps user-supplied input (recipe, CSV, payload) that could include destructive operations"
  - "Reviewing whether a class should be `internal`, `read-only`, `mutating`, or `destructive`"
---

# Worst-case capability — classify wrappers by what they *can* do, not what they do today

## Context

The `jc:class` annotation (KLA-444) tags every leaf Cobra command
with its mutation class. The semantic question for any wrapper —
`jc recipe run`, `jc bulk users`, `jc commands run`,
`jc apple-mdm payloads compose --create-policy` — is:

  *The default invocation is read-only / local, but a flag turns it
  into a write. Which class?*

Cursor Bugbot caught this on PR #62:
`jc apple-mdm payloads compose` was originally tagged `internal`
because most invocations just emit a `.mobileconfig` locally. With
`--create-policy` it POSTs a policy to JumpCloud, same wire shape as
the dedicated `apple-mdm payloads create-policy` command (which was
correctly tagged `mutating`).

## Guidance

**Class by worst-case capability.** If any reachable invocation of
the command can hit class N, the command is class N — regardless of
defaults, regardless of how often that invocation is exercised.

The hierarchy: `destructive > mutating > read-only > internal`. A
command lands at the highest class in the hierarchy that any of its
invocations can reach.

Concrete examples (from `internal/cmd/classifications.go`):

| Command | Default | Worst case | Class |
|---|---|---|---|
| `jc recipe run` | depends on the recipe | recipes can include `users delete` | `destructive` |
| `jc bulk users` | depends on the CSV | CSV can include delete rows | `destructive` |
| `jc commands run` | runs whatever the saved command says | arbitrary code on devices | `destructive` |
| `jc apple-mdm payloads compose` | offline emit | `--create-policy` POSTs to JC | `mutating` |
| `jc apple-mdm payloads template` | offline emit | no flag exists that POSTs | `internal` |
| `jc gsuite import-users` | mutating (creates users) | (same) | `mutating` |

The test `TestComposeNotInternal`
(`internal/cmd/annotations_test.go`) is a regression guard for the
specific compose vs. template distinction.

## Alternatives considered

- **Class by default invocation.** Rejected — `recipe run` with the
  built-in offboard-user recipe is destructive even though the
  *command* could in theory be invoked with a no-op recipe. The
  whole point of the annotation is to let policy callers
  (MCP filtering, future destructive-ops gate) treat the class as
  authoritative. If the class lies about worst-case, every policy
  caller has to second-guess it.

- **Class by per-invocation flag combination.** Considered briefly.
  Rejected because the annotation lives on the *command*, not the
  invocation. A future caller that asks "is `recipe run` ever
  destructive?" needs ONE answer; flag-conditional classification
  forces every caller to re-parse the invocation.

- **Drop the wrapper commands from classification entirely.**
  Rejected — the lint test (`TestEveryLeafIsClassified`) requires
  every leaf to be classified, and a missing entry is a louder
  failure mode than a wrong-too-conservative one.

## See also

- [parallel MCP / CLI taxonomies](parallel-mcp-and-cli-taxonomies-2026-06-20.md)
- KLA-444 (the work that established the annotation)
- PR #62 Bugbot finding (the catch that motivated this convention)
