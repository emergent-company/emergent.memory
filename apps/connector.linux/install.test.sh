#!/usr/bin/env bash
# Hermetic test for install.sh's dev-channel tag resolution (#956).
#
# The dev channel is published under `connector-dev`, namespaced by app like
# `cli-dev` and `mac-dev`. It must never resolve from a tag named like a branch:
# a `dev` tag makes `git fetch origin dev` resolve the tag instead of the branch,
# which is the bug #956 reports. The legacy `dev` tag remains a fallback for
# builds published before the migration, but only after `connector-dev` is
# absent.
#
# curl and tar are stubbed, so this runs offline and installs nothing real.
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
installer="$here/install.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

failures=0
ok() { printf 'ok   %s\n' "$1"; }
no() {
  printf 'FAIL %s\n' "$1"
  failures=$((failures + 1))
}

platform_os=$(uname -s)
case "$platform_os" in
  Linux*) os=linux ;;
  Darwin*) os=darwin ;;
  *) echo "unsupported test platform: $platform_os"; exit 1 ;;
esac
platform_arch=$(uname -m)
case "$platform_arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported test arch: $platform_arch"; exit 1 ;;
esac

mkdir -p "$tmp/bin" "$tmp/releases"
asset="memory-connector_0.0.0-dev.test_${os}_${arch}.tar.gz"
printf '{"assets":[{"name":"%s"}]}\n' "$asset" >"$tmp/releases/payload.json"

# Stub curl: log every URL, serve the payload only for the tags listed in
# FAKE_TAGS, and let downloads "succeed" so main reaches the tar step.
cat >"$tmp/bin/curl" <<'EOF'
#!/usr/bin/env bash
url=""
for arg in "$@"; do
  case "$arg" in http://*|https://*) url="$arg" ;; esac
done
printf '%s\n' "$url" >>"$CURL_LOG"
case "$url" in
  */releases/tags/connector-dev)
    [[ " $FAKE_TAGS " == *" connector-dev "* ]] && { cat "$FAKE_PAYLOAD"; exit 0; }
    exit 22 ;;
  */releases/tags/dev)
    [[ " $FAKE_TAGS " == *" dev "* ]] && { cat "$FAKE_PAYLOAD"; exit 0; }
    exit 22 ;;
  */releases/download/*) exit 0 ;;
  *.sha256) exit 22 ;;
  *) exit 22 ;;
esac
EOF

# Stub tar: create the binary the installer expects to find in the archive.
cat >"$tmp/bin/tar" <<'EOF'
#!/usr/bin/env bash
dir=""
while [ $# -gt 0 ]; do
  case "$1" in
    -C) dir="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$dir" ] || { echo "stub tar: no -C" >&2; exit 1; }
printf '#!/bin/sh\necho stub\n' >"$dir/memory-connector"
chmod +x "$dir/memory-connector"
EOF
chmod +x "$tmp/bin/curl" "$tmp/bin/tar"

run_install() {
  CURL_LOG="$tmp/curl.log"
  : >"$CURL_LOG"
  out=$(
    PATH="$tmp/bin:$PATH" \
    CURL_LOG="$CURL_LOG" \
    FAKE_PAYLOAD="$tmp/releases/payload.json" \
    FAKE_TAGS="$1" \
    bash "$installer" --channel dev --dir "$tmp/install-$2" 2>&1
  )
  status=$?
}

# 1. Both tags exist: prefer connector-dev and never touch the branch-named tag.
run_install "connector-dev dev" preferred
if ((status == 0)); then ok "preferred tag installs"; else no "preferred tag installs (exit $status)"; fi
if grep -q '/releases/tags/connector-dev$' "$tmp/curl.log"; then
  ok "preferred tag is connector-dev"
else
  no "preferred tag is connector-dev"
fi
if grep -q '/releases/tags/dev$' "$tmp/curl.log"; then
  no "branch-named dev tag is not queried when connector-dev exists"
else
  ok "branch-named dev tag is not queried when connector-dev exists"
fi
if grep -q "/releases/download/connector-dev/${asset}" "$tmp/curl.log"; then
  ok "download uses the connector-dev release"
else
  no "download uses the connector-dev release"
fi

# 2. Only the legacy tag exists: fall back so pre-migration dev builds still install.
run_install "dev" fallback
if ((status == 0)); then ok "legacy tag fallback installs"; else no "legacy tag fallback installs (exit $status)"; fi
if grep -q '/releases/tags/connector-dev$' "$tmp/curl.log" && grep -q '/releases/tags/dev$' "$tmp/curl.log"; then
  ok "legacy fallback is tried after connector-dev is absent"
else
  no "legacy fallback is tried after connector-dev is absent"
fi
if grep -q "/releases/download/dev/${asset}" "$tmp/curl.log"; then
  ok "legacy download uses the dev release"
else
  no "legacy download uses the dev release"
fi

# 3. No dev release at all: fail with a message naming both candidate tags.
run_install "none" missing
if ((status != 0)) && [[ $out == *"connector-dev"*"dev"* || $out == *"dev"*"connector-dev"* ]]; then
  ok "missing dev channel fails with both tag names"
else
  no "missing dev channel fails with both tag names (exit $status)"
  printf '%s\n' "$out" | sed 's/^/    /'
fi

if ((failures)); then
  printf 'install tests: %d failure(s)\n' "$failures"
  exit 1
fi
printf 'install tests: OK\n'
