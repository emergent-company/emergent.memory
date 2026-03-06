#!/usr/bin/env bash
# Start emergent development services from a stopped state
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "Starting Go server..."
task start
echo ""
sleep 2
bash "$(dirname "$0")/status.sh"
