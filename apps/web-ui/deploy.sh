#!/usr/bin/env bash
# Memory deploy pipeline.
#
#   1. Gateway  -> deploy the Go gateway (+ supervisor + web UI + Python bridge)
#                 via docker-compose on the gateway host (see docker-compose.yml).
#   2. iOS      -> Mac: rsync apps/ios/ + xcodebuild (VoiceAgent, iOS Simulator)
#
# The legacy Python backends (agent/admin.py, agent/api FastAPI, agent.worker
# per-agent systemd units) are retired — the gateway is the single client-facing
# origin and its supervisor spawns one bridge worker per agent.
#
# The Mac target (host + checkout path + subtree) is NOT hardcoded here: step
# 2/3 delegates to tools/ios-build-mac.sh, which resolves it from the shared
# config (tools/lib/mac-remote.sh + the gitignored apps/web-ui/.env). Set
# MEMORY_MAC_HOST / MEMORY_MAC_PATH there — see tools/mac-remote.env.example.
#
# Deploys the CURRENT working tree (no git step). Run after every change:
#     ./deploy.sh
set -euo pipefail

GATEWAY_HOST="home2"          # gateway host (SSH alias in ~/.ssh/config)
SRC_ROOT="$(cd "$(dirname "$0")" && pwd)"

step() { printf '\n==> %s\n' "$*"; }

# --- 1. gateway ------------------------------------------------------------
step "1/3 deploy gateway -> $GATEWAY_HOST (docker compose up --build)"
# Sync the gateway module + bridge + compose files, then rebuild/restart the
# container on the gateway host.
tar -C "$SRC_ROOT" -czf - gateway memory_bridge docker-compose.yml Dockerfile .env \
  | ssh "$GATEWAY_HOST" "tar -C /root/emergent.memory/apps/web-ui -xzf -"
ssh "$GATEWAY_HOST" "cd /root/emergent.memory/apps/web-ui && docker compose up -d --build"

# --- 2. iOS source -> Mac, and 3. build on the Mac -------------------------
# Delegated to the shared iOS build script so the host/path resolution, the
# guarded rsync (scoped --delete into client/ios) and the xcodebuild invocation
# all come from one place (tools/lib/mac-remote.sh). No Mac host or path is
# hardcoded in this file.
step "2/3 sync + build iOS on the Mac (tools/ios-build-mac.sh)"
"$SRC_ROOT/tools/ios-build-mac.sh"

step "deploy complete"
