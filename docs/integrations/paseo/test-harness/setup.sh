#!/usr/bin/env bash
# Stand up an ISOLATED Paseo test daemon for the Memory ACP provider.
#
# The isolated instance uses:
#   - its own home dir        (default /root/paseo-acp-test; override with
#                              PASEO_ACP_TEST_HOME)
#   - its own listen port     127.0.0.1:6768 (the live daemon holds 0.0.0.0:6767)
#   - its own config.json     (browser tools + MCP injection + relay + webUi off)
#   - a dedicated wrapper     memory-acp-test-wrapper.sh, loading its own env file
#
# It never writes to, reloads, or restarts the live daemon at ~/.paseo.
#
# Usage:
#   ./setup.sh                 # create home + config + wrapper + env example
#   ./setup.sh --start         # also start the test daemon in the background
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_HOME="${PASEO_ACP_TEST_HOME:-/root/paseo-acp-test}"
LISTEN="${PASEO_ACP_TEST_LISTEN:-127.0.0.1:6768}"

mkdir -p "$TEST_HOME"

# --- wrapper ---------------------------------------------------------------
install -m 0755 "$HERE/memory-acp-test-wrapper.sh" "$TEST_HOME/memory-acp-test-wrapper.sh"

# --- config.json (substitute the placeholder) -------------------------------
sed -e "s|__PASEO_ACP_TEST_HOME__|$TEST_HOME|g" \
    -e "s|127.0.0.1:6768|$LISTEN|g" \
    "$HERE/config.json" > "$TEST_HOME/config.json"
chmod 0644 "$TEST_HOME/config.json"

# --- env file (never committed; never contains a real token until you add one)
if [ ! -f "$TEST_HOME/memory-acp.env" ]; then
  cat > "$TEST_HOME/memory-acp.env" <<'EOF'
# Isolated test credentials for the ACP test daemon. Fill these in, then chmod 600.
# Do NOT commit this file and do NOT paste a real token into config.json.
MEMORY_SERVER_URL=https://api.dev.emergent-company.ai
MEMORY_PROJECT_TOKEN=emt_REPLACE_ME_WITH_A_REAL_PROJECT_TOKEN
MEMORY_PROJECT_ID=REPLACE_ME_WITH_PROJECT_UUID
MEMORY_AGENT=acp-probe-external
EOF
  chmod 600 "$TEST_HOME/memory-acp.env"
fi

echo "test home:  $TEST_HOME"
echo "listen:     $LISTEN"
echo "config:     $TEST_HOME/config.json"
echo "wrapper:    $TEST_HOME/memory-acp-test-wrapper.sh"
echo "env file:   $TEST_HOME/memory-acp.env  (fill in, chmod 600)"

case "${1:-}" in
  --start|-s)
    paseo daemon start --home "$TEST_HOME"
    ;;
esac
