#!/usr/bin/env bash
# Debug the Memory iOS app at runtime on the Mac build machine (mcj-mini).
#
# Workflow: rsync local repo changes to the Mac (run subcommand), boot the
# simulator, then drive the installed app via simctl/log/lldb over SSH.
# The app process name is "Memory" (PRODUCT_NAME); bundle id com.emergent.memory.
#
# Usage:
#   tools/ios-debug-mac.sh boot                       # boot the simulator
#   tools/ios-debug-mac.sh run [--no-sync]            # sync, build, install, launch (console-pty)
#   tools/ios-debug-mac.sh log [predicate]            # live os_log stream for process "Memory"
#   tools/ios-debug-mac.sh logs [predicate] [--last 10m]  # historical log show
#   tools/ios-debug-mac.sh screenshot [outfile]       # png; '-' or omitted streams raw to stdout
#   tools/ios-debug-mac.sh crash [--latest N]         # list recent .ips crash reports
#   tools/ios-debug-mac.sh crash --symbolicate <path> # symbolicate a .ips report
#   tools/ios-debug-mac.sh bt [name|pid]              # LLDB backtrace of running app
#   tools/ios-debug-mac.sh terminate                  # kill app on booted simulator
#   tools/ios-debug-mac.sh app                        # print resolved config + simulator state
#
# Env overrides:
#   MEMORY_MAC_HOST        SSH host (default: mcj-mini; empty string = local dest)
#   MEMORY_MAC_PATH        project dir on Mac (default: ~/code/alftred, no spaces)
#   MEMORY_MAC_IOS_SUBDIR  iOS subtree under MEMORY_MAC_PATH (default: client/ios)
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
IOS_SRC="$ROOT/apps/ios"

MAC_HOST="${MEMORY_MAC_HOST-mcj-mini}"
MAC_PATH="${MEMORY_MAC_PATH:-~/code/alftred}"
MAC_IOS_SUBDIR="${MEMORY_MAC_IOS_SUBDIR:-client/ios}"
MAC_IOS_PATH="$MAC_PATH/$MAC_IOS_SUBDIR"
SCHEME="${SCHEME:-VoiceAgent}"
BUNDLE_ID="${BUNDLE_ID:-com.emergent.memory}"
APP_PATH="${APP_PATH:-build/DerivedData/Build/Products/Debug-iphonesimulator/Memory.app}"

die() {
  echo "error: $*" >&2
  exit 1
}

# Auto-detect the simulator UDID unless the caller overrides it.
detect_udid() {
  ssh "$MAC_HOST" "xcrun simctl list devices available | grep -i iphone | head -1 | sed -E 's/.*\(([0-9A-F-]{36})\).*/\1/'"
}

UDID="${UDID:-$(detect_udid)}"

usage() { sed -n '2,/^set -euo/p' "$0" | sed '$d'; }

# `test -e` on the destination, local or over ssh. Read-only probe.
path_exists() {
  if [[ -n "$MAC_HOST" ]]; then
    # shellcheck disable=SC2029 # $1 is an absolute/~/ remote path, must expand remotely
    ssh "$MAC_HOST" "test -e $1"
  else
    test -e "$1"
  fi
}

# ---- shared rsync (same guards + scoped --delete as ios-build-mac.sh) -----
# The Mac build checkout is rsync'd (no .git); A .git at either the checkout or
# the synced subtree signals a mis-set destination that --delete could damage.
rsync_repo() {
  [[ -d "$IOS_SRC" ]] \
    || die "iOS source not found: $IOS_SRC (expected apps/ios/ — repo layout drift?)"

  # Matching a literal ~ / $HOME input (not an expanded path) is intentional.
# shellcheck disable=SC2088,SC2016
  case "$MAC_PATH" in
    ''|'/'|'//'|'.'|'..'|'~'|'~/'|'$HOME'|'${HOME}')
      die "refusing unsafe MEMORY_MAC_PATH='$MAC_PATH'" ;;
  esac
  [[ "$MAC_PATH" == '~'* || "$MAC_PATH" == '/'* ]] \
    || die "MEMORY_MAC_PATH must be absolute or ~-rooted: '$MAC_PATH'"

  [[ -n "$MAC_IOS_SUBDIR" ]] || die "MEMORY_MAC_IOS_SUBDIR must not be empty"
  [[ "$MAC_IOS_SUBDIR" != /* && "$MAC_IOS_SUBDIR" != '~'* ]] \
    || die "MEMORY_MAC_IOS_SUBDIR must be relative (a subtree of MEMORY_MAC_PATH): '$MAC_IOS_SUBDIR'"
  case "/$MAC_IOS_SUBDIR/" in
    *'/../'*|*'/./'*) die "MEMORY_MAC_IOS_SUBDIR must not contain '.' or '..': '$MAC_IOS_SUBDIR'" ;;
  esac

  if path_exists "$MAC_PATH/.git"; then
    [[ "${ALLOW_SYNC_INTO_GIT:-0}" == 1 ]] \
      || die "$MAC_PATH is a git checkout; set ALLOW_SYNC_INTO_GIT=1 to override"
  fi
  if path_exists "$MAC_IOS_PATH/.git"; then
    [[ "${ALLOW_DELETE_IN_GIT:-0}" == 1 ]] \
      || die "$MAC_IOS_PATH is a git work tree and --delete could remove tracked files; set ALLOW_DELETE_IN_GIT=1 to override"
  fi

  if [[ -n "$MAC_HOST" ]]; then
    RSYNC_DEST="$MAC_HOST:$MAC_IOS_PATH"
  else
    RSYNC_DEST="$MAC_IOS_PATH"
  fi

  echo "==> rsync $IOS_SRC/ -> $RSYNC_DEST/ (--delete scoped to '$MAC_IOS_SUBDIR/' only)"
  rsync -az --delete \
    --exclude '.DS_Store' \
    --exclude 'xcuserdata/' \
    --exclude '*.xcuserstate' \
    --exclude 'build/' \
    --exclude 'DerivedData/' \
    --exclude '.build/' \
    --exclude '.swiftpm/' \
    --exclude '.git/' \
    "$IOS_SRC/" "$RSYNC_DEST/"
}

cmd="$1"
shift || true

case "$cmd" in

  boot)
    # raw: xcrun simctl list devices available | grep -i iphone | head -1
    #      xcrun simctl boot <udid> && xcrun simctl bootstatus <udid> -b
    echo "==> booting simulator UDID=$UDID"
    ssh "$MAC_HOST" "xcrun simctl boot '$UDID' 2>/dev/null || true; xcrun simctl bootstatus '$UDID' -b"
    ;;

  run)
    # raw: rsync ... && xcodebuild -project 'client/ios/VoiceAgent.xcodeproj' -scheme 'VoiceAgent' \
    #        -destination 'generic/platform=iOS Simulator' -derivedDataPath 'build/DerivedData' \
    #        CODE_SIGNING_ALLOWED=NO build -quiet \
    #      && xcrun simctl install booted '<APP_PATH>' \
    #      && xcrun simctl launch --console-pty booted 'com.emergent.memory'
    SYNC=1
    if [[ "${1:-}" == "--no-sync" ]]; then SYNC=0; shift || true; fi
    if [[ "$SYNC" == 1 ]]; then rsync_repo; fi

    BUILD_CMD="xcodebuild -project 'client/ios/VoiceAgent.xcodeproj'"
    BUILD_CMD="$BUILD_CMD -scheme '$SCHEME'"
    BUILD_CMD="$BUILD_CMD -destination 'generic/platform=iOS Simulator'"
    BUILD_CMD="$BUILD_CMD -derivedDataPath 'build/DerivedData'"
    BUILD_CMD="$BUILD_CMD CODE_SIGNING_ALLOWED=NO build"
    if [[ -z "${VERBOSE:-}" ]]; then BUILD_CMD="$BUILD_CMD -quiet"; fi

    echo "==> ssh $MAC_HOST: $BUILD_CMD"
    ssh "$MAC_HOST" "cd $MAC_PATH && $BUILD_CMD"
    echo "==> install + launch (Ctrl-C stops; stdout streams via --console-pty)"
    ssh "$MAC_HOST" "cd $MAC_PATH && xcrun simctl install booted '$APP_PATH' && xcrun simctl launch --console-pty booted '$BUNDLE_ID'"
    ;;

  log)
    # raw: xcrun simctl spawn booted log stream --predicate 'process == "Memory"' --style compact
    PRED="${1:-process == \"Memory\"}"
    echo "==> log stream (predicate: $PRED) — Ctrl-C to stop"
    ssh "$MAC_HOST" "xcrun simctl spawn booted log stream --info --predicate '$PRED' --style compact"
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
    ssh "$MAC_HOST" "xcrun simctl spawn booted log show --info --last '$LAST' --predicate '$PRED'"
    ;;

  screenshot)
    # raw (remote file): xcrun simctl io booted screenshot /tmp/memory-<ts>.png
    # raw (stdout):      xcrun simctl io booted screenshot /tmp/memory-<ts>.png > file
    TS="$(date +%Y%m%d-%H%M%S)"
    REMOTE="/tmp/memory-$TS.png"
    OUT="${1:-}"
    if [[ -z "$OUT" || "$OUT" == "-" ]]; then
      # stream raw PNG bytes over ssh stdout; caller redirects: ... > shot.png
      ssh "$MAC_HOST" "xcrun simctl io booted screenshot '$REMOTE' >/dev/null 2>&1; cat '$REMOTE'"
    else
      # capture on Mac, then scp back to the local path
      ssh "$MAC_HOST" "xcrun simctl io booted screenshot '$REMOTE'"
      scp -q "$MAC_HOST:$REMOTE" "$OUT"
      ssh "$MAC_HOST" "rm -f '$REMOTE'"
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
      ssh "$MAC_HOST" "cd $MAC_PATH && if [ ! -d '$DSYM' ]; then echo 'WARNING: dSYM missing at $DSYM — rebuild with run (Debug build) to preserve symbols'; fi; xcrun symbolicatecrash '$CRASH_PATH' '$DSYM'"
    else
      N=5
      if [[ "${1:-}" == "--latest" ]]; then N="${2:?--latest needs a count}"; fi
      echo "==> recent crash reports (last $N)"
      ssh "$MAC_HOST" "ls -t ~/Library/Logs/DiagnosticReports/*.ips 2>/dev/null | head -$N; echo '--- simulator CrashReporter ---'; ls -t ~/Library/Developer/CoreSimulator/Devices/'$UDID'/data/Library/Logs/CrashReporter/*.ips 2>/dev/null | head -$N"
    fi
    ;;

  bt)
    # raw (app not running): xcrun simctl launch --wait-for-debugger booted 'com.emergent.memory' & sleep 2
    #      lldb --batch -o 'process attach --pid $(pgrep -f Memory)' -o 'bt all' -o 'detach' -o 'quit'
    # raw (app running):     lldb --batch -o 'process attach --pid $(pgrep -f Memory)' -o 'bt all' -o 'detach' -o 'quit'
    # Note: $(pgrep -f ...) must expand REMOTELY, hence escaped inside single quotes.
    TARGET="${1:-Memory}"
    ssh "$MAC_HOST" "if ! pgrep -f '$TARGET' >/dev/null 2>&1; then xcrun simctl launch --wait-for-debugger booted '$BUNDLE_ID' & sleep 2; fi; PID=\$(pgrep -f $TARGET); lldb --batch -o \"process attach --pid \$PID\" -o 'bt all' -o 'detach' -o 'quit'"
    ;;

  terminate)
    # raw: xcrun simctl terminate booted com.emergent.memory
    echo "==> terminate $BUNDLE_ID"
    ssh "$MAC_HOST" "xcrun simctl terminate booted '$BUNDLE_ID'"
    ;;

  app)
    # print resolved debug config for sanity-checking
    echo "BUNDLE_ID=$BUNDLE_ID"
    echo "UDID=$UDID"
    echo "APP_PATH=$APP_PATH"
    echo "MAC_HOST=$MAC_HOST"
    echo "MAC_PATH=$MAC_PATH"
    echo "SCHEME=$SCHEME"
    echo "--- simulator state ---"
    ssh "$MAC_HOST" "xcrun simctl list devices | grep -i booted || echo '(none booted — run: tools/ios-debug-mac.sh boot)'"
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
