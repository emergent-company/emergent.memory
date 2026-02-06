#!/bin/sh
#
# Emergent CLI Wrapper
# Automatically configures environment for CLI commands
#
# Usage: emergent [command] [args...]
#

# Set server URL (inside container, localhost:3002)
export EMERGENT_SERVER_URL="${EMERGENT_SERVER_URL:-http://localhost:3002}"

# Read API key from environment (set by docker-compose)
export EMERGENT_API_KEY="${EMERGENT_API_KEY:-$STANDALONE_API_KEY}"

# Execute the CLI with all passed arguments
exec /usr/local/bin/emergent-cli "$@"
