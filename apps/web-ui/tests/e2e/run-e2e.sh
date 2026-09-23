#!/bin/bash
# run-e2e.sh — start mock memory + gateway, block until killed.
# Playwright's webServer runs this and kills it when tests finish.
set -euo pipefail
cd "$(dirname "$0")/../.."   # repo root

# build the gateway binary
(cd gateway && go build -o memory .)

# mock memory
node tests/e2e/mock-memory.mjs &
MOCK=$!

# gateway (points at the mock). AUTH_MODE=dev keeps the no-auth mock harness
# usable (the default is "session", which needs Zitadel). The share surface is
# enabled by its two secrets; the public base pins copyable owner URLs and the
# share cookie to http://localhost (no Secure flag, so cookies work over http).
# Port 8097 avoids clashing with the shared dev gateway (8095).
MEMORY_URL=http://localhost:5301 \
AGENT_TRIGGER_TOKEN=test-token \
MEMORY_PROJECT_ID=p1 \
MEMORY_PORT=8097 \
AUTH_MODE=dev \
PUBLIC_BASE_URL=http://localhost:8097 \
SHARE_PUBLIC_BASE_URL=http://localhost:8097 \
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

# wait for readiness
for _ in $(seq 1 60); do
  curl -sf http://localhost:8097/api/health >/dev/null 2>&1 && break
  sleep 0.5
done

# block until killed
sleep infinity
