#!/usr/bin/env python3
"""Regenerate docs/API_COVERAGE.md — a scorecard of `jc` CLI coverage of the
JumpCloud Console API OpenAPI spec.

Usage:
    python3 scripts/api-coverage.py /path/to/index.yaml

The spec is large (~1.8MB) and lives outside the repo, so it is passed by
path rather than vendored. The generated markdown IS checked in and is the
trackable artifact.

Method (and its limits — stated honestly):
  * Every operation in the spec is bucketed by its first tag (resource area).
  * Each area is mapped to a `jc` command group via AREA_TO_COMMAND below —
    the reliable, ground-truth signal ("does a command group exist for this
    area?"). Per-operation completeness inside a covered area is NOT asserted
    here (dynamic path construction and the generic `graph` handler make a
    purely mechanical per-op match unreliable); those gaps are closed and
    tracked per-area as the coverage program proceeds.
"""
import sys
import collections

try:
    import yaml
except ImportError:
    sys.exit("PyYAML required: pip install pyyaml")

# Spec resource-area (first tag) -> jc command group that covers it, or None
# if there is no command group at all (the hard coverage gap). Keep this in
# sync with `jc --help`.
AREA_TO_COMMAND = {
    "Graph": "graph",
    "System Insights": "system-insights",
    "Assets": "assets",
    "Systems": "devices",
    "Active Directory": "ad",
    "User Groups": "groups",
    "Users": "users",
    "G Suite": "gsuite",
    "Apple MDM": "apple-mdm",
    "Policies": "policies",
    "Access Requests": "access-requests",
    "Systemusers": "users",
    "Applications": "apps",
    "System Groups": "groups",
    "Commands": "commands",
    "Office 365": "office365",
    "Policy Groups": "policy-groups",
    "SaaS App Management": "saas-management",
    "Software Apps": "software",
    "User Group Associations": "graph/groups",
    "Bulk Job Requests": "bulk",
    "Duo": "duo",
    "Organizations": "org",
    "Authentication Policies": "auth-policies",
    "LDAP Servers": "ldap",
    "Samba Domains": "ldap/samba-domains",
    "Translation Rules": "ad/translation-rules",
    "Systems Organization Settings": "devices/settings",
    "Password Policy": "password-policies",
    "Workflows": "workflows",
    "System Group Associations": "graph",
    "IP Lists": "iplists",
    "Policy Group Associations": "policy-groups",
    "Radius Servers": "radius",
    "RADIUS Servers": "radius",
    "Custom Emails": "custom-emails",
    "Service Accounts": "service-accounts",
    "Roles": "roles",
    "Saved Views": "saved-views",
    "Notifications Channels": "notification-channels",
    "Monitoring and Alerting": "alerts",
    "Search": "search",
    "Reports": "reports",
    "Administrators": "admins",
    "Command Results": "commands",
    "Policy Group Members & Membership": "policy-groups",
    "System Group Members & Membership": "groups",
    "User Group Members & Membership": "groups",
    "Application Templates": "app-templates",
    "Policytemplates": "policy-templates",
    "Command Triggers": "commands",
    "Directories": "directories",
    "Identity Providers": "identity-providers",
    "Directory Insights": "insights",
    "Groups": "groups",
    "Rules": "graph",
    "mdm": "apple-mdm/windows-mdm",
    "Microsoft MDM": "windows-mdm",
    "Object Storage": "apple-mdm",
    "fde": "devices",
}

# Areas intentionally out of scope for the CLI coverage program (needs a
# separate MSP/provider-auth track, or is console-internal and not
# customer-facing). Excluded from the "gap to close" total.
OUT_OF_SCOPE = {
    "Providers", "Managed Service Provider",  # MSP multi-tenant auth program
    "Sample Data", "Logos", "FeatureTrials", "Subscriptions", "App Catalog",
    "Push Verification", "Aggregated Policy Stats", "Webhook Notifications",
    "Webhook Notifications Channels", "Scope Groups", "Slack Notifications",
    "Organization Feature Settings", "Image",
}

METHODS = ("get", "post", "put", "patch", "delete", "head", "options")


def main():
    if len(sys.argv) < 2:
        sys.exit("usage: api-coverage.py /path/to/index.yaml")
    # The covered and out-of-scope buckets are counted independently, so a
    # tag in both would be double-counted and break covered + gap == in_scope.
    overlap = set(AREA_TO_COMMAND) & OUT_OF_SCOPE
    if overlap:
        sys.exit(f"area(s) in both AREA_TO_COMMAND and OUT_OF_SCOPE: {sorted(overlap)}")
    spec = yaml.safe_load(open(sys.argv[1]))
    title = spec.get("info", {}).get("title", "API")
    counts = collections.Counter()
    for path, item in (spec.get("paths") or {}).items():
        if not isinstance(item, dict):
            continue
        for m, op in item.items():
            if m.lower() in METHODS and isinstance(op, dict):
                tag = (op.get("tags") or ["(untagged)"])[0]
                counts[tag] += 1

    total = sum(counts.values())
    covered = [(n, t, AREA_TO_COMMAND[t]) for t, n in counts.items() if t in AREA_TO_COMMAND]
    missing = [(n, t) for t, n in counts.items() if t not in AREA_TO_COMMAND and t not in OUT_OF_SCOPE]
    oos = [(n, t) for t, n in counts.items() if t in OUT_OF_SCOPE]

    covered_ops = sum(n for n, _, _ in covered)
    missing_ops = sum(n for n, _ in missing)
    oos_ops = sum(n for n, _ in oos)
    in_scope = total - oos_ops
    pct = round(covered_ops / in_scope * 100) if in_scope else 0

    out = []
    out.append(f"# API Coverage Scorecard — {title}\n")
    out.append("_Generated by `scripts/api-coverage.py`. Do not edit by hand; "
               "rerun the script against the spec to refresh._\n")
    out.append("## Summary\n")
    out.append(f"| Metric | Ops |\n|---|---|")
    out.append(f"| Total operations in spec | **{total}** |")
    out.append(f"| Out of scope (MSP / console-internal) | {oos_ops} |")
    out.append(f"| In scope for the CLI | **{in_scope}** |")
    out.append(f"| In an area with a command group | {covered_ops} |")
    out.append(f"| In an area with **no** command group (gap) | **{missing_ops}** |")
    out.append(f"| Area-level coverage (in scope) | **{pct}%** |\n")
    out.append("> Area-level coverage means a command group exists for the "
               "resource. Per-operation completeness inside a covered area is "
               "tracked and closed as the program proceeds; see Phase 3.\n")

    out.append("## Gap — areas with no command group\n")
    out.append("| Ops | Resource area |\n|---:|---|")
    for n, t in sorted(missing, reverse=True):
        out.append(f"| {n} | {t} |")
    out.append(f"\n**{missing_ops} operations** across **{len(missing)} areas** remain to be covered.\n")

    out.append("## Covered areas (command group exists)\n")
    out.append("| Ops | Resource area | `jc` command |\n|---:|---|---|")
    for n, t, c in sorted(covered, reverse=True):
        out.append(f"| {n} | {t} | `{c}` |")

    out.append("\n## Out of scope\n")
    out.append("| Ops | Resource area |\n|---:|---|")
    for n, t in sorted(oos, reverse=True):
        out.append(f"| {n} | {t} |")
    out.append("\n_MSP/Providers needs a separate provider-auth program; the "
               "singletons are console-internal and not customer-facing._\n")

    open("docs/API_COVERAGE.md", "w").write("\n".join(out) + "\n")
    print(f"wrote docs/API_COVERAGE.md — {covered_ops}/{in_scope} in-scope area coverage ({pct}%), "
          f"{missing_ops} ops gap across {len(missing)} areas")


if __name__ == "__main__":
    main()
