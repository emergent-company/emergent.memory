#!/usr/bin/env bash
# entrypoint.sh — installs the memory CLI built from the checked-out source,
# then runs the Go test suite.
#
# This script does not fetch install.sh from raw.githubusercontent.com: the CLI
# under test is the one built from the source in the tree. The Dockerfile builds
# the `memory` binary from apps/cli and stages it at /opt/memory/memory; this
# script installs it into $HOME/.memory/bin — the exact destination install.sh
# uses — and then runs `memory version`.
#
# Environment variables consumed here:
#   CI=true               — non-interactive marker
#   MEMORY_TEST_SERVER    — Memory API server URL (set by docker-compose.yml)
#   TEST_LOG_DIR          — where test session logs are written (default /test-logs)
#   TEST_RUN              — optional -run filter, e.g. TestCLIInstalled_Version

set -euo pipefail

BUILT_BINARY="/opt/memory/memory"
INSTALL_DIR="${HOME}/.memory/bin"

echo "=== memory CLI install test entrypoint ==="
echo ">>> installing memory CLI from ${BUILT_BINARY} into ${INSTALL_DIR} ..."

# Mirror install.sh's install step: place the binary at $HOME/.memory/bin/memory
# and make it executable.  Hard-fail (no silent fallback) if the staged binary
# is missing.
if [ ! -x "${BUILT_BINARY}" ]; then
    echo "ERROR: built memory binary not found at ${BUILT_BINARY}.  Aborting." >&2
    exit 1
fi

mkdir -p "${INSTALL_DIR}"
cp "${BUILT_BINARY}" "${INSTALL_DIR}/memory"
chmod 0755 "${INSTALL_DIR}/memory"

# Ensure the install directory is on PATH for this session.
export PATH="${INSTALL_DIR}:${PATH}"

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
exec go test -v -timeout 10m -run "${TEST_RUN:-TestCLIInstalled_(Version|Help|SetToken|StatusAuthenticated|ConfigShowAndSet)|TestOpencode}" ./tests/cli/...
