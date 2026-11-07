#!/usr/bin/env bash
# Prune orphaned update archives from a checked-out gh-pages worktree.
#
# Why this is safe: Sparkle updates by reading a published appcast.xml and
# fetching the archives its <enclosure> elements point at. A file in
# appcast/<channel>/ that no published appcast.xml references is unreachable —
# no client can ever ask for it — so deleting it cannot break an update. This is
# strictly weaker than an age-based retention policy: the criterion is
# reachability, not age.
#
# The feeds themselves are generated in CI from the `memory-appcast` and
# `mac-dev` GitHub Releases (generate_appcast downloads the corpus with
# `gh release download`), so gh-pages is a publication target only. Pruning it
# cannot affect delta generation.
#
# Usage: prune-appcast-orphans.sh [--dry-run] [--root DIR] [channel]
#   channel   prune only appcast/<channel>; default is every channel dir.
#   --root    worktree root to operate on (default: current directory).
#   --dry-run print what would be deleted (with sizes and a total), delete none.
#
# Env: none.
#
# Guarantees: never deletes appcast.xml, never deletes a referenced file, never
# touches anything outside appcast/, and is idempotent (a second run is a no-op).
set -euo pipefail

dry_run=0
channel_filter=""
root="."

usage() { echo "usage: prune-appcast-orphans.sh [--dry-run] [--root DIR] [channel]" >&2; }

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dry-run) dry_run=1 ;;
    --root) shift; root="${1:?--root needs a directory}" ;;
    -h|--help) usage; exit 0 ;;
    -*) echo "error: unknown option: $1" >&2; usage; exit 2 ;;
    *) channel_filter="$1" ;;
  esac
  shift
done

root="$(cd "$root" && pwd)"
appcast_dir="${root}/appcast"

file_size_bytes() {
  # Portable across macOS (BSD stat) and Linux (GNU stat), with a wc fallback.
  stat -c%s "$1" 2>/dev/null \
    || stat -f%z "$1" 2>/dev/null \
    || wc -c < "$1" | tr -d '[:space:]'
}

# Print the basename of every URL referenced by an <enclosure> in a feed.
# Handles double- or single-quoted url attributes; drops query/fragment; returns
# nothing (and succeeds) when the feed has no enclosures.
enclosure_basenames() {
  local feed="$1"
  [ -f "$feed" ] || return 0
  {
    { grep -oE '<enclosure[^>]*>' "$feed" || true; } \
      | { grep -oE "url=\"[^\"]*\"|url='[^']*'" || true; } \
      | sed -E "s/^url=[\"']//; s/[\"']$//" \
      | sed -E 's/[?#].*$//' \
      | sed -E 's#.*/##'
  } | sed '/^$/d' || true
}

channels=()
if [ -n "$channel_filter" ]; then
  # Reject anything that could escape appcast/ (path separators, .., etc).
  case "$channel_filter" in
    *[!A-Za-z0-9._-]* | . | ..)
      echo "error: invalid channel name: $channel_filter" >&2
      exit 2
      ;;
  esac
  channels=( "$channel_filter" )
else
  if [ -d "$appcast_dir" ]; then
    for d in "${appcast_dir}"/*/; do
      [ -d "$d" ] || continue
      channels+=( "$(basename "$d")" )
    done
  fi
fi

if [ "${#channels[@]}" -eq 0 ]; then
  echo "prune: no appcast channels under ${appcast_dir}; nothing to do"
  exit 0
fi

grand_total=0
grand_count=0

for ch in "${channels[@]}"; do
  dir="${appcast_dir}/${ch}"
  if [ ! -d "$dir" ]; then
    echo "prune: skipping ${ch}: ${dir} is not a directory"
    continue
  fi

  feed="${dir}/appcast.xml"
  if [ ! -f "$feed" ]; then
    # Without a feed we cannot tell what is referenced. Deleting on a guess
    # would be the one unrecoverable mistake, so leave the channel alone.
    echo "prune: warning: ${dir} has no appcast.xml; leaving its files untouched" >&2
    continue
  fi

  refs_file="$(mktemp)"
  enclosure_basenames "$feed" | sort -u > "$refs_file"

  # A referenced-but-absent enclosure means the feed is already broken. Report
  # it (someone should know) but do not fail the publish.
  while IFS= read -r name; do
    [ -n "$name" ] || continue
    if [ ! -e "${dir}/${name}" ]; then
      echo "prune: warning: ${feed} references missing file ${name}" >&2
    fi
  done < "$refs_file"

  total=0
  count=0
  # Only regular files directly in the channel dir; appcast.xml is never a
  # candidate, and a symlink (-type f excludes it) is never removed.
  while IFS= read -r -d '' f; do
    name="$(basename "$f")"
    [ "$name" = "appcast.xml" ] && continue
    if grep -Fqx -- "$name" "$refs_file"; then
      continue
    fi
    # Defence in depth: only ever act on a direct child of this channel dir.
    case "$f" in
      "${dir}"/*) ;;
      *) echo "prune: warning: refusing to touch ${f} (outside ${dir})" >&2; continue ;;
    esac
    size="$(file_size_bytes "$f")"
    if [ "$dry_run" -eq 1 ]; then
      echo "would delete: ${ch}/${name} (${size} bytes)"
    else
      rm -f -- "$f"
      echo "deleted: ${ch}/${name} (${size} bytes)"
    fi
    total=$(( total + size ))
    count=$(( count + 1 ))
  done < <(find "$dir" -mindepth 1 -maxdepth 1 -type f ! -name appcast.xml -print0)
  rm -f -- "$refs_file"

  verb="reclaimed"
  [ "$dry_run" -eq 1 ] && verb="would reclaim"
  echo "prune: ${verb} ${total} bytes ($(( total / 1024 / 1024 )) MiB) across ${count} file(s) in ${ch}"

  grand_total=$(( grand_total + total ))
  grand_count=$(( grand_count + count ))
done

verb="reclaimed"
[ "$dry_run" -eq 1 ] && verb="would reclaim"
echo "prune: total ${verb} ${grand_total} bytes ($(( grand_total / 1024 / 1024 )) MiB) across ${grand_count} file(s)"
echo "prune-total-bytes=${grand_total}"
