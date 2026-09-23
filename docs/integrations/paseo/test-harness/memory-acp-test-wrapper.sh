#!/bin/sh
# Isolated test-instance wrapper for Paseo -> `memory acp`.
#
# Functionally identical to docs/integrations/paseo/memory-acp-wrapper.sh, but
# it loads a DEDICATED env file so the isolated test daemon (its own --home,
# its own listen port) never reads the operator's live credentials file at
# ~/.memory/memory-acp.env.
#
# Env file resolution:
#   - MEMORY_ACP_ENV_FILE  (explicit override — set via the provider's `env`
#                           block in the test config.json)
#   - else $PASEO_ACP_TEST_HOME/memory-acp.env
#   - else /root/paseo-acp-test/memory-acp.env
#
# The Memory binary is MEMORY_ACP_BIN (default /root/.memory/bin/memory).
set -eu

ENV_FILE="${MEMORY_ACP_ENV_FILE:-${PASEO_ACP_TEST_HOME:-/root/paseo-acp-test}/memory-acp.env}"
MEMORY_BIN="${MEMORY_ACP_BIN:-/root/.memory/bin/memory}"

# ACP clients probe `--version` on the provider command (Paseo: `paseo provider
# ls` / `diagnostic`). Answer it directly instead of exec'ing the stdio agent,
# which would block waiting on stdin until the probe times out (SIGTERM).
case "${1:-}" in
  --version|-v|version) exec "$MEMORY_BIN" --version ;;
esac

if [ ! -f "$ENV_FILE" ]; then
  echo "memory-acp-test-wrapper: credentials file not found: $ENV_FILE" >&2
  echo "memory-acp-test-wrapper: run setup.sh, then fill in the test env file" >&2
  exit 1
fi

# shellcheck disable=SC1090
# `set -a` is essential: sourcing a plain VAR=value file only sets shell
# variables, which are NOT inherited by the exec'd `memory acp` process. Without
# it the CLI sees no credentials and falls back to an expired stored session.
set -a
. "$ENV_FILE"
set +a

: "${MEMORY_SERVER_URL:?MEMORY_SERVER_URL missing in $ENV_FILE}"
: "${MEMORY_PROJECT_TOKEN:?MEMORY_PROJECT_TOKEN missing in $ENV_FILE}"
: "${MEMORY_AGENT:?MEMORY_AGENT missing in $ENV_FILE}"

exec "$MEMORY_BIN" acp --agent "$MEMORY_AGENT"
