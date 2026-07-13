# Third-party content notices for built-in bundles

## macos-cis-lvl1.yaml, macos-cis-lvl2.yaml

Derived from the **NIST macOS Security Compliance Project** (mSCP),
<https://github.com/usnistgov/macos_security>, release tag `tahoe_rev3`
(macOS 26 "Tahoe"), baselines `cis_lvl1` / `cis_lvl2`.

mSCP is licensed under **Creative Commons Attribution 4.0 International
(CC BY 4.0)**; contributions from the United States Government are not
subject to copyright in the United States (17 U.S.C. § 105(a)). See the
project's `LICENSE.md`.

What these files reproduce from mSCP: baseline titles, rule-derived
configuration values (`mobileconfig_info` payload facts with the
baseline's organization-defined values substituted). What they do NOT
reproduce: rule discussion prose, and Apple-contributed "Vendor
Description content" (which mSCP's license expressly excludes from
CC BY 4.0).

The `cis_lvl1`/`cis_lvl2` baselines are published by mSCP in
collaboration with the Center for Internet Security. These bundles are
*derived from mSCP's publication* and are not CIS Benchmarks; "CIS" is
a trademark of the Center for Internet Security, used here only to
identify the upstream baseline names.

Regeneration: each file's header comment records the exact
`jc bundle import mscp` invocation; the snapshot is pinned by tag +
SHA-256 in `internal/mscp/fetch.go`.

## example-baseline.yaml

Original jc-cli content; not derived from any external publication.
