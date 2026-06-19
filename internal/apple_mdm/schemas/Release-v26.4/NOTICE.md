# Apple device-management schemas (vendored)

This directory contains Apple's MDM Configuration Profile schemas, vendored
from the official Apple-published repository for offline catalog browsing
and `.mobileconfig` generation by `jc apple-mdm payloads`.

## Provenance

- **Source**: https://github.com/apple/device-management
- **Branch**: `release` (Apple's default; not `main`)
- **Tag**: `Release-v26.4`
- **Commit**: `67045e2fa06f528b196c01edee6a8bf88b844beb`
- **Release date**: 2026-03-25
- **Vendored on**: 2026-06-18

## What's included

- `profiles/*.yaml` — 127 Configuration Profile payload schemas (the
  primary input to `jc apple-mdm payloads`)
- `docs/schema.yaml` — Apple's meta-schema (JSON Schema describing the
  shape of every payload YAML); used for parser validation
- `LICENSE.txt` — Apple's MIT license (preserved as required by the terms
  of the license)

## What's NOT vendored

- `declarative/` — DDM schemas. JumpCloud doesn't fully support DDM yet
  (decision logged 2026-06-18). Will vendor in a follow-up when JC ships
  DDM support.
- `mdm/commands/` and `mdm/checkin/` — these are MDM wire-protocol
  schemas (e.g. `EraseDevice`, `RestartDevice`), not policies. Out of
  scope for the policy catalog.
- `mdm/errors/` — error code definitions; not relevant to policy
  generation.
- `docs/schema.md` and `docs/errata.md` — human-readable prose; reference
  via the upstream repo URL, no need to vendor.

## License compliance

Apple's repo is MIT-licensed. The MIT license requires preserving the
copyright notice (`LICENSE.txt` in this directory satisfies this). No
additional obligations apply for redistribution or derivative works.

## Refreshing

When Apple ships a new release (typical cadence: 3–6 per year, aligned
with iOS/macOS dot releases), bump this directory:

1. `git clone --depth 1 -b release https://github.com/apple/device-management.git /tmp/apple-dm`
2. Confirm the new tag/commit in `/tmp/apple-dm` and update this NOTICE.
3. Rename this directory to the new tag (e.g. `Release-v26.5`).
4. Re-vendor with the same file selection above.
5. Update `//go:embed` directive paths in `internal/apple_mdm/catalog.go`.
6. Run the parser test suite — any schema-shape changes will surface as
   parse failures with file-level granularity.
7. Bump the showcase-site version string and run `make site`.

A future `jc apple-mdm payloads update` command will automate steps 1–5.
