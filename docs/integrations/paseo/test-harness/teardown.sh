#!/usr/bin/env bash
# Stop and tear down the isolated Paseo test daemon.
#
# Stops the test daemon (its own --home only — never the live ~/.paseo daemon).
# Pass --wipe to also delete the test home directory. The env file is removed
# only with --wipe, so a re-run reuses your credentials.
set -euo pipefail

TEST_HOME="${PASEO_ACP_TEST_HOME:-/root/paseo-acp-test}"

if paseo daemon status --home "$TEST_HOME" >/dev/null 2>&1; then
  paseo daemon stop --home "$TEST_HOME"
fi

case "${1:-}" in
  --wipe|-w)
    rm -rf "$TEST_HOME"
    echo "removed $TEST_HOME"
    ;;
esac

echo "test daemon stopped (live ~/.paseo daemon untouched)"
