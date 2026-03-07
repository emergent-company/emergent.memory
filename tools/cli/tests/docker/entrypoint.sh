#!/usr/bin/env bash
# entrypoint.sh — installs the memory CLI from the GitHub release at runtime,
# then runs the Go test suite.
#
# This simulates exactly what a real user does:
#
#   curl -fsSL https://raw.githubusercontent.com/.../install.sh | bash
#
# The CLI is NOT baked into the Docker image.  It is downloaded fresh every
# time the container starts so the tests exercise the real install path.
#
# Environment variables consumed here:
#   CI=true                 — suppresses the interactive Google API-key prompt in install.sh
#   MEMORY_TEST_SERVER    — Emergent API server URL (set by docker-compose.yml)
#   TEST_LOG_DIR            — where test session logs are written (default /test-logs)

set -euo pipefail

INSTALL_URL="https://raw.githubusercontent.com/emergent-company/emergent.memory/main/tools/cli/install.sh"

echo "=== memory CLI install test entrypoint ==="
echo ">>> installing memory CLI from ${INSTALL_URL} ..."

# Run the real-user install command.  CI=true prevents install.sh from
# asking interactively for a Google API key.  set -euo pipefail means this
# script will exit non-zero immediately if the install fails.
CI=true curl -fsSL "${INSTALL_URL}" | bash

# Ensure the install directory is on PATH for this session.
# install.sh puts the binary at $HOME/.memory/bin by default.
export PATH="${HOME}/.memory/bin:${PATH}"

# Hard-fail if the binary isn't reachable — no silent fallback.
if ! command -v memory >/dev/null 2>&1; then
    echo "ERROR: memory binary not found on PATH after install.  Aborting." >&2
    exit 1
fi

echo ">>> memory installed at: $(command -v memory)"
memory version

# Create the log directory so tests never race on it.
mkdir -p "${TEST_LOG_DIR:-/test-logs}"

echo ">>> running Go tests ..."
cd /tests
exec go test -v -timeout 10m ./...
