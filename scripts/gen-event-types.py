#!/usr/bin/env python3
"""Regenerate internal/workflow/eventtypes.json — the Directory Insights event
type catalog that `jc workflows validate` checks jc_events triggers against.

Usage:
    python3 scripts/gen-event-types.py /path/to/api-insights-directory.json

A workflow with a mistyped trigger type saves, activates, and then silently
never fires. That is the worst failure shape the DSL offers, because it is
indistinguishable from an event that simply has not happened yet. Nothing in
the workflows API validates the type, so this catalog is the only way to catch
it at author time.

JumpCloud documents the vocabulary in the Directory Insights API reference —
as markdown tables inside the spec's info.description, not as an enum, which
is why this parses prose. Each row is `event_type | what it means`, grouped
under a section per service.

Like the other generators here, the source spec is passed by path rather than
vendored; the generated catalog IS checked in.

IMPORTANT: the catalog is a LOWER BOUND. Cross-checking against a live tenant
found 30 of 110 emitted event types absent from it (command_result,
policy_result, radius_auth_attempt, ldap_srch, ...). Consumers must warn, never
reject.
"""
import json
import re
import sys

OUT = "internal/workflow/eventtypes.json"

ROW = re.compile(r'^\s*\|?\s*([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\s*\|\s*([^|]+?)\s*\|?\s*$', re.M)
HEAD = re.compile(r'^#{2,4}\s*(.+?)\s*$', re.M)

# Section heading -> the Directory Insights service it belongs to, matching
# api.ValidInsightsServices so the two axes line up.
SECTION_SERVICE = {
    "directory - object events": "directory",
    "directory - user and admin events": "directory",
    "directory - command and policy events": "directory",
    "radius events": "radius",
    "sso events": "sso",
    "systems events": "systems",
    "password manager events": "password_manager",
    "password vault events": "password_manager",
    "software events": "software",
    "ldap events": "ldap",
    "mdm events": "mdm",
    "notification channel events": "notifications",
    "object storage events": "object_storage",
    "alert events": "alert",
    "saas management events": "saas_app_management",
    "access management events": "access_management",
    "reports events": "reports",
    "scheduled reports events": "reports",
    "asset management events": "asset_management",
}


def main():
    if len(sys.argv) < 2:
        sys.exit("usage: gen-event-types.py /path/to/api-insights-directory.json")

    spec = json.load(open(sys.argv[1]))
    desc = spec.get("info", {}).get("description", "")
    if not desc:
        sys.exit("spec has no info.description — wrong file?")

    heads = [(m.start(), m.group(1).strip()) for m in HEAD.finditer(desc)]

    def section_for(pos):
        cur = ""
        for p, h in heads:
            if p < pos:
                cur = h
            else:
                break
        return cur

    catalog = {}
    for m in ROW.finditer(desc):
        section = section_for(m.start())
        if "event" not in section.lower():
            continue
        name = m.group(1)
        entry = {"d": " ".join(m.group(2).split())[:120]}
        svc = SECTION_SERVICE.get(section.lower())
        if svc:
            entry["s"] = svc
        # First mention wins: the same type can be listed under several
        # sections (MTP repeats the directory events).
        catalog.setdefault(name, entry)

    if len(catalog) < 100:
        sys.exit(f"only {len(catalog)} event types parsed — the doc format probably changed")

    with open(OUT, "w") as f:
        json.dump(catalog, f, separators=(",", ":"), sort_keys=True)
        f.write("\n")

    with_svc = sum(1 for v in catalog.values() if v.get("s"))
    print(f"wrote {OUT} — {len(catalog)} event types ({with_svc} mapped to a service)")


if __name__ == "__main__":
    main()
