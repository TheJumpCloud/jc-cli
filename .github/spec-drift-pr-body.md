JumpCloud's published OpenAPI specs have moved: **__WAS__ → __OPS__ operations**. This regenerates the two files derived from them.

| File | Why it matters when stale |
|---|---|
| `internal/workflow/operations.json` | `jc workflows validate` checks every workflow step against it. The API accepts ids this file has not heard of, so every operation the spec has gained is reported to the author as a typo — with a "did you mean" naming unrelated neighbours, which reads like a corrected mistake rather than a missing entry. |
| `docs/API_COVERAGE.md` | The coverage scorecard. When the spec grows, a newly uncovered area is invisible and the percentage overstates coverage. It once read 100% while five areas had no command group. |

Opened automatically by `.github/workflows/spec-drift.yml`. Reproduce locally with `make refresh-generated`, or check without changing anything with `make verify-generated`.

**Checks will not start on their own.** A pull request opened with `GITHUB_TOKEN` does not trigger `pull_request` workflows, so `test` and the rest sit unreported. Close and reopen this PR to run them.

Worth reading the diff rather than merging blind. An operation **disappearing** from the spec is the interesting case: it means something `jc` may already expose has been withdrawn upstream, and that deserves a look before the index stops listing it.
