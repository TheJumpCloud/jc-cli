#!/usr/bin/env python3
"""Regenerate internal/workflow/operations.json — the operationId index that
`jc workflows validate` checks workflow DSL against.

Usage:
    python3 scripts/gen-operation-index.py /path/to/index.yaml

Workflow DSL invokes JumpCloud's own API by operationId:

    {"call": "jc_operation", "with": {"operationId": "getApiSystemusers"}}

Nothing in the workflow API validates that id, so a typo produces a workflow
that only fails once it runs. This index lets validation catch it at author
time, and lets `jc workflows explain` render each step as METHOD /path.

Like scripts/api-coverage.py, the spec is large (~1.8MB) and lives outside the
repo, so it is passed by path rather than vendored. The generated index IS
checked in and embedded into the binary — it is JumpCloud's own spec in
JumpCloud's own CLI, so there is no redistribution question, and generating it
here means no network fetch, no pinned hash, and no cache directory to go
stale.
"""
import json
import sys

try:
    import yaml
except ImportError:
    sys.exit("PyYAML required: pip install pyyaml")

OUT = "internal/workflow/operations.json"
METHODS = ("get", "post", "put", "patch", "delete", "head", "options")

# Keep the embedded payload small: summaries are for one-line `explain` output,
# not documentation.
SUMMARY_MAX = 80


def main():
    if len(sys.argv) < 2:
        sys.exit("usage: gen-operation-index.py /path/to/index.yaml")

    spec = yaml.safe_load(open(sys.argv[1]))
    index = {}
    for path, item in (spec.get("paths") or {}).items():
        if not isinstance(item, dict):
            continue
        for method, op in item.items():
            if method.lower() not in METHODS or not isinstance(op, dict):
                continue
            op_id = op.get("operationId")
            if not op_id:
                continue
            if op_id in index:
                sys.exit(f"duplicate operationId {op_id!r} — the index assumes uniqueness")
            index[op_id] = {
                "m": method.upper(),
                "p": path,
                "s": (op.get("summary") or "").strip()[:SUMMARY_MAX],
            }

    if not index:
        sys.exit("no operationIds found — wrong file?")

    with open(OUT, "w") as f:
        json.dump(index, f, separators=(",", ":"), sort_keys=True)
        f.write("\n")

    v2 = sum(1 for v in index.values() if v["p"].startswith("/api/v2"))
    print(f"wrote {OUT} — {len(index)} operationIds ({v2} v2, {len(index) - v2} v1)")


if __name__ == "__main__":
    main()
