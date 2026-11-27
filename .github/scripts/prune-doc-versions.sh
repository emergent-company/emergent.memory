#!/usr/bin/env bash
# Keep only the newest N versioned docs trees in a gh-pages checkout.
#
# Why this exists at all: gh-pages is one size budget shared by every publisher
# (check-pages-tree-size.sh enforces it from publish-appcast.sh). While the MkDocs
# site was live, every release tag made `mike deploy` add a full copy of the
# built site -- ~13 MiB, ~130 files -- and nothing ever removed the old ones. 24
# versioned trees plus the appcast reached 523 MiB against the 500 MiB aggregate
# guard, so the next `Mac Release` publish failed (#976, recurrence of #7).
# Raising the guard was not the fix: it only defers the failure, and GitHub stops
# publishing a Pages site over 1 GB.
#
# The MkDocs site is retired and its trees were cleared from the branch, so
# nothing publishes one now and this is legacy cleanup: it keeps a branch that
# still carries versioned trees within budget, and no-ops on one that does not.
# It is retained rather than deleted because a prune that no longer runs is
# indistinguishable from one that never worked, and the retention policy is
# still the answer if such a tree reappears.
#
# Retention, not reachability: versioned docs are user-visible, so this is a
# deliberate policy -- keep the newest N releases and drop the rest. Anything
# still linked keeps working:
#   * a version that carries an alias is never dropped, whatever its position
#     (`latest` points at the newest tree, but a pinned alias may not);
#   * entries that are not `vX.Y.Z` (the rolling `dev` tree) are kept;
#   * only direct children of --root that versions.json names are deleted, so
#     appcast/, the root-level files and every other path are untouched.
# The Sparkle feed is unaffected: it lives under appcast/ and is pruned by
# prune-appcast-orphans.sh.
#
# versions.json is rewritten in the same step as the deletions, from the parsed
# file (never by string surgery), because an entry left behind for a deleted
# tree 404s from the docs site's version selector. python3 does the parse and
# the rewrite; this script deletes and installs nothing until the rewritten file
# has been re-parsed and its entry count checks out, so a malformed or
# unexpected versions.json fails closed with the tree unchanged.
#
# Usage: prune-doc-versions.sh [--keep N] [--dry-run] [--root DIR]
#   --keep N    number of newest `vX.Y.Z` trees to keep (default: 10, or $DOC_VERSIONS_TO_KEEP)
#   --dry-run   print what would be pruned, delete nothing
#   --root DIR  gh-pages checkout to operate on (default: current directory)
#
# Exit: 0 on success (including "nothing to prune"), 1 when versions.json cannot
# be read, rewritten or validated -- in which case nothing is deleted -- 2 on a
# usage error. Idempotent: a second run is a no-op.
set -euo pipefail

keep="${DOC_VERSIONS_TO_KEEP:-10}"
dry_run=0
root="."

usage() { echo "usage: prune-doc-versions.sh [--keep N] [--dry-run] [--root DIR]" >&2; }

while [ "$#" -gt 0 ]; do
  case "$1" in
    --keep) shift; keep="${1:?--keep needs a count}" ;;
    --dry-run) dry_run=1 ;;
    --root) shift; root="${1:?--root needs a directory}" ;;
    -h|--help) usage; exit 0 ;;
    *) echo "error: unknown option: $1" >&2; usage; exit 2 ;;
  esac
  shift
done

case "$keep" in
  '' | *[!0-9]*)
    echo "error: --keep must be a positive integer, got '$keep'" >&2
    exit 2
    ;;
esac
# 0 would mean "delete every versioned tree", which is never what an operator
# means, and it would empty the docs site's version selector.
if [ "$keep" -lt 1 ]; then
  echo "error: --keep must be at least 1, got '$keep'" >&2
  exit 2
fi

[ -d "$root" ] || { echo "error: ${root} is not a directory" >&2; exit 1; }
root="$(cd "$root" && pwd)"
versions_file="${root}/versions.json"

if [ ! -f "$versions_file" ]; then
  echo "prune-doc-versions: no ${versions_file}; nothing to do"
  exit 0
fi

command -v python3 >/dev/null 2>&1 \
  || { echo "error: python3 is required to rewrite versions.json safely; nothing pruned" >&2; exit 1; }

file_size_bytes() {
  # Portable across macOS (BSD stat) and Linux (GNU stat), with a wc fallback.
  stat -c%s "$1" 2>/dev/null \
    || stat -f%z "$1" 2>/dev/null \
    || wc -c < "$1" | tr -d '[:space:]'
}

# The rewritten file is staged OUTSIDE the checkout: a temp file left in the
# worktree by an interrupted run would be published as a stray root-level file
# (publish-appcast.sh stages the tree with `git add -A`).
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
new_versions="${scratch}/versions.json"

# Drop every versioned entry outside the retention window (oldest first on
# stdout) and write the surviving entries to $new_versions. Ordering is numeric,
# so v0.100.0 sorts above v0.99.0 and v0.88.10 below v0.89.0.
selector=$(cat <<'PY'
import json
import re
import sys

path, keep, out = sys.argv[1], int(sys.argv[2]), sys.argv[3]

try:
    with open(path) as f:
        entries = json.load(f)
except ValueError as e:
    sys.exit("error: %s is not valid JSON: %s" % (path, e))

if isinstance(entries, dict):  # tolerate {"versions": [...]}
    entries = entries.get("versions", [])
if not isinstance(entries, list):
    sys.exit("error: %s does not hold a list of versions" % path)

VERSION = re.compile(r"^v?(\d+)\.(\d+)\.(\d+)$")


def entry_version(entry):
    if not isinstance(entry, dict):
        return None
    version = entry.get("version")
    return version if isinstance(version, str) and VERSION.match(version) else None


def sort_key(version):
    return tuple(int(part) for part in VERSION.match(version).groups())


versioned = sorted(
    (e for e in entries if entry_version(e)), key=lambda e: sort_key(entry_version(e)), reverse=True
)
protected = {e.get("version") for e in entries if isinstance(e, dict) and (e.get("aliases") or not entry_version(e))}
dropped = sorted(
    (entry_version(e) for e in versioned[keep:] if entry_version(e) not in protected),
    key=sort_key,
)

with open(out, "w") as f:
    json.dump([e for e in entries if entry_version(e) not in dropped], f, indent=2)
    f.write("\n")
for version in dropped:
    print(version)
PY
)

if ! selection="$(python3 -c "$selector" "$versions_file" "$keep" "$new_versions")"; then
  echo "error: could not rewrite ${versions_file}; nothing pruned" >&2
  exit 1
fi

if [ -z "$selection" ]; then
  echo "prune-doc-versions: nothing to prune (keeping the newest ${keep} versioned trees plus aliased ones)"
  exit 0
fi

dropped=()
while IFS= read -r version; do
  if [ -n "$version" ]; then dropped+=( "$version" ); fi
done <<< "$selection"

# The rewrite is only installed once it parses back to exactly the entries we
# expect: the original count minus the dropped versions. A short or empty file
# would break the docs site's version selector for every version, so fail closed.
# count_entries counts version entries in either the list or the tolerated
# {"versions": [...]} shape.
count_entries='import json,sys; d=json.load(open(sys.argv[1])); print(len(d.get("versions", []) if isinstance(d, dict) else d))'
before="$(python3 -c "$count_entries" "$versions_file")" || {
  echo "error: could not count entries in ${versions_file}; nothing pruned" >&2
  exit 1
}
after="$(python3 -c "$count_entries" "$new_versions")" || {
  echo "error: rewritten ${versions_file} is not valid JSON; nothing pruned" >&2
  exit 1
}
if [ "$after" -ne $(( before - ${#dropped[@]} )) ]; then
  echo "error: rewritten ${versions_file} has ${after} entries, expected $(( before - ${#dropped[@]} )); nothing pruned" >&2
  exit 1
fi

total=0
count=0
for version in "${dropped[@]}"; do
  dir="${root}/${version}"
  if [ -d "$dir" ]; then
    size=0
    while IFS= read -r -d '' f; do
      size=$(( size + $(file_size_bytes "$f") ))
    done < <(find "$dir" -type f -print0)
    if [ "$dry_run" -eq 1 ]; then
      echo "would delete: ${version} (${size} bytes)"
    else
      rm -rf -- "$dir"
      echo "deleted: ${version} (${size} bytes)"
    fi
    total=$(( total + size ))
    count=$(( count + 1 ))
  else
    # versions.json listed a tree that is not on disk: drop the entry so the
    # version selector stops offering a 404. Nothing to reclaim.
    if [ "$dry_run" -eq 1 ]; then
      echo "would remove stale entry: ${version} (no directory)"
    else
      echo "removed stale entry: ${version} (no directory)"
    fi
  fi
done

if [ "$dry_run" -eq 1 ]; then
  echo "prune-doc-versions: dry run, would reclaim ${total} bytes ($(( total / 1024 / 1024 )) MiB) across ${count} tree(s)"
  exit 0
fi

chmod 644 -- "$new_versions"
mv -f -- "$new_versions" "$versions_file"
trap - EXIT
echo "prune-doc-versions: reclaimed ${total} bytes ($(( total / 1024 / 1024 )) MiB) across ${count} tree(s); kept the newest ${keep} plus aliased trees"
