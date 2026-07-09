---
title: Fetch-on-demand catalogs — when the upstream license forbids vendoring
date: 2026-07-08
category: design-patterns
module: internal/windows_mdm
tags: [licensing, vendoring, catalogs, windows, ddf, embed]
applies_when:
  - "Vendoring third-party schema/catalog data into the repo (the Apple //go:embed pattern)"
  - "The upstream data source has no explicit license or restrictive terms"
  - "Deciding between //go:embed, generate-and-strip, and fetch-on-demand for reference data"
---

# Fetch-on-demand catalogs — when the upstream license forbids vendoring

## Context

The Apple MDM catalog (KLA-449) vendors Apple's schema YAML directly
into the repo — clean, because Apple publishes it under MIT. The
Windows Policy CSP catalog (KLA-460) planned to do the same with
Microsoft's DDF v2 files and hit a wall at the empirical gate:

- The DDF zip from download.microsoft.com ships **no license file**.
- Microsoft Learn's Terms of Use default to *personal, non-commercial
  use; no reproduction/redistribution without written consent*.
- The docs' public CC-BY GitHub mirror no longer exists (the repo went
  private), so there's no alternative licensed source of the same data.

Copying Microsoft's XML into a public repo was real legal exposure —
and the data (8.1MB raw) was also forcing an artificial "curated
15-area subset" just to keep the vendor tree reasonable.

## Guidance

**When upstream terms don't clearly permit redistribution, ship zero
upstream content and fetch at runtime** (see
`internal/windows_mdm/catalog.go`):

1. **Pin the snapshot**: official URL + SHA-256 recorded as consts,
   updated together in one commit (same discipline as an Apple vendor
   bump). The hash pin means a swapped/tampered download fails loud.
2. **Cache + marker**: extract into `~/.cache/jc/<feature>/<snapshot>/`,
   write a completion marker last so a mid-extract crash re-extracts.
   Everything after the one-time fetch is offline.
3. **Air-gapped escape hatch**: check for a pre-placed zip next to the
   cache dir before downloading, and say so in the download-failure
   error message.
4. **Tests use a hand-written fixture** that is byte-faithful to the
   upstream format (BOM, DOCTYPE, namespaces, every value-constraint
   shape) but contains none of the upstream's content.

The surprise upside: dropping the vendoring constraint removed the
size pressure entirely — KLA-460 ships all ~230 Policy CSP areas
(~3,700 settings) instead of a curated 15, because the binary carries
none of it.

## Alternatives considered

- **Vendor raw upstream XML with a NOTICE.** The Apple pattern.
  Rejected — a NOTICE doesn't create a license that doesn't exist;
  Microsoft's TOU defaults are restrictive and this is a public repo.

- **Vendor "facts only" (paths, formats, enums stripped of prose).**
  Facts aren't copyrightable, so a generated fact-table would likely
  be safe — but the *descriptions* are what make search useful, and
  rewriting ~3,700 of them isn't viable. Fetch-on-demand keeps the
  descriptions AND the legal cleanliness.

- **Scrape the learn.microsoft.com pages at runtime instead.** Same
  licensing posture as the zip but a far worse parse target (generated
  HTML vs. uniform DDF v2 XML), and hundreds of requests instead of
  one 727KB download.

## See also

- [OS-agnostic policy templates](os-agnostic-policy-templates-2026-07-08.md)
  (the KLA-459 companion pattern this catalog feeds)
- `internal/apple_mdm/schemas/*/NOTICE.md` — the vendoring pattern for
  permissively-licensed upstreams
- KLA-460 Linear comment (2026-07-08) — the full gate evidence
