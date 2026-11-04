#!/usr/bin/env bash
# Fail if a file destined for GitHub Pages exceeds the safe size threshold.
#
# GitHub blocks any single file larger than 100 MiB at push time. The Mac app's
# update DMG is committed to the gh-pages branch (see publish-appcast.sh), so
# crossing the limit makes the push fail and silently breaks the Sparkle feed's
# enclosure URL. Guard at a safe 90 MiB (overridable) with the actual size.
#
# Usage: check-pages-file-size.sh <file>...
# Env:   PAGES_FILE_LIMIT_MIB (default 90)
set -euo pipefail

limit_mib="${PAGES_FILE_LIMIT_MIB:-90}"

file_size_bytes() {
  # Portable across macOS (BSD stat) and Linux (GNU stat), with a wc fallback.
  stat -c%s "$1" 2>/dev/null \
    || stat -f%z "$1" 2>/dev/null \
    || wc -c < "$1" | tr -d '[:space:]'
}

[ "$#" -gt 0 ] || { echo "usage: check-pages-file-size.sh <file>..." >&2; exit 2; }

limit_bytes=$(( limit_mib * 1024 * 1024 ))
fail=0
for f in "$@"; do
  if [ ! -f "$f" ]; then
    echo "error: $f not found" >&2
    exit 1
  fi
  size="$(file_size_bytes "$f")"
  mib=$(( size / 1024 / 1024 ))
  if [ "$size" -gt "$limit_bytes" ]; then
    echo "error: $(basename "$f") is ${mib} MiB (${size} bytes), over the ${limit_mib} MiB guard (GitHub blocks files >100 MiB)" >&2
    fail=1
  else
    echo "size ok: $(basename "$f") = ${mib} MiB (${size} bytes) — within the ${limit_mib} MiB guard"
  fi
done
exit "$fail"
