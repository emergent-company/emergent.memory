#!/bin/sh
# start-server-local.sh — start the server in background for e2e tests.
# Loads .env.local and overrides Postgres to local Docker test-db.
# Usage: called by `task server:local:start` from apps/server directory.

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
  . "$REPO_ROOT/.env.local"
  set +a
fi

nohup "$SERVER_BIN" --port 3002 >> "$LOG_FILE" 2>> "$ERR_FILE" < /dev/null &
SERVER_PID=$!
echo "$SERVER_PID" > "$PID_FILE"
echo "Server started (PID $SERVER_PID) on port 3002"
