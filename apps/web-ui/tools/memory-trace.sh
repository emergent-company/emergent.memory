#!/usr/bin/env bash
# Merge the iOS app trace + worker trace for a room into one chronological view.
# Usage: tools/memory-trace.sh <room>
set -euo pipefail

ROOM="${1:-}"
MAC_HOST="${MEMORY_MAC_HOST:-mcj-mini}"
TRACE_DIR="${MEMORY_TRACE_DIR:-/tmp/memory-trace}"
APP_TRACE="Documents/memory-trace.jsonl"
BUNDLE_ID="com.emergent.memory"

if [[ -z "$ROOM" ]]; then
  echo "usage: $0 <room>" >&2
  echo "latest worker rooms:" >&2
  ls -t "$TRACE_DIR" 2>/dev/null | sed 's/\.jsonl$//' | head -5 >&2 || true
  exit 1
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

# 1) worker trace (local on the dev server)
if [[ -f "$TRACE_DIR/$ROOM.jsonl" ]]; then
  cat "$TRACE_DIR/$ROOM.jsonl" >> "$tmp"
fi

# 2) app trace (from the simulator on the Mac)
appdir="$(ssh "$MAC_HOST" "xcrun simctl get_app_container booted $BUNDLE_ID data" 2>/dev/null || true)"
if [[ -n "$appdir" ]]; then
  ssh "$MAC_HOST" "cat '$appdir/$APP_TRACE' 2>/dev/null" 2>/dev/null \
    | jq -c "select((.room // \"\") == \"\" or .room == \"$ROOM\")" >> "$tmp" || true
fi

# 3) merge by timestamp, one human-readable line each
jq -s 'sort_by(.ts)[] | "\(.ts)  \(.event)  room=\(.room // "-")  \(del(.ts,.event,.room,.identity) | tostring)"' "$tmp" 2>/dev/null || cat "$tmp"
