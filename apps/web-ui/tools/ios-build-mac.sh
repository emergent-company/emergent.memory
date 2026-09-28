#!/usr/bin/env bash
# Build the Memory iOS app on the Mac build machine.
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
# Target config (required, no repo-specific defaults — see lib/mac-remote.sh and
# tools/mac-remote.env.example). Set in the environment or in apps/web-ui/.env:
#   MEMORY_MAC_HOST        SSH host, e.g. mcj-mini (empty string = local dest)
#   MEMORY_MAC_PATH        project dir on Mac, e.g. ~/code/alftred (no spaces)
#   MEMORY_MAC_IOS_SUBDIR  iOS subtree under MEMORY_MAC_PATH (default: client/ios)
#
# Other env overrides:
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
MAC_SRC="$ROOT/apps/ios"

# shellcheck source=lib/mac-remote.sh
# shellcheck disable=SC1091
. "$HERE/lib/mac-remote.sh"

SCHEME="${SCHEME:-VoiceAgent}"
DESTINATION="${DESTINATION:-generic/platform=iOS Simulator}"

SYNC=1
DEVICE=0
CLEAN=0
RUN_TESTS=0
MAC_DRY_RUN=0

usage() { sed -n '2,/^set -euo/p' "$0" | sed '$d'; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --device) DEVICE=1; DESTINATION="generic/platform=iOS"; shift ;;
    --test) RUN_TESTS=1; shift ;;
    --clean) CLEAN=1; shift ;;
    --no-sync) SYNC=0; shift ;;
    --dry-run) MAC_DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage >&2; exit 2 ;;
  esac
done

# XCTest cannot run against a generic destination; --test needs a concrete
# simulator. Override with DESTINATION or IOS_SIM_NAME.
if [[ "$RUN_TESTS" == 1 && "$DESTINATION" == "generic/platform=iOS Simulator" ]]; then
  DESTINATION="platform=iOS Simulator,name=${IOS_SIM_NAME:-iPhone 17 Pro}"
fi

# ---- resolve/validate the remote target (fail fast) ---------------------
mac_resolve_target client/ios MEMORY_MAC_IOS_SUBDIR

# ---- rsync local -> Mac (guarded: scoped --delete + destination guards) --
if [[ "$SYNC" == 1 ]]; then
  mac_rsync_guarded "$MAC_SRC"
fi

if [[ "$MAC_DRY_RUN" == 1 ]]; then
  echo "==> dry run: not building"
  exit 0
fi

# ---- build on Mac -------------------------------------------------------
BUILD_CMD="xcodebuild"
BUILD_CMD="$BUILD_CMD -project '$MAC_SUBTREE/VoiceAgent.xcodeproj'"
BUILD_CMD="$BUILD_CMD -scheme '$SCHEME'"
BUILD_CMD="$BUILD_CMD -destination '$DESTINATION'"
# relative after `cd $MAC_CHECKOUT_PATH` (avoids remote tilde-quoting issues)
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

REMOTE_CMD="cd $MAC_CHECKOUT_PATH"
if [[ "$CLEAN" == 1 ]]; then
  REMOTE_CMD="$REMOTE_CMD && rm -rf 'build/DerivedData'"
fi
REMOTE_CMD="$REMOTE_CMD && $BUILD_CMD"

echo "==> ssh $MAC_HOST: $BUILD_CMD"
mac_ssh "$REMOTE_CMD"
