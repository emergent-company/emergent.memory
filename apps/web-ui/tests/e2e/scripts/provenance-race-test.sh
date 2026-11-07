#!/bin/bash
# provenance-race-test.sh — hermetic regression case for #810's silent-attach risk.
#
# Pre-flight only proves the ports were free before the build. On a cold tree the
# build takes minutes; a foreign listener can take the gateway port in that
# window and answer /api/health while our gateway dies on EADDRINUSE. Curl alone
# cannot tell the two apart, so this test reproduces exactly that:
#
#   1. select a pair with ports.mjs (the same selector the configs use),
#   2. force a slow build with a `go` shim that drops a marker and sleeps,
#   3. after the pre-flight (marker seen), bind the gateway port with an HTTP
#      server that answers /api/health with a foreign build id,
#   4. assert run-e2e.sh exits non-zero and names the port, instead of
#      declaring the foreign server ready.
#
# Needs the same prerequisites as the suites (Go, node, curl, ss) and their
# generated assets; in CI those steps run before this one.
set -euo pipefail
cd "$(dirname "$0")/.."

work="$(mktemp -d)"
shim="$work/bin"
marker="$work/build-started"
ports="$work/ports.env"
log="$work/run.log"
foreign_log="$work/foreign.log"
mkdir -p "$shim"
real_go="$(command -v go)"

cat >"$shim/go" <<EOF
#!/bin/bash
: > "$marker"
sleep 2
exec "$real_go" "\$@"
EOF
chmod +x "$shim/go"

runner_pid=""
foreign_pid=""
cleanup() {
  kill "$runner_pid" "$foreign_pid" 2>/dev/null || true
  pkill -P "$runner_pid" 2>/dev/null || true
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

node ports.mjs --lane provenance-race --out "$ports" >/dev/null
port="$(sed -n 's/^MEMORY_PORT=//p' "$ports")"
if [ -z "$port" ]; then
  echo "FAIL: ports.mjs did not produce a MEMORY_PORT in $ports" >&2
  exit 1
fi

echo "selected gateway port $port; starting run-e2e.sh with a slowed build"
PATH="$shim:$PATH" E2E_PORTS_FILE="$ports" bash run-e2e.sh >"$log" 2>&1 &
runner_pid=$!

# Wait for the shimmed build to start: pre-flight has passed by then.
seen=0
for _ in $(seq 1 400); do
  if [ -f "$marker" ]; then seen=1; break; fi
  if ! kill -0 "$runner_pid" 2>/dev/null; then
    echo "FAIL: run-e2e.sh exited before the build started (see below)" >&2
    cat "$log" >&2
    exit 1
  fi
  sleep 0.05
done
if [ "$seen" -ne 1 ]; then
  echo "FAIL: the shimmed build never started" >&2
  exit 1
fi

# The foreign server the runner must refuse: it owns the port and answers
# health, but it is not the gateway run-e2e.sh started.
echo "occupying $port with a foreign /api/health responder"
node -e '
const http = require("node:http");
const port = Number(process.argv[1]);
const server = http.createServer((req, res) => {
  if (req.url === "/api/health") {
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ build: "FOREIGN" }));
    return;
  }
  res.writeHead(404);
  res.end();
});
server.listen(port, "127.0.0.1", () => console.log("foreign health on " + port));
' "$port" >"$foreign_log" 2>&1 &
foreign_pid=$!

# Wait until the foreign responder is actually up before letting the build finish.
up=0
for _ in $(seq 1 100); do
  if curl -sf "http://localhost:$port/api/health" >/dev/null 2>&1; then up=1; break; fi
  sleep 0.05
done
if [ "$up" -ne 1 ]; then
  echo "FAIL: foreign responder never came up on $port" >&2
  cat "$foreign_log" >&2
  exit 1
fi

# The runner must exit on its own, non-zero, and name the port.
set +e
for _ in $(seq 1 240); do
  if ! kill -0 "$runner_pid" 2>/dev/null; then break; fi
  sleep 0.25
done
if kill -0 "$runner_pid" 2>/dev/null; then
  echo "FAIL: run-e2e.sh is still running after 60s — it accepted the foreign server" >&2
  kill "$runner_pid" 2>/dev/null || true
  wait "$runner_pid" 2>/dev/null
  cat "$log" >&2
  exit 1
fi
wait "$runner_pid"
status=$?
set -e

if [ "$status" -eq 0 ]; then
  echo "FAIL: run-e2e.sh exited 0 after the gateway port was stolen" >&2
  cat "$log" >&2
  exit 1
fi
if ! grep -q "port $port" "$log"; then
  echo "FAIL: run-e2e.sh exited $status but did not name port $port" >&2
  cat "$log" >&2
  exit 1
fi

matched="$(grep -m1 "run-e2e.sh:.*port $port" "$log" || true)"
echo "PASS: run-e2e.sh exited $status and named port $port instead of using the foreign server"
echo "      $matched"
