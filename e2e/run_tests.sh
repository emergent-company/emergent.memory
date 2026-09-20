#!/usr/bin/env bash
# run_tests.sh — build the Docker test image and run the emergent CLI install tests.
#
# Usage:
#   ./run_tests.sh                          # full stack (server + tests)
#   ./run_tests.sh --tests-only             # skip server, run tests against MEMORY_TEST_SERVER
#   ./run_tests.sh --build-only             # build image, don't run
#   MEMORY_SERVER_IMAGE=... ./run_tests.sh
#   TEST_RUN=TestCLIInstalled_Version ./run_tests.sh --tests-only
#
# Exit code mirrors the Go test exit code.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_DIR="${SCRIPT_DIR}/test-logs"
RUNS_DB_DIR="${SCRIPT_DIR}/logs"
TESTS_ONLY=false
BUILD_ONLY=false

# ─── Parse flags ──────────────────────────────────────────────────────────────
for arg in "$@"; do
  case "$arg" in
    --tests-only) TESTS_ONLY=true ;;
    --build-only) BUILD_ONLY=true ;;
    *) echo "Unknown argument: $arg" >&2; exit 1 ;;
  esac
done

# ─── Ensure log directory exists ──────────────────────────────────────────────
mkdir -p "$LOG_DIR"
mkdir -p "$RUNS_DB_DIR"
echo "> test session logs will be written to: $LOG_DIR"
echo "> runs DB will be written to: $RUNS_DB_DIR/runs.db"

# ─── Build the test image ─────────────────────────────────────────────────────
echo "> building Docker test image..."
docker build -t emergent-cli-install-tests "$SCRIPT_DIR"

if $BUILD_ONLY; then
  echo "> build complete (--build-only, not running tests)"
  exit 0
fi

# ─── Run the tests ────────────────────────────────────────────────────────────
if $TESTS_ONLY; then
  # Run test container only — requires MEMORY_TEST_SERVER to point at a live server.
  SERVER_URL="${MEMORY_TEST_SERVER:-http://localhost:3002}"
  echo "> running tests against $SERVER_URL (--tests-only mode)"

  docker run --rm \
    --name emergent-cli-install-tests-run \
    -e MEMORY_TEST_SERVER="$SERVER_URL" \
    -e TEST_LOG_DIR="/test-logs" \
    -e TEST_RUNNER="docker" \
    -e CI=true \
    -e TEST_RUN="${TEST_RUN:-}" \
    -v "$LOG_DIR:/test-logs" \
    -v "$RUNS_DB_DIR:/tests/logs" \
    emergent-cli-install-tests
else
  # Full stack: bring up the server + run tests via docker-compose.
  echo "> starting full test stack (server + tests)..."

  # Bring down any stale containers from previous runs.
  docker compose -f "$SCRIPT_DIR/docker-compose.yml" down --remove-orphans 2>/dev/null || true

  # Run; --abort-on-container-exit stops everything when any container exits.
  # --exit-code-from captures the exit code of the test client.
  docker compose -f "$SCRIPT_DIR/docker-compose.yml" \
    up \
    --build \
    --abort-on-container-exit \
    --exit-code-from test-emergent-client

  # Bring down cleanly regardless of outcome.
  docker compose -f "$SCRIPT_DIR/docker-compose.yml" down --remove-orphans 2>/dev/null || true
fi

echo "> done. logs: $LOG_DIR"
