## Summary

<!-- What and why. Link the Linear ticket (KLA-xxx). -->

## Verification

<!-- Tests added/updated; live verification against a tenant where applicable. -->

## Checklist

- [ ] Tests cover the change (new behavior has a regression guard)
- [ ] **Docs updated where the change is user-visible**: README, AGENTS.md,
      docs/QUICKSTART.md, the `jc` skill (`skills/skills/jc/SKILL.md`) —
      the schema-driven docs (`docs/site/`, llms.txt) regenerate via
      `make site`, but the prose docs only stay current if you touch them
- [ ] New leaf commands classified in `internal/cmd/classifications.go`
- [ ] MCP tool count + `expectedTools` updated if tools were added
- [ ] Release label applied (`major` / `minor` / `patch` / `no version`)
