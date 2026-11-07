#!/usr/bin/env bash
# Fail if the whole gh-pages tree exceeds the aggregate published-Pages limit.
#
# check-pages-file-size.sh guards a single file at GitHub's 100 MiB *push*
# limit. Nothing guards the separate 1 GB limit on a *published* Pages site:
# above it GitHub stops building the site, which freezes the Sparkle update feed
# and the docs/landing page. This guard measures the aggregate tree (the
# checked-out worktree, or an explicit file list) and fails above a threshold,
# so a publish stops before it can cross the cliff.
#
# The .git directory is excluded: it is not part of the published tree.
#
# Usage: check-pages-tree-size.sh [path...]   (default: current directory)
#   A directory is walked recursively (minus .git); a file contributes its size.
# Env:   PAGES_TREE_LIMIT_MIB (default 500)
set -euo pipefail

limit_mib="${PAGES_TREE_LIMIT_MIB:-500}"
case "$limit_mib" in
  ''|*[!0-9]*) echo "error: PAGES_TREE_LIMIT_MIB must be a non-negative integer, got '$limit_mib'" >&2; exit 2 ;;
esac
limit_bytes=$(( limit_mib * 1024 * 1024 ))

file_size_bytes() {
  # Portable across macOS (BSD stat) and Linux (GNU stat), with a wc fallback.
  stat -c%s "$1" 2>/dev/null \
    || stat -f%z "$1" 2>/dev/null \
    || wc -c < "$1" | tr -d '[:space:]'
}

[ "$#" -gt 0 ] || set -- .

total=0
count=0
for path in "$@"; do
  if [ -d "$path" ]; then
    while IFS= read -r -d '' f; do
      size="$(file_size_bytes "$f")"
      total=$(( total + size ))
      count=$(( count + 1 ))
    done < <(find "$path" -name .git -prune -o -type f -print0)
  elif [ -f "$path" ]; then
    size="$(file_size_bytes "$path")"
    total=$(( total + size ))
    count=$(( count + 1 ))
  else
    echo "error: ${path} not found" >&2
    exit 1
  fi
done

mib=$(( total / 1024 / 1024 ))
echo "pages tree: ${total} bytes (${mib} MiB) across ${count} file(s); guard ${limit_mib} MiB"
if [ "$total" -gt "$limit_bytes" ]; then
  echo "error: gh-pages tree is ${mib} MiB (${total} bytes), over the ${limit_mib} MiB aggregate guard; GitHub stops publishing Pages sites over 1 GB" >&2
  exit 1
fi
echo "pages tree size ok — within the ${limit_mib} MiB aggregate guard"
