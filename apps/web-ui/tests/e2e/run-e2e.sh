#!/bin/bash
# run-e2e.sh — start mock memory + gateway, block until killed.
# Playwright's webServer runs this and kills it when tests finish.
set -euo pipefail
script_dir="$(cd "$(dirname "$0")" && pwd)"
cd "$script_dir/../.."   # repo root

# Ports are derived per run, never pinned (#810). `ports.mjs` is the single
# source of truth: the Playwright config selects a free gateway/mock pair, writes
# it to a file and passes that path here as E2E_PORTS_FILE; this script boots
# from the same file, so the config's baseURL and the servers cannot disagree.
# There is deliberately no numeric default — a pinned constant is what let two
# concurrent jobs on one host bind the same ports.
ports_file="${E2E_PORTS_FILE:-}"
if [ -n "$ports_file" ]; then
  if [ ! -f "$ports_file" ]; then
    echo "run-e2e.sh: E2E_PORTS_FILE=$ports_file does not exist — the config already built its baseURL from a different selection; refusing to derive a second pair." >&2
    exit 1
  fi
  file_gateway="$(sed -n 's/^MEMORY_PORT=\([0-9][0-9]*\)$/\1/p' "$ports_file")"
  file_mock="$(sed -n 's/^MOCK_PORT=\([0-9][0-9]*\)$/\1/p' "$ports_file")"
  if [ -z "$file_gateway" ] || [ -z "$file_mock" ]; then
    echo "run-e2e.sh: $ports_file exists but has no MEMORY_PORT/MOCK_PORT pair — refusing to guess ports." >&2
    exit 1
  fi
  # Drift guard: an environment override that contradicts the reservation file
  # means two sources are live. Fail instead of booting where the config is not
  # looking.
  if [ -n "${MEMORY_PORT:-}" ] && [ "$MEMORY_PORT" != "$file_gateway" ]; then
    echo "run-e2e.sh: MEMORY_PORT=$MEMORY_PORT disagrees with $ports_file ($file_gateway)." >&2
    exit 1
  fi
  if [ -n "${MOCK_PORT:-}" ] && [ "$MOCK_PORT" != "$file_mock" ]; then
    echo "run-e2e.sh: MOCK_PORT=$MOCK_PORT disagrees with $ports_file ($file_mock)." >&2
    exit 1
  fi
  PORT="$file_gateway"
  MOCK_PORT="$file_mock"
elif [ -n "${MEMORY_PORT:-}" ] || [ -n "${MOCK_PORT:-}" ]; then
  # Explicit local override: both halves are required, because a lone one would
  # otherwise be silently replaced by a fresh selection while the caller believes
  # it pinned a port.
  if [ -z "${MEMORY_PORT:-}" ] || [ -z "${MOCK_PORT:-}" ]; then
    echo "run-e2e.sh: set both MEMORY_PORT and MOCK_PORT (or neither); got MEMORY_PORT=${MEMORY_PORT:-<unset>} MOCK_PORT=${MOCK_PORT:-<unset>}." >&2
    exit 1
  fi
  PORT="$MEMORY_PORT"
else
  # Direct invocation (no Playwright config): select a pair for this process and
  # keep it next to the runner's temp dir, like the configs do.
  ports_file="$(mktemp -d)/ports.env"
  eval "$(node "$script_dir/ports.mjs" --lane "${E2E_LANE:-local}" --out "$ports_file" --shell)"
  PORT="$MEMORY_PORT"
fi

# Pre-flight: this harness is deliberately self-contained. If either port is
# already served, the gateway or mock bind fails, and with a backgrounded job +
# `set -e` that failure is silent. This check only proves the ports were free
# *now*; the build below can take minutes, so the readiness loop proves
# provenance again before it accepts the stack.
port_in_use() {
  # true when something is already accepting TCP connections on 127.0.0.1:$1
  (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}
for _port in "$PORT" "$MOCK_PORT"; do
  if port_in_use "$_port"; then
    echo "run-e2e.sh: port $_port is already in use — refusing to reuse a foreign server. The port selector should have skipped it; if this repeats, remove ${ports_file:-the reservation file} and re-run." >&2
    exit 1
  fi
done
unset _port

# Build the gateway binary. On an unwarmed tree (e.g. a fresh worktree) the
# generated *_templ.go files, the gallery's derived graph
# (devgallery_*_gen.go) and the compiled Tailwind stylesheet
# (webui/static/css/app.css) are all gitignored build output and absent, and
# `go build` fails without them — webui.go's cssGuard turns a missing app.css
# into a compile error, and the graph files are compiled in. Generate whichever
# is missing, then build fresh so a stale binary is never served.
(cd gateway && {
  if ! compgen -G '*_templ.go' >/dev/null; then
    # Install the pinned CLI rather than `go run`-ing it. In module mode
    # (GOWORK=off, which CI sets for these lanes) `go run` resolves the CLI's
    # own dependencies through the caller's module graph, which does not carry
    # them; a versioned `go install` brings its own. The version comes from
    # gateway/go.mod, like the image and the `gateway` CI job (#2314).
    #
    # Honour an explicit GOBIN: on the CI pool `$(go env GOPATH)` is shared and
    # persistent across runner slots, so two jobs installing templ at once can
    # write the same binary while the other execs it ("Text file busy"). CI
    # points GOBIN at a per-job directory; a local run keeps the old location.
    templ_bin="${GOBIN:-$(go env GOPATH)/bin}/templ"
    go install "github.com/a-h/templ/cmd/templ@$(GOWORK=off go list -m -f '{{.Version}}' github.com/a-h/templ)"
    "$templ_bin" generate ./...
  fi
  # componentgraph derives the gallery graph from the *_templ.go call sites, so
  # this must follow templ generate above.
  if [ ! -f devgallery_graph_gen.go ] || [ ! -f devgallery_adoption_gen.go ]; then
    go generate ./...
  fi
  if [ ! -f webui/static/css/app.css ]; then
    command -v task >/dev/null || { echo "run-e2e.sh: task not found — add it to PATH (e.g. export PATH=\"/root/go/bin:\$PATH\") to compile app.css" >&2; exit 1; }
    task css
  fi
  go build -o memory .
})

# mock memory. MEMORY_PORT must reach the child too: the mock serves OAuth
# metadata and redirects on the gateway origin, so omitting it made it fall back
# to a stale pinned origin (#810).
MEMORY_PORT="$PORT" MOCK_PORT="$MOCK_PORT" node tests/e2e/mock-memory.mjs &
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
# Also clean up on INT/TERM: a cancelled or timed-out job (Playwright's webServer
# teardown escalates to SIGTERM, CI cancels with signals) must not leak the mock
# and gateway children still holding the port.
trap cleanup EXIT INT TERM

# `ss` proves the listening socket belongs to *our* gateway pid. A foreign
# server can answer /api/health while our gateway died on EADDRINUSE; curl
# alone cannot tell them apart. `ss` is part of iproute2 on the runner image.
port_owned_by() {
  local port="$1" pid="$2"
  if ! command -v ss >/dev/null 2>&1; then
    echo "run-e2e.sh: ss is required to prove the gateway on port $port is the one this script started (install iproute2)." >&2
    exit 1
  fi
  ss -ltnp "sport = :$port" 2>/dev/null | grep -q "pid=$pid[,)]"
}

# Wait for readiness — and for *our* process, not a sibling's. The pre-flight
# guard can be stale by the time the build finishes, so a foreign listener may
# already answer the health URL. Accept only when our gateway owns the port,
# then settle and re-check both PIDs so a process that died right after binding
# is not masked by a successful curl.
ready=0
foreign=0
for _ in $(seq 1 60); do
  if ! kill -0 "$MOCK" 2>/dev/null; then
    echo "run-e2e.sh: mock-memory.mjs exited before the gateway was ready (port $MOCK_PORT)." >&2
    exit 1
  fi
  if ! kill -0 "$GW" 2>/dev/null; then
    echo "run-e2e.sh: gateway exited before becoming ready on port $PORT (bind failure or startup crash)." >&2
    exit 1
  fi
  if curl -sf "http://localhost:$PORT/api/health" >/dev/null 2>&1; then
    if port_owned_by "$PORT" "$GW"; then
      sleep 0.5
      if ! kill -0 "$GW" 2>/dev/null; then
        echo "run-e2e.sh: gateway exited right after answering health on port $PORT (bind race)." >&2
        exit 1
      fi
      if ! kill -0 "$MOCK" 2>/dev/null; then
        echo "run-e2e.sh: mock-memory.mjs exited right after the gateway answered health (port $MOCK_PORT)." >&2
        exit 1
      fi
      ready=1
      break
    fi
    # Something answered health, but it is not the gateway we started. Keep
    # waiting; if our gateway has died the kill -0 above names the port next pass.
    foreign=1
  fi
  sleep 0.5
done
if [ "$ready" -ne 1 ]; then
  if [ "$foreign" -eq 1 ]; then
    echo "run-e2e.sh: port $PORT answered /api/health but not from the gateway this script started (pid $GW) — refusing to test a foreign server." >&2
  else
    echo "run-e2e.sh: gateway did not become ready on port $PORT within 30s." >&2
  fi
  exit 1
fi

# block until killed
sleep infinity
