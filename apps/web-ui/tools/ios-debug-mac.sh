#!/usr/bin/env bash
# Debug the Memory iOS app at runtime on the Mac build machine.
#
# Workflow: rsync local repo changes to the Mac (run subcommand), boot the
# simulator, then drive the installed app via simctl/log/lldb over SSH.
# The app process name is "Memory" (PRODUCT_NAME); bundle id com.emergent.memory.
#
# Usage:
#   tools/ios-debug-mac.sh boot                       # boot the simulator
#   tools/ios-debug-mac.sh run [--no-sync] [--dry-run]  # sync, build, install, launch
#   tools/ios-debug-mac.sh --dry-run                  # print the rsync plan only
#   tools/ios-debug-mac.sh log [predicate]            # live os_log stream for process "Memory"
#   tools/ios-debug-mac.sh logs [predicate] [--last 10m]  # historical log show
#   tools/ios-debug-mac.sh screenshot [outfile]       # png; '-' or omitted streams raw to stdout
#   tools/ios-debug-mac.sh crash [--latest N]         # list recent .ips crash reports
#   tools/ios-debug-mac.sh crash --symbolicate <path> # symbolicate a .ips report
#   tools/ios-debug-mac.sh bt [name|pid]              # LLDB backtrace of running app
#   tools/ios-debug-mac.sh terminate                  # kill app on booted simulator
#   tools/ios-debug-mac.sh app                        # print resolved config + simulator state
#
# Target config (required, no repo-specific defaults — see lib/mac-remote.sh and
# tools/mac-remote.env.example). Set in the environment or in apps/web-ui/.env:
#   MEMORY_MAC_HOST        SSH host, e.g. mcj-mini (empty string = local dest)
#   MEMORY_MAC_PATH        project dir on Mac, e.g. ~/code/alftred (no spaces)
#   MEMORY_MAC_IOS_SUBDIR  iOS subtree under MEMORY_MAC_PATH (default: client/ios)
#
# Other env overrides:
#   SCHEME                 Xcode scheme (default: VoiceAgent)
#   VERBOSE=1              full xcodebuild output (default: -quiet, errors only)
#   BUNDLE_ID              app bundle id (default: com.emergent.memory)
#   UDID                   simulator device id (default: auto-detect)
#   APP_PATH               built .app path on Mac (default: build/DerivedData/Build/Products/Debug-iphonesimulator/Memory.app)
#   ALLOW_SYNC_INTO_GIT=1  permit syncing into a git-checkout root (dangerous)
#   ALLOW_DELETE_IN_GIT=1  permit --delete into a git work tree (dangerous)

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)" # monorepo root
MAC_SRC="$ROOT/apps/ios"

# shellcheck source=lib/mac-remote.sh
# shellcheck disable=SC1091
. "$HERE/lib/mac-remote.sh"

SCHEME="${SCHEME:-VoiceAgent}"
BUNDLE_ID="${BUNDLE_ID:-com.emergent.memory}"
APP_PATH="${APP_PATH:-build/DerivedData/Build/Products/Debug-iphonesimulator/Memory.app}"

# Resolve/validate the remote target up front (fail fast).
mac_resolve_target client/ios MEMORY_MAC_IOS_SUBDIR

# Auto-detect the simulator UDID unless the caller overrides it.
detect_udid() {
  mac_ssh "xcrun simctl list devices available | grep -i iphone | head -1 | sed -E 's/.*\(([0-9A-F-]{36})\).*/\1/'"
}

UDID="${UDID:-$(detect_udid)}"

usage() { sed -n '2,/^set -euo/p' "$0" | sed '$d'; }

MAC_DRY_RUN=0
cmd="${1:-}"
shift || true

# `--dry-run` at the top level previews the sync plan (same as `run --dry-run`).
if [[ "$cmd" == "--dry-run" ]]; then
  MAC_DRY_RUN=1
  cmd="run"
fi

case "$cmd" in

  boot)
    # raw: xcrun simctl list devices available | grep -i iphone | head -1
    #      xcrun simctl boot <udid> && xcrun simctl bootstatus <udid> -b
    echo "==> booting simulator UDID=$UDID"
    mac_ssh "xcrun simctl boot '$UDID' 2>/dev/null || true; xcrun simctl bootstatus '$UDID' -b"
    ;;

  run)
    # raw: rsync ... && xcodebuild -project 'client/ios/VoiceAgent.xcodeproj' -scheme 'VoiceAgent' \
    #        -destination 'generic/platform=iOS Simulator' -derivedDataPath 'build/DerivedData' \
    #        CODE_SIGNING_ALLOWED=NO build -quiet \
    #      && xcrun simctl install booted '<APP_PATH>' \
    #      && xcrun simctl launch --console-pty booted 'com.emergent.memory'
    SYNC=1
    if [[ "${1:-}" == "--no-sync" ]]; then SYNC=0; shift || true; fi
    if [[ "${1:-}" == "--dry-run" ]]; then MAC_DRY_RUN=1; shift || true; fi
    if [[ "$SYNC" == 1 ]]; then mac_rsync_guarded "$MAC_SRC"; fi
    if [[ "$MAC_DRY_RUN" == 1 ]]; then echo "==> dry run: not building"; exit 0; fi

    BUILD_CMD="xcodebuild -project '$MAC_SUBTREE/VoiceAgent.xcodeproj'"
    BUILD_CMD="$BUILD_CMD -scheme '$SCHEME'"
    BUILD_CMD="$BUILD_CMD -destination 'generic/platform=iOS Simulator'"
    BUILD_CMD="$BUILD_CMD -derivedDataPath 'build/DerivedData'"
    BUILD_CMD="$BUILD_CMD CODE_SIGNING_ALLOWED=NO build"
    if [[ -z "${VERBOSE:-}" ]]; then BUILD_CMD="$BUILD_CMD -quiet"; fi

    echo "==> ssh $MAC_HOST: $BUILD_CMD"
    mac_ssh "cd $MAC_CHECKOUT_PATH && $BUILD_CMD"
    echo "==> install + launch (Ctrl-C stops; stdout streams via --console-pty)"
    mac_ssh "cd $MAC_CHECKOUT_PATH && xcrun simctl install booted '$APP_PATH' && xcrun simctl launch --console-pty booted '$BUNDLE_ID'"
    ;;

  log)
    # raw: xcrun simctl spawn booted log stream --predicate 'process == "Memory"' --style compact
    PRED="${1:-process == \"Memory\"}"
    echo "==> log stream (predicate: $PRED) — Ctrl-C to stop"
    mac_ssh "xcrun simctl spawn booted log stream --info --predicate '$PRED' --style compact"
    ;;

  logs)
    # raw: xcrun simctl spawn booted log show --last 10m --predicate 'process == "Memory"'
    PRED='process == "Memory"'
    LAST="10m"
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --last) LAST="${2:?--last needs a value}"; shift 2 ;;
        *) PRED="$1"; shift ;;
      esac
    done
    echo "==> log show --last $LAST (predicate: $PRED)"
    mac_ssh "xcrun simctl spawn booted log show --info --last '$LAST' --predicate '$PRED'"
    ;;

  screenshot)
    # raw (remote file): xcrun simctl io booted screenshot /tmp/memory-<ts>.png
    # raw (stdout):      xcrun simctl io booted screenshot /tmp/memory-<ts>.png > file
    TS="$(date +%Y%m%d-%H%M%S)"
    REMOTE="/tmp/memory-$TS.png"
    OUT="${1:-}"
    if [[ -z "$OUT" || "$OUT" == "-" ]]; then
      # stream raw PNG bytes over ssh stdout; caller redirects: ... > shot.png
      mac_ssh "xcrun simctl io booted screenshot '$REMOTE' >/dev/null 2>&1; cat '$REMOTE'"
    else
      # capture on Mac, then scp back to the local path
      mac_ssh "xcrun simctl io booted screenshot '$REMOTE'"
      scp -q "$MAC_HOST:$REMOTE" "$OUT"
      mac_ssh "rm -f '$REMOTE'"
      echo "==> screenshot: $OUT"
    fi
    ;;

  crash)
    # raw: ls -t ~/Library/Logs/DiagnosticReports/*.ips | head -N
    #      ls -t ~/Library/Developer/CoreSimulator/Devices/<UDID>/data/Library/Logs/CrashReporter/*.ips | head -N
    #      xcrun symbolicatecrash <path> Memory.app.dSYM
    if [[ "${1:-}" == "--symbolicate" ]]; then
      CRASH_PATH="${2:?--symbolicate needs a path}"
      # dSYM lives next to the built app in DerivedData
      DSYM="$(dirname "$APP_PATH")/Memory.app.dSYM"
      echo "==> symbolicating $CRASH_PATH with $DSYM"
      mac_ssh "cd $MAC_CHECKOUT_PATH && if [ ! -d '$DSYM' ]; then echo 'WARNING: dSYM missing at $DSYM — rebuild with run (Debug build) to preserve symbols'; fi; xcrun symbolicatecrash '$CRASH_PATH' '$DSYM'"
    else
      N=5
      if [[ "${1:-}" == "--latest" ]]; then N="${2:?--latest needs a count}"; fi
      echo "==> recent crash reports (last $N)"
      mac_ssh "ls -t ~/Library/Logs/DiagnosticReports/*.ips 2>/dev/null | head -$N; echo '--- simulator CrashReporter ---'; ls -t ~/Library/Developer/CoreSimulator/Devices/'$UDID'/data/Library/Logs/CrashReporter/*.ips 2>/dev/null | head -$N"
    fi
    ;;

  bt)
    # raw (app not running): xcrun simctl launch --wait-for-debugger booted 'com.emergent.memory' & sleep 2
    #      lldb --batch -o 'process attach --pid $(pgrep -f Memory)' -o 'bt all' -o 'detach' -o 'quit'
    # raw (app running):     lldb --batch -o 'process attach --pid $(pgrep -f Memory)' -o 'bt all' -o 'detach' -o 'quit'
    # Note: $(pgrep -f ...) must expand REMOTELY, hence escaped inside single quotes.
    TARGET="${1:-Memory}"
    mac_ssh "if ! pgrep -f '$TARGET' >/dev/null 2>&1; then xcrun simctl launch --wait-for-debugger booted '$BUNDLE_ID' & sleep 2; fi; PID=\$(pgrep -f $TARGET); lldb --batch -o \"process attach --pid \$PID\" -o 'bt all' -o 'detach' -o 'quit'"
    ;;

  terminate)
    # raw: xcrun simctl terminate booted com.emergent.memory
    echo "==> terminate $BUNDLE_ID"
    mac_ssh "xcrun simctl terminate booted '$BUNDLE_ID'"
    ;;

  app)
    # print resolved debug config for sanity-checking
    echo "BUNDLE_ID=$BUNDLE_ID"
    echo "UDID=$UDID"
    echo "APP_PATH=$APP_PATH"
    echo "MAC_HOST=$MAC_HOST"
    echo "MAC_PATH=$MAC_CHECKOUT_PATH"
    echo "MAC_SUBTREE=$MAC_SUBTREE"
    echo "SCHEME=$SCHEME"
    echo "--- simulator state ---"
    mac_ssh "xcrun simctl list devices | grep -i booted || echo '(none booted — run: tools/ios-debug-mac.sh boot)'"
    ;;

  -h|--help)
    usage
    exit 0
    ;;

  *)
    echo "unknown command: $cmd" >&2
    usage >&2
    exit 2
    ;;
esac
