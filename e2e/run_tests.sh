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
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
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

# ─── Compose teardown on ANY exit path ────────────────────────────────────────
# A cancelled/killed run (SIGINT/SIGTERM) used to leave the per-run
# `e2e-<runid>-integration_default` network (and its anonymous volumes) behind
# (#1876). Trap exit + signals so the stack is torn down no matter how the
# script ends. `-v` drops anonymous volumes. No-op in --tests-only/--build-only
# (those modes never start a compose stack).
cleanup() {
  if $TESTS_ONLY || $BUILD_ONLY; then
    return 0
  fi
  docker compose -f "$SCRIPT_DIR/docker-compose.yml" down -v --remove-orphans 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# ─── Ensure log directory exists ──────────────────────────────────────────────
mkdir -p "$LOG_DIR"
mkdir -p "$RUNS_DB_DIR"
echo "> test session logs will be written to: $LOG_DIR"
echo "> runs DB will be written to: $RUNS_DB_DIR/runs.db"

# ─── Build the test image ─────────────────────────────────────────────────────
# The build context is the REPO ROOT: the Dockerfile compiles the memory CLI
# from apps/cli (see e2e/Dockerfile). The repo is private, so the test no longer
# downloads the CLI/install script from GitHub.
#
# Buildx builder pinning (infra #335): the `memory` self-hosted runner pool spans
# two hosts. On the Linux runner the daemon-default buildx builder (docker driver)
# hands build containers systemd-resolved's stub resolver 127.0.0.53, which is
# unreachable from the build network namespace, so every network `RUN` dies
# instantly: `dial tcp: lookup proxy.golang.org on 127.0.0.53:53: network is
# unreachable` (the "Preflight: build test image (fetches opencode)" step). Named
# docker-container builders on the same host resolve fine. When the caller pins a
# builder via E2E_BUILDX_BUILDER, use it here and — only when set — export it as
# BUILDX_BUILDER so the compose build below selects the same builder. Unset or
# empty keeps the current daemon-default behaviour so local runs are unaffected.
if [ -n "${E2E_BUILDX_BUILDER:-}" ]; then
  # Compose selects its buildx builder via the BUILDX_BUILDER env var; export it
  # (only when pinned) so the `docker compose up -d --build` below builds on the
  # same builder.
  export BUILDX_BUILDER="$E2E_BUILDX_BUILDER"
fi
echo "> building Docker test image..."
if [ -n "${E2E_BUILDX_BUILDER:-}" ]; then
  docker buildx build --load --builder "$E2E_BUILDX_BUILDER" \
    -f "$SCRIPT_DIR/Dockerfile" -t emergent-cli-install-tests "$REPO_ROOT"
else
  docker build -f "$SCRIPT_DIR/Dockerfile" -t emergent-cli-install-tests "$REPO_ROOT"
fi

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
    -e GOWORK=off \
    -e TEST_RUN="${TEST_RUN:-}" \
    -v "$LOG_DIR:/test-logs" \
    -v "$RUNS_DB_DIR:/tests/logs" \
    emergent-cli-install-tests
else
  # Full stack: bring up the server + run tests via docker-compose.
  echo "> starting full test stack (server + tests)..."

  # Bring down any stale containers from previous runs.
  docker compose -f "$SCRIPT_DIR/docker-compose.yml" down --remove-orphans 2>/dev/null || true

  # Bring up the full stack detached. storage-init is a one-shot that creates
  # the document buckets and exits; --abort-on-container-exit would stop the
  # stack on that exit, so wait for the test client explicitly instead.
  docker compose -f "$SCRIPT_DIR/docker-compose.yml" up -d --build

  # Wait for the test client to finish (it exits after the suite runs).
  docker compose -f "$SCRIPT_DIR/docker-compose.yml" wait test-emergent-client || true

  # Print the test client's logs so failures are visible in CI.
  docker compose -f "$SCRIPT_DIR/docker-compose.yml" logs test-emergent-client

  # Capture the test client's exit code for the script's own exit status.
  # Services have no fixed container_name (the compose project is namespaced per
  # CI job), so resolve the container by service name. `ps -aq` includes the
  # already-exited client; plain `ps -q` would return nothing.
  client_id=$(docker compose -f "$SCRIPT_DIR/docker-compose.yml" ps -aq test-emergent-client)
  if [ -n "$client_id" ]; then
    exit_code=$(docker inspect -f '{{.State.ExitCode}}' "$client_id")
  else
    echo "> could not resolve the test-emergent-client container; failing" >&2
    exit_code=1
  fi

  # Bring down cleanly regardless of outcome. `-v` also drops anonymous volumes;
  # the EXIT trap above is the backstop for a cancelled/killed run (#1876).
  docker compose -f "$SCRIPT_DIR/docker-compose.yml" down -v --remove-orphans 2>/dev/null || true

  exit "$exit_code"
fi

echo "> done. logs: $LOG_DIR"
