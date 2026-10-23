#!/usr/bin/env bash
#
# Fail when main carries merged, release-labelled work that no tag covers.
#
# Why this exists. release.yml serializes itself with a `concurrency` group,
# and GitHub Actions holds at most ONE running plus ONE pending run per group:
# a third queued run cancels the pending one. On 2026-10-22 five labelled PRs
# merged in 94 seconds, and the runs for #173 and #175 were cancelled. A
# cancelled run executes no steps, so it cannot report its own death -- which
# is why nothing anywhere said a release had been skipped.
#
# That incident was harmless: #33 merged afterwards, its run succeeded, and the
# tag it cut was taken from main *after* all five merges, so it contained
# everything and its generated notes named every PR. The dangerous shape is the
# same cancellation with nothing merging behind it -- then shipped-looking work
# sits untagged for as long as nobody looks. This script is the thing that
# looks.
#
# It is deliberately a script rather than inline workflow YAML so it can be run
# by hand (`make check-unreleased`) and changed without a merge to find out
# whether the change worked.

set -euo pipefail

REPO="${REPO:-TheJumpCloud/jc-cli}"

# A release run takes 3-5 minutes, so a PR merged moments ago is expected to
# have no tag yet. Anything still uncovered after this long is not in flight.
GRACE_MINUTES="${GRACE_MINUTES:-20}"

# No `v` prefix: the org ruleset "Version Tag Protections" forbids refs/tags/v*,
# so release tags here are bare (1.46.69). Sort -V to get the newest.
latest_tag=$(git tag --list '[0-9]*.[0-9]*.[0-9]*' | sort -V | tail -1)
if [ -z "$latest_tag" ]; then
  echo "unreleased-check: no release tag found — wrong clone, or a shallow one?" >&2
  echo "  (this script needs full history: actions/checkout with fetch-depth: 0)" >&2
  exit 1
fi

# Merge commits on main since that tag, newest first, with commit timestamps.
# --first-parent so only the merges into main are considered, not merges that
# happened inside a contributor's branch.
cutoff=$(( $(date +%s) - GRACE_MINUTES * 60 ))

unreleased=()
in_flight=()
while IFS=$'\t' read -r ts subject; do
  [ -n "${subject:-}" ] || continue
  # "Merge pull request #173 from TheJumpCloud/..." — squash merges carry
  # "(#173)" at the end of the subject instead, so accept either shape.
  pr=$(printf '%s' "$subject" | sed -n -E 's/.*[Mm]erge pull request #([0-9]+).*/\1/p')
  if [ -z "$pr" ]; then
    pr=$(printf '%s' "$subject" | sed -n -E 's/.*\(#([0-9]+)\)$/\1/p')
  fi
  [ -n "$pr" ] || continue

  labels=$(gh pr view "$pr" --repo "$REPO" --json labels \
             --jq '[.labels[].name] | join(",")' 2>/dev/null || echo "")
  case ",$labels," in
    *,major,*|*,minor,*|*,patch,*) ;;
    *) continue ;;   # "no version", or unlabelled: nothing was owed
  esac

  if [ "$ts" -gt "$cutoff" ]; then
    in_flight+=("#$pr")
  else
    unreleased+=("#$pr")
  fi
# --reverse so both lists come out oldest first, which is the order a
# recovery has to run in for the version numbers to follow merge order.
# (git does this itself; reversing afterwards would need `tac` on Linux and
# `tail -r` on macOS, and this script runs in both places.)
done < <(git log --first-parent --merges --reverse --format='%ct%x09%s' "$latest_tag..HEAD")

if [ ${#in_flight[@]} -gt 0 ]; then
  echo "unreleased-check: ${in_flight[*]} merged within the last ${GRACE_MINUTES}m — a release run is probably still going; not counted."
fi

if [ ${#unreleased[@]} -eq 0 ]; then
  echo "unreleased-check: nothing owed — newest tag $latest_tag covers every release-labelled merge."
  exit 0
fi

echo "unreleased-check: these merged PRs asked for a release and no tag covers them:" >&2
for pr in "${unreleased[@]}"; do
  echo "  $pr  https://github.com/$REPO/pull/${pr#\#}" >&2
done
cat >&2 <<MSG

Newest tag: $latest_tag

Most likely their release runs were cancelled by release.yml's concurrency
group (see the comment at the top of this file). Recover one at a time, oldest
first, waiting for each to finish:

  gh workflow run release.yml --repo $REPO -f pr=<number>

That path refuses a PR whose work a tag already contains, so it is safe to try.
MSG
exit 1
