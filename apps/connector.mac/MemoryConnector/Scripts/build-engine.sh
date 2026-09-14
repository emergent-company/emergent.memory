#!/bin/bash
# Builds the connector engine (memory-connector) into the app bundle's
# Resources directory as part of the Xcode build.
#
# Embed-vs-identity rationale: the engine is spawned only as a DIRECT child
# Process of this app (never via launchd or NSWorkspace/`open`), so macOS
# Automation (TCC) prompts are attributed to the signed app itself and each
# Notes/Reminders grant happens once. A launchd-spawned copy would be treated
# as its own identity and re-prompt on every run.
#
# Path arithmetic: SRCROOT = <repo>/client/macos/MemoryConnector, so the repo
# root is $SRCROOT/../../.. and the engine module lives at
# <root>/connector (its own go.mod; building from that directory works).
set -euo pipefail

REPO_ROOT="$SRCROOT/../../.."
ENGINE_SRC="$REPO_ROOT/apps/connector.linux"
OUT_DIR="${TARGET_BUILD_DIR}/${UNLOCALIZED_RESOURCES_FOLDER_PATH}"
OUT_BIN="$OUT_DIR/memory-connector"

export PATH="$PATH:/opt/homebrew/bin:/usr/local/bin"
export CGO_ENABLED=0
export GOOS=darwin
export GOARCH=arm64

if ! command -v go >/dev/null 2>&1; then
  echo "error: 'go' not found on PATH — install the Go toolchain on the build machine" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"
cd "$ENGINE_SRC"
go build -o "$OUT_BIN" ./cmd/memory-connector
chmod +x "$OUT_BIN"
echo "engine embedded at: $OUT_BIN"
