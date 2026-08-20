---
title: MCP tools and Cobra commands are parallel taxonomies, not 1:1
date: 2026-06-20
category: conventions
module: multiple
tags: [mcp, cli, architecture]
applies_when:
  - "Designing a cross-cutting policy that wants to drive both CLI and MCP behavior from one declaration"
  - "Considering replacing the Execute-bool reflection gate with a Cobra annotation lookup"
  - "Building a tool that mirrors a CLI subcommand and wondering whether to call into it"
---

# MCP tools and Cobra commands are parallel taxonomies, not 1:1

## Context

A natural first-read of the codebase suggests `users_delete` (MCP
tool) and `jc users delete` (Cobra command) are two facets of the
same thing. They're not — they're independent implementations that
each call the same JC API client directly.

This bites whenever someone proposes "drive MCP X from CLI Y":

- KLA-444 ("Cobra annotations as policy registry") originally
  scoped to *also* drive MCP filtering from `jc:class` annotations.
  Recon showed the two registries don't share a name space, so the
  MCP migration was carved off as a follow-up rather than landed
  alongside the CLI annotation work.

## Guidance

**Treat MCP tools and Cobra commands as siblings, not parent/child.**
When a cross-cutting policy needs to cover both:

1. Define the policy at the API-client layer (where both surfaces
   meet) if the policy is a runtime check.
2. Declare the policy in *both* registries if the policy is metadata.
   Cobra commands carry `jc:class` annotations (see KLA-444 and
   `internal/cmd/classifications.go`); MCP tools should carry their
   own classification map indexed by tool name, NOT inferred from
   the Cobra path.
3. Don't try to derive one from the other. The names look similar
   but the boundaries don't actually match (`recipe_run` is one MCP
   tool but ~7 Cobra commands; `commands run` is one CLI command
   but the MCP server splits it into `commands_run` and
   `commands_trigger`).

When a new MCP tool needs to do something a Cobra command already
does, **inline the API client call**, don't invoke the Cobra
command. Reasons:

- Cobra commands write to stdout in formatted output; MCP needs
  structured returns.
- Cobra commands consult global flags (`--plan`, `--quiet`, etc.)
  via viper; MCP has per-tool argument shapes.
- The Cobra path includes prompt-confirmation flows MCP can't
  reproduce.

## Alternatives considered

- **Make MCP tools call into Cobra commands.** Tried during the
  KLA-419 typed-app helper work. Rejected — Cobra commands assume
  a terminal output sink, viper-bound flags, and a process-wide
  configuration. Reproducing that inside an MCP handler ended up
  larger than just calling the API client directly.

- **Auto-generate MCP tools from the Cobra schema.** Considered
  occasionally. The mismatch is real: many Cobra commands take
  string flags that the MCP tool would need to expose as structured
  fields; many MCP tools take structured input that no Cobra command
  models cleanly. Generation produces awkward shims either direction.

## See also

- [step-up auth chokepoint](../design-patterns/step-up-auth-chokepoint-2026-05-08.md)
- KLA-419 typed-app registration helper (the work that surfaced the
  taxonomy gap)
- KLA-444 (the work that confirmed it via recon)
