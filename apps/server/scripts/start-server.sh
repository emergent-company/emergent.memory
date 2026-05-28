#!/bin/sh
# start-server.sh — start the server binary in background, loading .env.local from repo root.
# Usage: called by `task start` from apps/server directory.

set -e

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
SERVER_BIN="$(cd "$(dirname "$0")/.." && pwd)/dist/server"
PID_FILE="$REPO_ROOT/pids/server.pid"
LOG_FILE="$REPO_ROOT/logs/server/server.log"
ERR_FILE="$REPO_ROOT/logs/server/server.error.log"

mkdir -p "$REPO_ROOT/pids" "$REPO_ROOT/logs/server"

# Load .env.local if present.
if [ -f "$REPO_ROOT/.env.local" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$REPO_ROOT/.env.local"
  set +a
fi

nohup "$SERVER_BIN" >> "$LOG_FILE" 2>> "$ERR_FILE" < /dev/null &
SERVER_PID=$!
echo "$SERVER_PID" > "$PID_FILE"
echo "Server started (PID $SERVER_PID)"
