#!/usr/bin/env bash
# Build the Memory iOS app on the Mac build machine (mcj-mini).
#
# Workflow: rsync the iOS app sources to the Mac, then run xcodebuild there.
# Simulator build by default (no code signing). Device build needs a signed
# DEVELOPMENT_TEAM + provisioning profile.
#
# Monorepo layout: the app lives at apps/ios/ in this repo. On the Mac it is
# synced into the checkout's client/ios/ subtree — the only path this script
# owns, and the only path its --delete is allowed to touch. Everything else on
# the Mac (client/apps/openspec, client/macos, build/DerivedData, ...) is left
# untouched.
#
# Usage:
#   tools/ios-build-mac.sh              # simulator build (CODE_SIGNING_ALLOWED=NO)
#   tools/ios-build-mac.sh --device     # device build (requires signing setup)
#   tools/ios-build-mac.sh --test       # build + run unit tests on simulator
#   tools/ios-build-mac.sh --clean      # wipe DerivedData before building
#   tools/ios-build-mac.sh --no-sync    # skip rsync (build what's on the Mac now)
#   tools/ios-build-mac.sh --dry-run    # print the rsync plan only; no ssh/build
#
# Env overrides:
#   MEMORY_MAC_HOST        SSH host (default: mcj-mini; empty string = local dest)
#   MEMORY_MAC_PATH        project dir on Mac (default: ~/code/alftred, no spaces)
#   MEMORY_MAC_IOS_SUBDIR  iOS subtree under MEMORY_MAC_PATH (default: client/ios)
#   SCHEME                 Xcode scheme (default: VoiceAgent)
#   DESTINATION            xcodebuild -destination (default: generic/platform=iOS Simulator)
#   IOS_SIM_NAME           concrete simulator used by --test (default: iPhone 17 Pro)
#   XCODEBUILD_FLAGS       extra xcodebuild args
#   ALLOW_SYNC_INTO_GIT=1  permit syncing into a git-checkout root (dangerous)
#   ALLOW_DELETE_IN_GIT=1  permit --delete into a git work tree (dangerous)
#   VERBOSE=1              full xcodebuild output (default: -quiet, errors only)

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)" # monorepo root
IOS_SRC="$ROOT/apps/ios"

MAC_HOST="${MEMORY_MAC_HOST-mcj-mini}"
MAC_PATH="${MEMORY_MAC_PATH:-~/code/alftred}"
MAC_IOS_SUBDIR="${MEMORY_MAC_IOS_SUBDIR:-client/ios}"
SCHEME="${SCHEME:-VoiceAgent}"
DESTINATION="${DESTINATION:-generic/platform=iOS Simulator}"

SYNC=1
DEVICE=0
CLEAN=0
RUN_TESTS=0
DRY_RUN=0

usage() { sed -n '2,/^set -euo/p' "$0" | sed '$d'; }

die() {
  echo "error: $*" >&2
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --device) DEVICE=1; DESTINATION="generic/platform=iOS"; shift ;;
    --test) RUN_TESTS=1; shift ;;
    --clean) CLEAN=1; shift ;;
    --no-sync) SYNC=0; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage >&2; exit 2 ;;
  esac
done

# XCTest cannot run against a generic destination; --test needs a concrete
# simulator. Override with DESTINATION or IOS_SIM_NAME.
if [[ "$RUN_TESTS" == 1 && "$DESTINATION" == "generic/platform=iOS Simulator" ]]; then
  DESTINATION="platform=iOS Simulator,name=${IOS_SIM_NAME:-iPhone 17 Pro}"
fi

# ---- fail-fast guards ---------------------------------------------------
# A mis-set destination must error out, never delete anything.
[[ -d "$IOS_SRC" ]] \
  || die "iOS source not found: $IOS_SRC (expected apps/ios/ — repo layout drift?)"

# Matching a literal ~ / $HOME input (not an expanded path) is intentional.
# shellcheck disable=SC2088,SC2016
case "$MAC_PATH" in
  ''|'/'|'//'|'.'|'..'|'~'|'~/'|'$HOME'|'${HOME}')
    die "refusing unsafe MEMORY_MAC_PATH='$MAC_PATH'"
    ;;
esac
[[ "$MAC_PATH" == '~'* || "$MAC_PATH" == '/'* ]] \
  || die "MEMORY_MAC_PATH must be absolute or ~-rooted: '$MAC_PATH'"

[[ -n "$MAC_IOS_SUBDIR" ]] || die "MEMORY_MAC_IOS_SUBDIR must not be empty"
[[ "$MAC_IOS_SUBDIR" != /* && "$MAC_IOS_SUBDIR" != '~'* ]] \
  || die "MEMORY_MAC_IOS_SUBDIR must be relative (a subtree of MEMORY_MAC_PATH): '$MAC_IOS_SUBDIR'"
case "/$MAC_IOS_SUBDIR/" in
  *'/../'*|*'/./'*) die "MEMORY_MAC_IOS_SUBDIR must not contain '.' or '..': '$MAC_IOS_SUBDIR'" ;;
esac

MAC_IOS_PATH="$MAC_PATH/$MAC_IOS_SUBDIR"

# `test -e` on the destination, local or over ssh. Read-only probe.
path_exists() {
  if [[ -n "$MAC_HOST" ]]; then
    # shellcheck disable=SC2029 # $1 is an absolute/~/ remote path, must expand remotely
    ssh "$MAC_HOST" "test -e $1"
  else
    test -e "$1"
  fi
}

# ---- rsync local -> Mac -------------------------------------------------
if [[ "$SYNC" == 1 ]]; then
  # The Mac build checkout is rsync'd (it has no .git). A .git here means the
  # destination was mis-set to a real clone that --delete could damage.
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
  RSYNC_ARGS=(
    -az --delete
    --exclude '.DS_Store'
    --exclude 'xcuserdata/'
    --exclude '*.xcuserstate'
    --exclude 'build/'
    --exclude 'DerivedData/'
    --exclude '.build/'
    --exclude '.swiftpm/'
    --exclude '.git/'
  )
  if [[ "$DRY_RUN" == 1 ]]; then
    RSYNC_ARGS+=(--dry-run -i)
  fi
  rsync "${RSYNC_ARGS[@]}" "$IOS_SRC/" "$RSYNC_DEST/"
fi

if [[ "$DRY_RUN" == 1 ]]; then
  echo "==> dry run: not building"
  exit 0
fi

# ---- build on Mac -------------------------------------------------------
BUILD_CMD="xcodebuild"
BUILD_CMD="$BUILD_CMD -project '$MAC_IOS_SUBDIR/VoiceAgent.xcodeproj'"
BUILD_CMD="$BUILD_CMD -scheme '$SCHEME'"
BUILD_CMD="$BUILD_CMD -destination '$DESTINATION'"
# relative after `cd $MAC_PATH` (avoids remote tilde-quoting issues)
BUILD_CMD="$BUILD_CMD -derivedDataPath 'build/DerivedData'"

if [[ "$DEVICE" == 0 ]]; then
  BUILD_CMD="$BUILD_CMD CODE_SIGNING_ALLOWED=NO"
else
  BUILD_CMD="$BUILD_CMD -allowProvisioningUpdates"
fi

if [[ "$RUN_TESTS" == 1 ]]; then
  BUILD_CMD="$BUILD_CMD test"
else
  BUILD_CMD="$BUILD_CMD build"
fi

if [[ -z "${VERBOSE:-}" ]]; then
  BUILD_CMD="$BUILD_CMD -quiet"
fi

if [[ -n "${XCODEBUILD_FLAGS:-}" ]]; then
  BUILD_CMD="$BUILD_CMD $XCODEBUILD_FLAGS"
fi

REMOTE_CMD="cd $MAC_PATH"
if [[ "$CLEAN" == 1 ]]; then
  REMOTE_CMD="$REMOTE_CMD && rm -rf 'build/DerivedData'"
fi
REMOTE_CMD="$REMOTE_CMD && $BUILD_CMD"

echo "==> ssh $MAC_HOST: $BUILD_CMD"
# shellcheck disable=SC2029 # $REMOTE_CMD is intentionally composed locally, then sent
ssh "$MAC_HOST" "$REMOTE_CMD"
