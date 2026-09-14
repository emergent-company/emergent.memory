#!/bin/bash
# Builds the EventKit-backed reminders helper (memory-reminders) into the app
# bundle's Resources directory as part of the Xcode build.
#
# Why a separate CLI: the engine's osascript `reminders_list` path cannot scale
# on large libraries. The helper gives the Go engine a fast EventKit path while
# keeping the AppleScript path as a fallback when the helper is absent (Linux,
# unit tests).
#
# Embed-vs-identity rationale: like memory-connector, the helper is spawned only
# as a DIRECT child of the engine (itself a child of this app), so the macOS
# Reminders (TCC) grant is attributed to the signed app.
#
# Path arithmetic: SRCROOT = <repo>/client/macos/MemoryConnector.
set -euo pipefail

OUT_DIR="${TARGET_BUILD_DIR}/${UNLOCALIZED_RESOURCES_FOLDER_PATH}"
OUT_BIN="$OUT_DIR/memory-reminders"
SRC_DIR="$SRCROOT/RemindersHelper"

export PATH="$PATH:/opt/homebrew/bin:/usr/local/bin"

if ! command -v xcrun >/dev/null 2>&1; then
  echo "error: 'xcrun' not found on PATH — install the Xcode command line tools" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"
# shellcheck disable=SC2086 # unquoted glob is intentional: compile every source
xcrun --sdk macosx swiftc -O -framework EventKit -o "$OUT_BIN" "$SRC_DIR"/*.swift
chmod +x "$OUT_BIN"
echo "reminders helper embedded at: $OUT_BIN"
