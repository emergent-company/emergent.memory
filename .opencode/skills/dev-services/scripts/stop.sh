#!/usr/bin/env bash
# Stop all emergent development services
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "Stopping Go server..."
task stop 2>/dev/null && echo "Server stopped." || echo "Server was not running."
