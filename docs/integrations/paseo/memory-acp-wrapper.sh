#!/bin/sh
# Memory ACP provider wrapper for Paseo.
#
# Paseo spawns this script (see ~/.paseo/config.json -> agents.providers.memory).
# It loads operator-controlled credentials from a private env file, then execs
# the Memory CLI in ACP agent mode. Keeping the token in a 0600 file OUTSIDE
# ~/.paseo/config.json avoids embedding secrets in the daemon config.
#
# Override the env file location with MEMORY_ACP_ENV_FILE.
set -eu

ENV_FILE="${MEMORY_ACP_ENV_FILE:-$HOME/.memory/memory-acp.env}"
MEMORY_BIN="${MEMORY_ACP_BIN:-$HOME/.memory/bin/memory}"

# ACP clients probe `--version` on the provider command (Paseo: `paseo provider
# ls` / `diagnostic`). Answer it directly instead of exec'ing the stdio agent,
# which would block waiting on stdin until the probe times out (SIGTERM).
case "${1:-}" in
  --version|-v|version) exec "$MEMORY_BIN" --version ;;
esac

if [ ! -f "$ENV_FILE" ]; then
  echo "memory-acp-wrapper: credentials file not found: $ENV_FILE" >&2
  echo "memory-acp-wrapper: copy $HOME/.memory/memory-acp.env.example and fill it in" >&2
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
