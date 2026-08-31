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


# Event types the documentation omits but a live tenant demonstrably emits.
#
# The docstring above has warned since this was written that the catalog is a
# lower bound. A warning in a comment reconciles nothing: run
# `jc workflows event-types --audit 30d` against a real tenant and the gap
# becomes a list. On org 5ec71e8e96bfda0611fc6c5b over 30 days, 19 of the 64
# emitted types were absent from a 341-entry catalog — 30% of what that tenant
# actually produces.
#
# Descriptions here say what is KNOWN. These types are verified to exist,
# because the tenant emitted them; their exact semantics are not documented
# anywhere, so nothing is claimed beyond the name and the observation.
#
# ldap_srch is the sharpest entry: the documentation lists `ldap_search`, which
# this tenant never emitted, while emitting `ldap_srch` 240 times. That is a
# documentation ERROR rather than an omission, and a workflow triggering on the
# documented spelling would silently never fire.
OBSERVED = {
    "software_status_update": {"d": "Observed live (678x/30d); not in the documented catalog."},
    "ldap_srch": {
        "d": "An LDAP search. Emitted as ldap_srch (240x/30d); the documentation "
             "lists ldap_search, which was never emitted — trigger on this spelling.",
        "s": "ldap",
    },
    "policy_result": {"d": "A policy application result. Observed live (84x/30d).", "s": "systems"},
    "command_result": {"d": "A command execution result. Observed live (10x/30d).", "s": "systems"},
    "slack_notification_sent": {"d": "A Slack notification was sent. Observed live (8x/30d)."},
    "bulk_update_alerts": {"d": "Alerts updated in bulk. Observed live (6x/30d)."},
    "bulk_delete_alerts": {"d": "Alerts deleted in bulk. Observed live (2x/30d)."},
    "attributemappings_add": {"d": "An attribute mapping was added. Observed live (4x/30d)."},
    "attributemappings_update": {"d": "An attribute mapping was updated. Observed live (2x/30d)."},
    "attributemappings_delete": {"d": "An attribute mapping was deleted. Observed live (4x/30d)."},
    "rule_config_created": {"d": "A rule configuration was created. Observed live (1x/30d)."},
    "rule_config_updated": {"d": "A rule configuration was updated. Observed live (3x/30d)."},
    "rule_config_deleted": {"d": "A rule configuration was deleted. Observed live (1x/30d)."},
    "saas_management_application_review": {
        "d": "A SaaS-managed application was reviewed. Observed live (3x/30d).",
        "s": "saas_management",
    },
    "radius_auth_attempt": {"d": "A RADIUS authentication attempt. Observed live (1x/30d).", "s": "radius"},
    "workflow_update": {"d": "A workflow is updated. Observed live (5x/30d).", "s": "workflows"},
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

    # Live observations are merged in, never overriding a documented entry:
    # if the docs later describe one of these, the doc wins.
    for name, entry in OBSERVED.items():
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
