#!/usr/bin/env bash
# mac-doctor.sh — verify the remote Mac build machine is usable.
#
# Usage:
#   tools/mac-doctor.sh --check     # run all checks; exit non-zero if any fail
#
# Checks (all read-only):
#   1. MEMORY_MAC_HOST / MEMORY_MAC_PATH configured (env or apps/web-ui/.env)
#   2. SSH reachability of MEMORY_MAC_HOST
#   3. the remote checkout path exists
#   4. xcodebuild present + its version
#   5. at least one usable iOS Simulator destination (needs an installed runtime)
#   6. a code-signing team is configured (DEVELOPMENT_TEAM)
#   7. opencode.json's xcodebuild MCP host derives from MEMORY_MAC_HOST
#
# All target resolution is delegated to lib/mac-remote.sh — this script only
# reports. Env: the MEMORY_MAC_* variables (see tools/mac-remote.env.example),
# plus DEVELOPMENT_TEAM.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/mac-remote.sh
# shellcheck disable=SC1091
. "$HERE/lib/mac-remote.sh"

# Fail fast on an unreachable host instead of hanging on ssh. Consumed by
# mac_ssh (lib/mac-remote.sh).
export MAC_SSH_OPTS="-o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=5 -o ServerAliveCountMax=3"

FAIL=0
WARN=0
ok()   { printf '  [ ok ] %s\n' "$*"; }
warn() { printf '  [WARN] %s\n' "$*"; WARN=$((WARN + 1)); }
bad()  { printf '  [FAIL] %s\n' "$*"; FAIL=$((FAIL + 1)); }

usage() { sed -n '2,/^set -euo/p' "$0" | sed '$d'; }

case "${1:---check}" in
  --check) ;;
  -h|--help) usage; exit 0 ;;
  *) echo "unknown arg: $1" >&2; usage >&2; exit 2 ;;
esac

printf 'mac-doctor: Mac remote build target\n'

mac_load_env

# ---- 1. configuration -----------------------------------------------------
if [[ -z "${MEMORY_MAC_HOST+x}" ]]; then
  bad "MEMORY_MAC_HOST is not set"
  # shellcheck disable=SC2154 # webui_dir is set by lib/mac-remote.sh
  printf '       -> set it in %s/.env or export MEMORY_MAC_HOST=<ssh-host>\n' "$webui_dir"
  printf '          (see tools/mac-remote.env.example)\n'
elif [[ -z "$MEMORY_MAC_HOST" ]]; then
  warn "MEMORY_MAC_HOST is empty (local-destination mode); remote checks skipped"
else
  ok "MEMORY_MAC_HOST=$MEMORY_MAC_HOST"
fi

if [[ -z "${MEMORY_MAC_PATH:-}" ]]; then
  bad "MEMORY_MAC_PATH is not set"
  printf '       -> set the project dir on the Mac, e.g. ~/code/alftred (absolute or ~-rooted)\n'
else
  ok "MEMORY_MAC_PATH=$MEMORY_MAC_PATH"
fi

# ---- 7. opencode MCP host derives from the same config --------------------
opencode_json="$webui_dir/opencode.json"
if [[ -f "$opencode_json" ]] && grep -q 'xcodebuild' "$opencode_json"; then
  if grep -q '{env:MEMORY_MAC_HOST}' "$opencode_json"; then
    ok "opencode.json: xcodebuild MCP host derives from {env:MEMORY_MAC_HOST}"
  else
    warn "opencode.json: xcodebuild MCP host is not derived from MEMORY_MAC_HOST"
    printf '       -> use ["ssh", "{env:MEMORY_MAC_HOST}", ...] (see tools/README.md)\n'
  fi
fi

if (( FAIL > 0 )); then
  printf '\nmac-doctor: %d failure(s)\n' "$FAIL"
  exit 1
fi

# Resolve/validate the target the same way the build scripts do (dies fast on
# unsafe configuration).
mac_resolve_target client/ios MEMORY_MAC_IOS_SUBDIR

if [[ -z "$MAC_HOST" ]]; then
  printf '\nmac-doctor: local-destination mode; remote checks skipped (%d warning(s))\n' "$WARN"
  exit 0
fi

# ---- 2. ssh reachability --------------------------------------------------
if mac_ssh "true" 2>/dev/null; then
  ok "ssh: $MAC_HOST reachable"
else
  bad "ssh: cannot reach $MAC_HOST"
  printf '       -> check ~/.ssh/config and the key: ssh %s\n' "$MAC_HOST"
  printf '\nmac-doctor: %d failure(s)\n' "$FAIL"
  exit 1
fi

# ---- 3. remote checkout path ----------------------------------------------
if mac_path_exists "$MAC_CHECKOUT_PATH"; then
  ok "checkout: $MAC_CHECKOUT_PATH exists"
else
  bad "checkout: $MAC_CHECKOUT_PATH not found on $MAC_HOST"
  printf '       -> sync first (tools/ios-build-mac.sh) or fix MEMORY_MAC_PATH\n'
fi

# ---- 4. xcodebuild --------------------------------------------------------
xcode_version="$(mac_ssh 'xcodebuild -version 2>/dev/null | head -1' 2>/dev/null || true)"
if [[ -n "$xcode_version" ]]; then
  ok "xcode: $xcode_version"
else
  bad "xcodebuild not found or no developer dir selected on $MAC_HOST"
  printf '       -> install Xcode; select it: sudo xcode-select -s /Applications/Xcode.app\n'
fi

# ---- 5. iOS simulator destination ----------------------------------------
sim="$(mac_ssh "xcrun simctl list devices available 2>/dev/null | grep -Ei 'iPhone|iPad' | head -1" 2>/dev/null || true)"
if [[ -n "$sim" ]]; then
  ok "simulator: $(echo "$sim" | sed -E 's/^[[:space:]]+//')"
else
  bad "no available iOS Simulator destination on $MAC_HOST"
  printf '       -> Xcode > Settings > Components: install an iOS Simulator runtime\n'
fi

# ---- 6. signing team ------------------------------------------------------
team="${DEVELOPMENT_TEAM:-}"
xcconfig="$webui_dir/../ios/VoiceAgent/VoiceAgent.xcconfig"
if [[ -z "$team" && -f "$xcconfig" ]]; then
  team="$(grep -E '^[[:space:]]*DEVELOPMENT_TEAM' "$xcconfig" | head -1 | sed -E 's/.*=[[:space:]]*//')"
fi
if [[ -n "$team" ]]; then
  ok "signing: DEVELOPMENT_TEAM=$team"
else
  warn "signing: no DEVELOPMENT_TEAM set (simulator builds are fine; device/TestFlight need one)"
fi

printf '\nmac-doctor: %d failure(s), %d warning(s)\n' "$FAIL" "$WARN"
(( FAIL == 0 )) || exit 1
