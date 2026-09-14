#!/usr/bin/env bash
# Memory deploy pipeline.
#
#   1. Gateway  -> deploy the Go gateway (+ supervisor + web UI + Python bridge)
#                 via docker-compose on the gateway host (see docker-compose.yml).
#   2. iOS      -> Mac (mcj-mini-2-1): rsync client/ios/
#   3. Build    -> Mac: xcodebuild (VoiceAgent scheme, iOS Simulator)
#
# The legacy Python backends (agent/admin.py, agent/api FastAPI, agent.worker
# per-agent systemd units) are retired — the gateway is the single client-facing
# origin and its supervisor spawns one bridge worker per agent.
#
# Deploys the CURRENT working tree (no git step). Run after every change:
#     ./deploy.sh
set -euo pipefail

GATEWAY_HOST="home2"          # gateway host (SSH alias in ~/.ssh/config)
MAC_HOST="mcj-mini-2-1"       # Mac hostname (Tailscale MagicDNS)
MAC_USER="mcj"
MAC_IOS_DIR="/Users/mcj/alfred/client/ios"
SRC_ROOT="$(cd "$(dirname "$0")" && pwd)"

step() { printf '\n==> %s\n' "$*"; }

# --- 1. gateway ------------------------------------------------------------
step "1/3 deploy gateway -> $GATEWAY_HOST (docker compose up --build)"
# Sync the gateway module + bridge + compose files, then rebuild/restart the
# container on the gateway host.
tar -C "$SRC_ROOT" -czf - gateway memory_bridge docker-compose.yml Dockerfile .env \
  | ssh "$GATEWAY_HOST" "tar -C /root/alfred -xzf -"
ssh "$GATEWAY_HOST" "cd /root/alfred && docker compose up -d --build"

# --- 2. iOS source -> Mac --------------------------------------------------
step "2/3 sync iOS source -> $MAC_HOST"
rsync -avz --delete \
  --exclude 'xcuserdata' \
  --exclude '.DS_Store' \
  --exclude 'swiftpm/configuration' \
  --exclude '.swiftpm' \
  "$SRC_ROOT/client/ios/" "$MAC_USER@$MAC_HOST:$MAC_IOS_DIR/"

# --- 3. build on Mac -------------------------------------------------------
step "3/3 build on $MAC_HOST"
ssh "$MAC_USER@$MAC_HOST" \
  "cd '$MAC_IOS_DIR' && xcodebuild -project VoiceAgent.xcodeproj -scheme VoiceAgent -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO build"

step "deploy complete"
