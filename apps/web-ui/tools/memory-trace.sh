#!/usr/bin/env bash
# Merge the iOS app trace + worker trace for a room into one chronological view.
# Usage: tools/memory-trace.sh <room>
#
# The Mac host comes from the shared config (MEMORY_MAC_HOST in the environment
# or the gitignored apps/web-ui/.env) — no repo default is baked in here. See
# tools/mac-remote.env.example.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/mac-remote.sh
# shellcheck disable=SC1091
. "$HERE/lib/mac-remote.sh"

ROOM="${1:-}"
mac_load_env
MAC_HOST="${MEMORY_MAC_HOST:-}"
TRACE_DIR="${MEMORY_TRACE_DIR:-/tmp/memory-trace}"
APP_TRACE="Documents/memory-trace.jsonl"
BUNDLE_ID="com.emergent.memory"

if [[ -z "$ROOM" ]]; then
  echo "usage: $0 <room>" >&2
  echo "latest worker rooms:" >&2
  # shellcheck disable=SC2012 # trace files are machine-generated *.jsonl names
  ls -t "$TRACE_DIR" 2>/dev/null | sed 's/\.jsonl$//' | head -5 >&2 || true
  exit 1
fi

if [[ -z "$MAC_HOST" ]]; then
  mac_die "MEMORY_MAC_HOST is not set. Point it at the Mac build machine, e.g.:
    export MEMORY_MAC_HOST=mcj-mini
  or set it in $HERE/../.env (see tools/mac-remote.env.example)."
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

# 1) worker trace (local on the dev server)
if [[ -f "$TRACE_DIR/$ROOM.jsonl" ]]; then
  cat "$TRACE_DIR/$ROOM.jsonl" >> "$tmp"
fi

# 2) app trace (from the simulator on the Mac)
appdir="$(mac_ssh "xcrun simctl get_app_container booted $BUNDLE_ID data" 2>/dev/null || true)"
if [[ -n "$appdir" ]]; then
  mac_ssh "cat '$appdir/$APP_TRACE' 2>/dev/null" 2>/dev/null \
    | jq -c "select((.room // \"\") == \"\" or .room == \"$ROOM\")" >> "$tmp" || true
fi

# 3) merge by timestamp, one human-readable line each
jq -s 'sort_by(.ts)[] | "\(.ts)  \(.event)  room=\(.room // "-")  \(del(.ts,.event,.room,.identity) | tostring)"' "$tmp" 2>/dev/null || cat "$tmp"
