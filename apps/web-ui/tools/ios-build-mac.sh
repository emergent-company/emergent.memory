#!/usr/bin/env bash
# Build the Memory iOS app on the Mac build machine (mcj-mini).
#
# Workflow: rsync local repo changes to the Mac, then run xcodebuild there.
# Simulator build by default (no code signing). Device build needs a signed
# DEVELOPMENT_TEAM + provisioning profile.
#
# Usage:
#   tools/ios-build-mac.sh              # simulator build (CODE_SIGNING_ALLOWED=NO)
#   tools/ios-build-mac.sh --device     # device build (requires signing setup)
#   tools/ios-build-mac.sh --test       # build + run unit tests on simulator
#   tools/ios-build-mac.sh --clean      # wipe DerivedData before building
#   tools/ios-build-mac.sh --no-sync    # skip rsync (build what's on the Mac now)
#
# Env overrides:
#   MEMORY_MAC_HOST   SSH host (default: mcj-mini)
#   MEMORY_MAC_PATH   project dir on Mac (default: ~/code/alftred, no spaces)
#   SCHEME            Xcode scheme (default: VoiceAgent)
#   DESTINATION       xcodebuild -destination (default: generic/platform=iOS Simulator)
#   IOS_SIM_NAME      concrete simulator used by --test (default: iPhone 17 Pro)
#   XCODEBUILD_FLAGS  extra xcodebuild args
#   VERBOSE=1         full xcodebuild output (default: -quiet, errors only)

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"

MAC_HOST="${MEMORY_MAC_HOST:-mcj-mini}"
MAC_PATH="${MEMORY_MAC_PATH:-~/code/alftred}"
SCHEME="${SCHEME:-VoiceAgent}"
DESTINATION="${DESTINATION:-generic/platform=iOS Simulator}"

SYNC=1
DEVICE=0
CLEAN=0
RUN_TESTS=0

usage() { sed -n '2,23p' "$0"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --device)  DEVICE=1; DESTINATION="generic/platform=iOS"; shift ;;
    --test)    RUN_TESTS=1; shift ;;
    --clean)   CLEAN=1; shift ;;
    --no-sync) SYNC=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage >&2; exit 2 ;;
  esac
done

# XCTest cannot run against a generic destination; --test needs a concrete
# simulator. Override with DESTINATION or IOS_SIM_NAME.
if [[ "$RUN_TESTS" == 1 && "$DESTINATION" == "generic/platform=iOS Simulator" ]]; then
  DESTINATION="platform=iOS Simulator,name=${IOS_SIM_NAME:-iPhone 17 Pro}"
fi

# ---- rsync local -> Mac -------------------------------------------------
if [[ "$SYNC" == 1 ]]; then
  echo "==> rsync $REPO -> $MAC_HOST:$MAC_PATH"
  rsync -az --delete \
    --exclude '.git/' \
    --exclude '.venv/' \
    --exclude '.slim/' \
    --exclude '.env' \
    --exclude 'node_modules' \
    --exclude 'gateway/memory' \
    --exclude 'gateway/tmp/' \
    --exclude 'memory' \
    --exclude 'build/' \
    --exclude 'DerivedData/' \
    --exclude '.build/' \
    --exclude '.swiftpm/' \
    --exclude 'xcuserdata/' \
    --exclude '__pycache__/' \
    --exclude '*.pyc' \
    --exclude '.DS_Store' \
    "$REPO/" "$MAC_HOST:$MAC_PATH/"
fi

# ---- build on Mac -------------------------------------------------------
BUILD_CMD="xcodebuild"
BUILD_CMD="$BUILD_CMD -project 'client/ios/VoiceAgent.xcodeproj'"
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
ssh "$MAC_HOST" "$REMOTE_CMD"
