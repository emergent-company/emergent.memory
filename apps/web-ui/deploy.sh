#!/usr/bin/env bash
# Memory deploy pipeline.
#
#   1. Gateway  -> deploy the Go gateway (+ supervisor + web UI + Python bridge)
#                 via docker-compose to $GATEWAY_DIR (default /opt/memory,
#                 local; set GATEWAY_HOST to deploy over ssh).
#   2. iOS      -> Mac: rsync apps/ios/ + xcodebuild (VoiceAgent, iOS Simulator)
#
# The legacy Python backends (agent/admin.py, agent/api FastAPI, agent.worker
# per-agent systemd units) are retired — the gateway is the single client-facing
# origin and its supervisor spawns one bridge worker per agent.
#
# The Mac target (host + checkout path + subtree) is NOT hardcoded here: step
# 2/3 delegates to tools/ios-build-mac.sh, which resolves it from the shared
# config (apps/connector.mac/Scripts/lib/mac-remote.sh + the gitignored apps/connector.mac/.env). Set
# MEMORY_MAC_HOST / MEMORY_MAC_PATH there — see apps/connector.mac/.env.example.
#
# Deploys the CURRENT working tree (no git step). Run after every change:
#     ./deploy.sh
set -euo pipefail

GATEWAY_HOST="${GATEWAY_HOST:-}"          # gateway host (SSH alias in ~/.ssh/config); empty = local
GATEWAY_DIR="${GATEWAY_DIR:-/opt/memory}" # gateway deploy directory
SRC_ROOT="$(cd "$(dirname "$0")" && pwd)"

step() { printf '\n==> %s\n' "$*"; }

# --- 1. gateway ------------------------------------------------------------
if [ -n "$GATEWAY_HOST" ]; then
  target="$GATEWAY_HOST"
else
  target="localhost"
fi
step "1/3 deploy gateway -> $target (docker compose up --build)"
# Sync the gateway module + bridge + compose files, then rebuild/restart the
# container in $GATEWAY_DIR (local, or on $GATEWAY_HOST over ssh). The checkout's
# .env is never shipped (secrets): the local path copies it once only if
# $GATEWAY_DIR/.env is absent, and the remote path expects an existing .env.
files="gateway memory_bridge docker-compose.yml Dockerfile pyproject.toml uv.lock"
if [ -n "$GATEWAY_HOST" ]; then
  ssh "$GATEWAY_HOST" "mkdir -p $GATEWAY_DIR"
  tar -C "$SRC_ROOT" -czf - $files \
    | ssh "$GATEWAY_HOST" "tar -C $GATEWAY_DIR -xzf -"
  if ! ssh "$GATEWAY_HOST" "test -f $GATEWAY_DIR/.env"; then
    printf 'warning: %s/.env is missing on %s; create it before the container starts\n' "$GATEWAY_DIR" "$GATEWAY_HOST" >&2
  fi
  ssh "$GATEWAY_HOST" "cd $GATEWAY_DIR && docker compose up -d --build"
else
  mkdir -p "$GATEWAY_DIR"
  tar -C "$SRC_ROOT" -czf - $files \
    | tar -C "$GATEWAY_DIR" -xzf -
  if [ ! -f "$GATEWAY_DIR/.env" ] && [ -f "$SRC_ROOT/.env" ]; then
    cp "$SRC_ROOT/.env" "$GATEWAY_DIR/.env"
    printf 'warning: seeded %s/.env from the checkout .env (first deploy)\n' "$GATEWAY_DIR" >&2
  fi
  cd "$GATEWAY_DIR" && docker compose up -d --build
fi

# --- 2. iOS source -> Mac, and 3. build on the Mac -------------------------
# Delegated to the shared iOS build script so the host/path resolution, the
# guarded rsync (scoped --delete into client/ios) and the xcodebuild invocation
# all come from one place (apps/connector.mac/Scripts/lib/mac-remote.sh). No Mac host or path is
# hardcoded in this file.
step "2/3 sync + build iOS on the Mac (tools/ios-build-mac.sh)"
"$SRC_ROOT/tools/ios-build-mac.sh"

step "deploy complete"
