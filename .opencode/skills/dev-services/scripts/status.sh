#!/usr/bin/env bash
# Check status of emergent development services
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "=== Emergent Dev Services Status ==="
echo ""

# Server
echo "--- Go Server (port 3012) ---"
task status 2>/dev/null || echo "Server: NOT RUNNING"
echo ""

# Database
echo "--- Database (port 5436) ---"
if pg_isready -h localhost -p 5436 -q 2>/dev/null; then
  echo "Postgres: RUNNING on :5436"
else
  echo "Postgres: NOT RESPONDING on :5436"
fi
echo ""

# Log files
echo "--- Recent logs ---"
echo "Server:  logs/server/server.log"
echo "Errors:  logs/server/server.error.log"
if [ -f logs/server/server.error.log ]; then
  ERRORS=$(tail -5 logs/server/server.error.log 2>/dev/null)
  [ -n "$ERRORS" ] && echo "Last 5 error lines:" && echo "$ERRORS"
fi
