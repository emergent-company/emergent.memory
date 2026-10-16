#!/bin/bash
# run-e2e.sh — start mock memory + gateway, block until killed.
# Playwright's webServer runs this and kills it when tests finish.
set -euo pipefail
cd "$(dirname "$0")/../.."   # repo root

# Ports. The gateway defaults to 8097 (avoids the shared dev gateway on 8095) and
# the mock to 5301; both are overridable so two suites can run concurrently on
# one machine/runner without sharing a server. CI sets a distinct MEMORY_PORT/
# MOCK_PORT per job (see .github/workflows/web-ui.yml), so the `datasources` and
# `share` lanes never collide.
PORT="${MEMORY_PORT:-8097}"
MOCK_PORT="${MOCK_PORT:-5301}"

# Pre-flight: this harness is deliberately self-contained (Playwright runs it
# with reuseExistingServer:false). If either port is already served, the gateway
# or mock bind fails, and with a backgrounded job + `set -e` that failure is
# silent — the readiness check below would then be satisfied by the *foreign*
# server and the specs would run against unrelated state. Fail fast and loudly
# instead of piggybacking.
port_in_use() {
  # true when something is already accepting TCP connections on 127.0.0.1:$1
  (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}
for _port in "$PORT" "$MOCK_PORT"; do
  if port_in_use "$_port"; then
    echo "run-e2e.sh: port $_port is already in use — refusing to reuse a foreign server. Stop whatever is listening there, or set MEMORY_PORT/MOCK_PORT to free ports." >&2
    exit 1
  fi
done
unset _port

# Build the gateway binary. On an unwarmed tree (e.g. a fresh worktree) the
# generated *_templ.go files and the compiled Tailwind stylesheet
# (webui/static/css/app.css) are gitignored and absent, and `go build` fails
# without them — webui.go's cssGuard turns a missing app.css into a compile
# error. Generate whichever is missing, then build fresh so a stale binary is
# never served.
(cd gateway && {
  if ! compgen -G '*_templ.go' >/dev/null; then
    # `go run` pins templ to the go.mod version (same invocation as the
    # gateway CI job) and needs only the Go toolchain — no global templ binary.
    go run github.com/a-h/templ/cmd/templ generate ./...
  fi
  if [ ! -f webui/static/css/app.css ]; then
    command -v task >/dev/null || { echo "run-e2e.sh: task not found — add it to PATH (e.g. export PATH=\"/root/go/bin:\$PATH\") to compile app.css" >&2; exit 1; }
    task css
  fi
  go build -o memory .
})

# mock memory
MOCK_PORT="$MOCK_PORT" node tests/e2e/mock-memory.mjs &
MOCK=$!

# gateway (points at the mock). AUTH_MODE=dev keeps the no-auth mock harness
# usable (the default is "session", which needs Zitadel). The share surface is
# enabled by its two secrets; the public base pins copyable owner URLs and the
# share cookie to http://localhost (no Secure flag, so cookies work over http).
MEMORY_URL="http://localhost:$MOCK_PORT" \
AGENT_TRIGGER_TOKEN=test-token \
MEMORY_PROJECT_ID=p1 \
MEMORY_PORT="$PORT" \
AUTH_MODE=dev \
MEMORY_COMPONENT_GALLERY=on \
FEEDBACK_OVERLAY_URL="http://localhost:$MOCK_PORT" \
PUBLIC_BASE_URL="http://localhost:$PORT" \
SHARE_PUBLIC_BASE_URL="http://localhost:$PORT" \
SHARE_COOKIE_SECRET=dev-share-cookie-secret \
SHARE_REF_SECRET=dev-share-ref-secret \
SHARE_RATE_IP_PER_MIN=100000 \
SHARE_RATE_IP_BURST=100000 \
SHARE_RATE_LINK_PER_MIN=100000 \
SHARE_RATE_LINK_BURST=100000 \
BRIDGE_BIN=/bin/true \
SUPERVISOR_INTERVAL=60s \
./gateway/memory &
GW=$!

cleanup() { kill "$MOCK" "$GW" 2>/dev/null || true; }
trap cleanup EXIT

# Wait for readiness — and for *our* process, not a sibling's. The pre-flight
# guard above should make a bind failure impossible, but a backgrounded process
# that dies (bind race, startup crash) otherwise leaves this loop to be answered
# by whoever holds the port, so re-check both PIDs each pass and fail loudly.
ready=0
for _ in $(seq 1 60); do
  if ! kill -0 "$MOCK" 2>/dev/null; then
    echo "run-e2e.sh: mock-memory.mjs exited before the gateway was ready (port $MOCK_PORT)." >&2
    exit 1
  fi
  if ! kill -0 "$GW" 2>/dev/null; then
    echo "run-e2e.sh: gateway exited before becoming ready on port $PORT (bind failure or startup crash)." >&2
    exit 1
  fi
  curl -sf "http://localhost:$PORT/api/health" >/dev/null 2>&1 && { ready=1; break; }
  sleep 0.5
done
if [ "$ready" -ne 1 ]; then
  echo "run-e2e.sh: gateway did not become ready on port $PORT within 30s." >&2
  exit 1
fi

# block until killed
sleep infinity
