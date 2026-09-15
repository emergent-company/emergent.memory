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

# gateway (points at the mock)
MEMORY_URL=http://localhost:5301 \
MEMORY_TOKEN=test-token \
MEMORY_PROJECT_ID=p1 \
MEMORY_PORT=8095 \
BRIDGE_BIN=/bin/true \
SUPERVISOR_INTERVAL=60s \
./gateway/memory &
GW=$!

cleanup() { kill "$MOCK" "$GW" 2>/dev/null || true; }
trap cleanup EXIT

# wait for readiness
for _ in $(seq 1 60); do
  curl -sf http://localhost:8095/api/health >/dev/null 2>&1 && break
  sleep 0.5
done

# block until killed
sleep infinity
