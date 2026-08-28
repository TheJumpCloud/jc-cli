#!/usr/bin/env python3
"""Regenerate internal/workflow/corrected.json — jc's corrected copies of the
JumpCloud workflow templates that ship with a defect.

Usage:
    jc workflows templates show "<name>" -o json > /tmp/t.json   # per template
    python3 scripts/gen-corrected-templates.py /tmp/*.json

The correction is mechanical and provable, not editorial: it deletes the
`actions.X.status == 200 &&` conjunct that opens a guard, and keeps the rest.

Why that conjunct is wrong, established by live runs and not by reading:

  * A non-2xx from any jc_operation halts the whole run, so a guard that asks
    whether an earlier call succeeded is only ever evaluated when it did.
  * Worse, `== 200` is an equality against one specific success code. A create
    returning 201 makes it FALSE, silently skipping a task that should run.
    Observed directly: in one run, a task guarded on `status == 200` after a
    201 reported "Skipping — if condition did not match", while the same call
    guarded on `>= 200 && < 300` executed.
  * Deleting it is safe even when the guarded call is itself conditional. A
    guard referencing a task that was SKIPPED evaluates false and does not
    error, so the surviving conjunct still suppresses the task correctly.

Everything else about each template is left byte-identical, including its
REPLACE_WITH_* markers, so the corrected copy stays a drop-in replacement.
"""
import json
import re
import sys

# The conjunct to delete: a status equality that OPENS a guard.
DEAD = re.compile(r'actions\.[A-Za-z_][A-Za-z0-9_]*\.status\s*==\s*200\s*&&\s*')


def slug(name):
    return re.sub(r'[^a-z0-9]+', '-', name.lower()).strip('-')


def main():
    if len(sys.argv) < 2:
        sys.exit("usage: gen-corrected-templates.py <template.json> [...]")

    out = []
    for path in sys.argv[1:]:
        d = json.load(open(path))
        dsl = d.get('dsl', d)
        raw = json.dumps(dsl)
        fixed, n = DEAD.subn('', raw)
        if n == 0:
            print(f"skipping {d.get('name')!r}: nothing to correct", file=sys.stderr)
            continue
        out.append({
            "id": "jc:" + slug(d["name"]),
            "name": d["name"],
            "description": d.get("description", ""),
            "category": d.get("category", ""),
            "corrects": d["name"],
            "corrects_id": d.get("id", ""),
            "changes": (
                f"Removed {n} dead `actions.X.status == 200 &&` conjunct"
                f"{'s' if n != 1 else ''} from task guards. The rest of each "
                "guard is unchanged and still does the real work."
            ),
            "dsl": json.loads(fixed),
        })

    out.sort(key=lambda t: t["name"])
    with open("internal/workflow/corrected.json", "w") as f:
        json.dump(out, f, indent=2, sort_keys=True)
        f.write("\n")
    print(f"wrote internal/workflow/corrected.json — {len(out)} corrected templates")


if __name__ == "__main__":
    main()
