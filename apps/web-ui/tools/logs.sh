#!/usr/bin/env bash
# logs.sh — read logs from all deployed Memory services (prod + dev memory stacks + voice infra).
#
# Usage:
#   tools/logs.sh                              # tail 200 lines from every service
#   tools/logs.sh server web-ui                # only matching services (substring, case-insensitive)
#   tools/logs.sh --tail 500 alfred            # 500 lines, all alfred* services
#   tools/logs.sh --since 1h                   # last hour only
#   tools/logs.sh --errors                     # only error/panic/fatal lines
#   tools/logs.sh -f alfred-worker:diane       # live stream one worker
#   tools/logs.sh --host voice                 # voice infra only (home2 LXC 112)
#   tools/logs.sh --host memory                # prod memory stack only (VM 220)
#   tools/logs.sh --host dev                   # dev stack only (LXC 140)
#   tools/logs.sh --list                       # catalog of known services
#
# Env overrides:
#   MEMORY_HOST      prod SSH alias         (default: emergent-memory)
#   MEMORY_PROD_DIR  compose dir on prod    (default: /root/emergent)
#   VOICE_HOST       voice SSH alias        (default: home2)
#   VOICE_LXC        LXC container id       (default: 112)
#   LIVEKIT_COMPOSE  livekit compose file   (default: /opt/livekit/docker-compose.yml)
#   DEV_HOST         dev Proxmox SSH alias  (default: proxmox-zoidberg)
#   DEV_CT           dev LXC container id   (default: 140)
#   DEV_DIR          compose dir on dev     (default: /opt/emergent-dev)
set -euo pipefail

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------
PROD_HOST="${MEMORY_HOST:-emergent-memory}"
PROD_DIR="${MEMORY_PROD_DIR:-/root/emergent}"
VOICE_HOST="${VOICE_HOST:-home2}"
VOICE_LXC="${VOICE_LXC:-112}"
LIVEKIT_COMPOSE="${LIVEKIT_COMPOSE:-/opt/livekit/docker-compose.yml}"
DEV_HOST="${DEV_HOST:-proxmox-zoidberg}"
DEV_CT="${DEV_CT:-140}"
DEV_DIR="${DEV_DIR:-/opt/emergent-dev}"

TAIL=200
SINCE=""
FOLLOW=0
ERRORS=0
HOST_FILTER="all"
SERVICES=()

# ---------------------------------------------------------------------------
# Service catalog
#   group | transport | label | name
#   transport: compose (docker) | journald (systemd)
# ---------------------------------------------------------------------------
read -r -d '' CATALOG <<'EOF' || true
memory|compose|server|server
memory|compose|web-ui|web-ui
memory|compose|postgres|postgres
memory|compose|minio|minio
memory|compose|kreuzberg|kreuzberg
voice|compose|livekit|livekit
voice|compose|redis|redis
voice|journald|alfred|alfred.service
voice|journald|alfred-admin|alfred-admin.service
voice|journald|alfred-api|alfred-api.service
voice|journald|alfred-worker:google-rt|alfred-worker@alfred-google-rt.service
voice|journald|alfred-worker:diane|alfred-worker@diane.service
dev|compose|memory-server|memory-server
dev|compose|web-ui|web-ui
dev|compose|memory-db|memory-db
dev|compose|minio|minio
dev|compose|zitadel|zitadel
dev|compose|login|login
dev|compose|zitadel-db|zitadel-db
EOF

usage() {
  sed -n '2,18p' "$0" | sed 's/^# \{0,1\}//'
}

list_catalog() {
  printf '%-8s %-9s %-24s %s\n' GROUP TRANSPORT LABEL NAME
  printf '%-8s %-9s %-24s %s\n' ----- --------- ----- ----
  while IFS='|' read -r g t l n; do
    [ -n "$g" ] || continue
    printf '%-8s %-9s %-24s %s\n' "$g" "$t" "$l" "$n"
  done <<<"$CATALOG"
}

# ---------------------------------------------------------------------------
# Arg parsing
# ---------------------------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    -n|--tail)    TAIL="${2:-200}"; shift 2 ;;
    -s|--since)   SINCE="${2:-}"; shift 2 ;;
    -f|--follow)  FOLLOW=1; shift ;;
    -e|--errors)  ERRORS=1; shift ;;
    --host)       HOST_FILTER="${2:-all}"; shift 2 ;;
    --list)       list_catalog; exit 0 ;;
    -h|--help)    usage; exit 0 ;;
    -*)           echo "unknown flag: $1" >&2; usage >&2; exit 2 ;;
    *)            SERVICES+=("$1"); shift ;;
  esac
done

# ---------------------------------------------------------------------------
# Selection
# ---------------------------------------------------------------------------
matches_host() {
  [ "$HOST_FILTER" = "all" ] || [ "$HOST_FILTER" = "$1" ]
}

matches_service() {
  local label="$1"
  if [ ${#SERVICES[@]} -eq 0 ]; then
    return 0
  fi
  local s
  for s in "${SERVICES[@]}"; do
    if [[ "${label,,}" == *"${s,,}"* ]]; then
      return 0
    fi
  done
  return 1
}

selected_entries() {
  while IFS='|' read -r g t l n; do
    [ -n "$g" ] || continue
    if matches_host "$g" && matches_service "$l"; then
      printf '%s|%s|%s|%s\n' "$g" "$t" "$l" "$n"
    fi
  done <<<"$CATALOG"
}

# ---------------------------------------------------------------------------
# Command builders (remote side, single connection per host for non-follow)
# ---------------------------------------------------------------------------
DOCKER_OPTS="--no-color"
[ -n "$SINCE" ] && DOCKER_OPTS="$DOCKER_OPTS --since '$SINCE'"

# compose_log_cmd <compose-file-or-dir> <service>
compose_log_cmd() {
  local cfile="$1" svc="$2" args
  if [ "$FOLLOW" -eq 1 ]; then
    args="-f"
  else
    args=""
  fi
  args="$args --tail=$TAIL"
  echo "docker compose logs $args $DOCKER_OPTS '$svc'"
}

# journal_log_cmd <unit>
journal_log_cmd() {
  local unit="$1" args
  args="--no-pager -n $TAIL"
  [ "$FOLLOW" -eq 1 ] && args="-f $args"
  [ -n "$SINCE" ] && args="$args --since '$SINCE'"
  echo "journalctl $args -u '$unit'"
}

# render_err_filter — append error grep if requested
err_filter() {
  if [ "$ERRORS" -eq 1 ]; then
    printf " 2>&1 | grep -iE 'error|panic|fatal|exception|traceback|level=(error|fatal)' || true"
  else
    printf " 2>&1 || true"
  fi
}

# ---------------------------------------------------------------------------
# Non-follow: batch per host into a single ssh (fast), per-service headers
# ---------------------------------------------------------------------------
run_batched() {
  local -a memory_compose=() voice_compose=() voice_journald=() dev_compose=()

  while IFS='|' read -r g t l n; do
    [ -n "$g" ] || continue
    case "$g:$t" in
      memory:compose)   memory_compose+=("$l|$n") ;;
      voice:compose)    voice_compose+=("$l|$n") ;;
      voice:journald)   voice_journald+=("$l|$n") ;;
      dev:compose)      dev_compose+=("$l|$n") ;;
    esac
  done < <(selected_entries)

  # --- prod memory stack ---
  if [ ${#memory_compose[@]} -gt 0 ]; then
    {
      echo "cd '$PROD_DIR'"
      local e l n
      for e in "${memory_compose[@]}"; do
        l="${e%%|*}"; n="${e##*|}"
        echo "printf '%s\\n' '===== $PROD_HOST / $l ====='"
        echo "docker compose logs $( [ $FOLLOW -eq 1 ] && echo -n '-f ' )--tail=$TAIL $DOCKER_OPTS '$n'$(err_filter)"
      done
    } | ssh "$PROD_HOST" "bash -s"
  fi

  # --- voice infra ---
  if [ ${#voice_compose[@]} -gt 0 ] || [ ${#voice_journald[@]} -gt 0 ]; then
    {
      local e l n
      for e in "${voice_compose[@]}"; do
        l="${e%%|*}"; n="${e##*|}"
        echo "printf '%s\\n' '===== $VOICE_HOST / $l ====='"
        echo "docker compose -f '$LIVEKIT_COMPOSE' logs $( [ $FOLLOW -eq 1 ] && echo -n '-f ' )--tail=$TAIL $DOCKER_OPTS '$n'$(err_filter)"
      done
      for e in "${voice_journald[@]}"; do
        l="${e%%|*}"; n="${e##*|}"
        echo "printf '%s\\n' '===== $VOICE_HOST / $l ====='"
        echo "journalctl --no-pager $( [ $FOLLOW -eq 1 ] && echo -n '-f ' )-n $TAIL $([ -n "$SINCE" ] && echo "--since '$SINCE'") -u '$n'$(err_filter)"
      done
    } | ssh "$VOICE_HOST" "pct exec $VOICE_LXC -- bash -s"
  fi

  # --- dev stack (LXC 140) ---
  if [ ${#dev_compose[@]} -gt 0 ]; then
    {
      echo "cd '$DEV_DIR'"
      local e l n
      for e in "${dev_compose[@]}"; do
        l="${e%%|*}"; n="${e##*|}"
        echo "printf '%s\\n' '===== dev / $l ====='"
        echo "docker compose logs $( [ $FOLLOW -eq 1 ] && echo -n '-f ' )--tail=$TAIL $DOCKER_OPTS '$n'$(err_filter)"
      done
    } | ssh "$DEV_HOST" "pct exec $DEV_CT -- bash -s"
  fi
}

# ---------------------------------------------------------------------------
# Follow: one parallel stream per service (keeps per-service provenance)
# ---------------------------------------------------------------------------
run_follow() {
  local g t l n
  while IFS='|' read -r g t l n; do
    [ -n "$g" ] || continue
    case "$g:$t" in
      memory:compose)
        ( printf '%s\n' "===== $PROD_HOST / $l ====="
          ssh "$PROD_HOST" "cd '$PROD_DIR' && docker compose logs -f --tail=$TAIL $DOCKER_OPTS '$n'" 2>&1 || true ) &
        ;;
      voice:compose)
        ( printf '%s\n' "===== $VOICE_HOST / $l ====="
          ssh "$VOICE_HOST" "pct exec $VOICE_LXC -- docker compose -f '$LIVEKIT_COMPOSE' logs -f --tail=$TAIL $DOCKER_OPTS '$n'" 2>&1 || true ) &
        ;;
      voice:journald)
        ( printf '%s\n' "===== $VOICE_HOST / $l ====="
          ssh "$VOICE_HOST" "pct exec $VOICE_LXC -- journalctl -f --no-pager -n $TAIL $([ -n "$SINCE" ] && echo "--since '$SINCE'") -u '$n'" 2>&1 || true ) &
        ;;
      dev:compose)
        ( printf '%s\n' "===== dev / $l ====="
          ssh "$DEV_HOST" "pct exec $DEV_CT -- bash -c \"cd $DEV_DIR && docker compose logs -f --tail=$TAIL $DOCKER_OPTS '$n'\"" 2>&1 || true ) &
        ;;
    esac
  done < <(selected_entries)
  wait
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
if [ -z "$(selected_entries)" ]; then
  echo "no services matched. try: tools/logs.sh --list" >&2
  exit 1
fi

if [ "$FOLLOW" -eq 1 ]; then
  trap 'kill 0' INT TERM
  run_follow
else
  run_batched
fi
