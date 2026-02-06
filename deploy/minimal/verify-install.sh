#!/bin/bash

INSTALL_DIR="${INSTALL_DIR:-$HOME/emergent-standalone}"
SERVER_PORT="${SERVER_PORT:-3002}"

echo "Emergent Installation Verification"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

cd "$INSTALL_DIR/deploy/minimal" 2>/dev/null || {
    echo "❌ Installation directory not found: $INSTALL_DIR"
    echo "   Have you run the installer yet?"
    exit 1
}

echo "✅ Installation directory found"

if ! docker compose -f docker-compose.local.yml ps | grep -q "emergent-server"; then
    echo "❌ Emergent server container not found"
    echo "   Run: docker compose -f docker-compose.local.yml up -d"
    exit 1
fi

echo "✅ Server container running"

if ! curl -sf http://localhost:$SERVER_PORT/health > /dev/null 2>&1; then
    echo "❌ Server health check failed"
    echo "   Check logs: docker logs emergent-server"
    exit 1
fi

echo "✅ Server health check passed"

if ! docker exec emergent-server emergent-cli status > /dev/null 2>&1; then
    echo "❌ CLI authentication failed"
    echo "   Check API key in credentials.txt"
    exit 1
fi

echo "✅ CLI authentication successful"

PROJECT_COUNT=$(docker exec emergent-server emergent-cli projects list --output json 2>/dev/null | jq '. | length' 2>/dev/null || echo "0")

if [ "$PROJECT_COUNT" -gt 0 ]; then
    echo "✅ Default project created ($PROJECT_COUNT project(s) found)"
else
    echo "⚠️  No projects found (may need manual creation)"
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Installation Status: ✅ HEALTHY"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Server URL: http://localhost:$SERVER_PORT"
echo "Credentials: $INSTALL_DIR/deploy/minimal/credentials.txt"
echo ""
echo "Next steps:"
echo "  docker exec emergent-server emergent-cli projects list"
echo "  cat $INSTALL_DIR/deploy/minimal/credentials.txt"
echo ""
