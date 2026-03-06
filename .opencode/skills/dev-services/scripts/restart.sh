#!/usr/bin/env bash
# Restart emergent development services
# Usage: restart.sh [--clean]
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

CLEAN=0
for arg in "$@"; do
  [ "$arg" = "--clean" ] && CLEAN=1
done

echo "Stopping server..."
task stop 2>/dev/null || true

if [ "$CLEAN" = "1" ]; then
  echo "Clean build..."
  rm -f apps/server-go/dist/server
fi

echo "Starting server..."
task start
echo ""
sleep 2
bash "$(dirname "$0")/status.sh"
