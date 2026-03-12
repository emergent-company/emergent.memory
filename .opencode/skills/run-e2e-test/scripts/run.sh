#!/usr/bin/env bash
# run.sh — run e2e tests against a configured environment.
#
# Usage:
#   bash .opencode/skills/run-e2e-test/scripts/run.sh
#   bash .opencode/skills/run-e2e-test/scripts/run.sh TestAINewsBlueprint_InstallAndRun
#   bash .opencode/skills/run-e2e-test/scripts/run.sh localhost TestCLIInstalled_Version
#
# Args:
#   $1  (optional) env name or test filter
#       - if it looks like a test name (starts with "Test") → used as filter against default env
#       - otherwise → treated as env name (your-server | localhost | <blank for base .env>)
#   $2  (optional) test filter when $1 is an env name

set -euo pipefail

export PATH="/root/.local/bin:$PATH"

E2E_DIR="/root/emergent.memory.e2e"

ENV_NAME="${DEFAULT_E2E_ENV:-localhost}"
FILTER=""

if [[ $# -ge 1 ]]; then
  if [[ "$1" == Test* ]]; then
    FILTER="$1"
  else
    ENV_NAME="$1"
    [[ $# -ge 2 ]] && FILTER="$2"
  fi
fi

ARGS=("$ENV_NAME")
[[ -n "$FILTER" ]] && ARGS+=("$FILTER")

# AI news blueprint and Orchestrator tests need a long timeout — pass it through.
if [[ "$FILTER" == *AINews* || "$FILTER" == *Blueprint* || "$FILTER" == *V2Orchestrator* || "$FILTER" == *V3Orchestrator* || "$FILTER" == *Orchestrator* ]]; then
  exec bash "$E2E_DIR/test" "${ARGS[@]}" -- -timeout 60m
else
  exec bash "$E2E_DIR/test" "${ARGS[@]}"
fi
