---
title: Stacked PRs blocked by "approval from someone other than last pusher"
date: 2026-06-19
category: postmortems
module: meta
tags: [git, github, branch-protection, stacked-prs, msps-workflow]
applies_when:
  - "About to build a stack of 4+ dependent PRs as a single contributor"
  - "Branch protection requires review-from-different-user and you're the only active reviewer"
  - "Stuck waiting for your own PRs to merge with no second reviewer available"
---

# Stacked PRs blocked by "approval from someone other than last pusher"

## Context

The Apple-MDM arc (KLA-449 → KLA-450 → KLA-451 → KLA-452) was
authored as a stack of 5 dependent PRs (#50, #51, #52, #53, #54).
Each one depended on its predecessor's branch. Branch protection on
`main` required a code review from someone *other than the last
pusher*. As the only active contributor, self-approval was blocked
on every PR.

The five PRs sat in an approval-deadlock for ~6 hours while
post-recon work on KLA-452 continued on the local branches.

## What worked

**Consolidate the stack into one PR** off main:

```bash
git checkout main && git pull
git checkout -b juergen/kla-449-455-apple-mdm-bundle
git merge juergen/kla-452-apple-mdm-mcp  # tip of the stack carries all 5
gh pr create --title "feat(apple-mdm): catalog through MCP integration (KLA-449..452)"
# close PRs #50-#54 as stale
```

Branch protection allowed a different reviewer to approve the
consolidated bundle (~7,500 LOC across 5 logical units). Lost
per-PR review granularity, but the alternative was waiting
indefinitely for a second reviewer.

The follow-up Apple-MDM PR (#60 for KLA-452 final) was much smaller
and went through normally because by then a second contributor was
available for reviews.

## Guidance

**Author stacks only when at least two active reviewers are
guaranteed.** For a solo arc:

- Prefer one branch per ticket merged to main as you go.
- If a follow-up ticket depends on an in-flight PR, fork from the
  in-flight branch and rebase onto main after the parent merges.
- When you discover mid-arc that you've built a stack and the
  reviewer pool is thin, consolidate early rather than late —
  every extra PR added to the stack is one more "couldn't I
  squeeze this in" before you give up and bundle.

When you must consolidate:

- Open the bundle PR with a description that names every ticket
  and every former PR number (so the closed PRs' history is
  recoverable from the bundle).
- Close stale PRs with a comment pointing at the bundle, not just
  silently. Otherwise their notifications confuse future readers.

## Alternatives considered

- **Force-push to bypass the rule.** Not an option (`force-push to
  main` is also protected) and would have been the wrong call even
  if it had been: the rule exists because solo-merge of large
  changes is genuinely riskier than multi-reviewer.

- **Wait for a second reviewer.** Tried for ~3 hours; reviewer
  availability was sporadic. Sunk cost on holding 5 branches in
  sync against `main` (which kept moving) wasn't worth the
  per-PR review granularity.

- **Ask for the branch-protection rule to be temporarily relaxed.**
  Not pursued — the rule is correct, the situation was a planning
  failure (over-stacking), not a rule failure.

## See also

- KLA-449..452 (the arc)
- PR #55 (the consolidated bundle that unblocked the stack)
