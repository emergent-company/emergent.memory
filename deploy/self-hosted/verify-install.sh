#!/bin/bash
#
# Verify a self-hosted Memory installation created by install-online.sh.
#
#   ./verify-install.sh
#   INSTALL_DIR=/opt/memory SERVER_PORT=8080 ./verify-install.sh
#
set -uo pipefail

INSTALL_DIR="${INSTALL_DIR:-$HOME/.memory}"
SERVER_PORT="${SERVER_PORT:-3002}"

COMPOSE_FILE="${INSTALL_DIR}/docker/docker-compose.yml"
ENV_FILE="${INSTALL_DIR}/config/.env.local"

echo "Memory Installation Verification"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

fail() {
    echo "✗ $1"
    shift
    for line in "$@"; do echo "   $line"; done
    exit 1
}

if [[ ! -f "$COMPOSE_FILE" ]]; then
    fail "No installation found at $INSTALL_DIR" \
        "Expected $COMPOSE_FILE" \
        "Run install-online.sh first, or set INSTALL_DIR to the right directory."
fi
echo "✓ Installation directory: $INSTALL_DIR"

compose() {
    docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" "$@"
}

if ! compose ps --status running --services 2>/dev/null | grep -qx server; then
    fail "Server container is not running" \
        "Start it: docker compose -f $COMPOSE_FILE --env-file $ENV_FILE up -d" \
        "Logs:    docker compose -f $COMPOSE_FILE logs server --tail 50"
fi
echo "✓ Server container running"

if ! curl -sf "http://localhost:${SERVER_PORT}/health" > /dev/null 2>&1; then
    fail "Health check failed on http://localhost:${SERVER_PORT}/health" \
        "Logs: docker compose -f $COMPOSE_FILE logs server --tail 50"
fi
echo "✓ Health check passed"

server_id="$(compose ps -q server 2>/dev/null | head -1)"
if [[ -n "$server_id" ]] && ! docker exec "$server_id" memory status > /dev/null 2>&1; then
    fail "CLI authentication inside the container failed" \
        "Check the API key in $ENV_FILE (STANDALONE_API_KEY)."
fi
echo "✓ CLI authentication passed"

if [[ -x "${INSTALL_DIR}/bin/memory" ]]; then
    echo "✓ Host CLI: ${INSTALL_DIR}/bin/memory"
else
    echo "! Host CLI not found at ${INSTALL_DIR}/bin/memory (install-online.sh reports this;"
    echo "  the container CLI still works: docker exec \$server_id memory <command>)"
fi

echo ""
echo "Installation looks healthy. Server: http://localhost:${SERVER_PORT}"
