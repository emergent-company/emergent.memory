#!/usr/bin/env bash
# Build the MemoryConnector macOS menu-bar app on the Mac build machine.
#
# Workflow: rsync local repo changes to the Mac, regenerate the Xcode project
# with XcodeGen, then run xcodebuild there (ad-hoc during the build, then
# re-signed with the stable dev identity — see 'Stable signing' below).
#
# Usage:
#   tools/mac-build.sh                  # sync + generate + build
#   tools/mac-build.sh --gen            # regenerate project only (no build)
#   tools/mac-build.sh --test           # run hosted unit tests (test action)
#   tools/mac-build.sh --install        # build + copy .app to ~/Applications + open
#   tools/mac-build.sh --clean          # wipe build/DerivedData before building
#   tools/mac-build.sh --no-sync        # skip rsync (build what's on the Mac now)
#
# Env overrides:
#   MEMORY_MAC_HOST   SSH host (default: mcj-mini)
#   MEMORY_MAC_PATH   project dir on Mac (default: ~/code/alftred, no spaces)
#   VERBOSE=1         full xcodebuild output (default: -quiet, errors only)
#   XCODEBUILD_FLAGS  extra xcodebuild args
#   SIGN_IDENTITY     manual Apple-Development identity override (tools/mac-sign.sh)
#
# Stable signing: run tools/mac-setup-signing.sh ONCE on the Mac to create a
# dedicated codesigning keychain. mac-build.sh then signs every build with that
# identity automatically (works over ssh), so Keychain tokens and Automation
# (TCC) grants survive rebuilds. Without it, builds fall back to ad-hoc.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"

MAC_HOST="${MEMORY_MAC_HOST:-mcj-mini}"
MAC_PATH="${MEMORY_MAC_PATH:-~/code/alftred}"
SYNC=1
GEN_ONLY=0
RUN_TESTS=0
INSTALL=0
CLEAN=0

usage() { sed -n '2,27p' "$0"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --gen)      GEN_ONLY=1; shift ;;
    --test)     RUN_TESTS=1; shift ;;
    --install)  INSTALL=1; shift ;;
    --clean)    CLEAN=1; shift ;;
    --no-sync)  SYNC=0; shift ;;
    -h|--help)  usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage >&2; exit 2 ;;
  esac
done

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

# ---- regenerate the Xcode project ---------------------------------------
# xcodegen lives in Homebrew on the Mac; export PATH so it resolves over ssh
# (non-interactive shells may not source the brew shellenv snippet).
# NOTE: $MAC_PATH is left UNQUOTED so the remote shell expands the leading ~
# (a quoted "~/..." is treated as a literal path by zsh/bash). xcodegen runs
# inside a SUBSHELL so the working directory returns to $MAC_PATH — the
# xcodebuild step below resolves its project path relative to the repo root.
GEN_CMD="export PATH=\"\$PATH:/opt/homebrew/bin\" && (cd $MAC_PATH/client/macos/MemoryConnector && xcodegen generate --quiet)"

if [[ "$GEN_ONLY" == 1 ]]; then
  echo "==> ssh $MAC_HOST: xcodegen generate"
  ssh "$MAC_HOST" "$GEN_CMD"
  exit 0
fi

# ---- build on Mac -------------------------------------------------------
BUILD_CMD="xcodebuild"
BUILD_CMD="$BUILD_CMD -project 'client/macos/MemoryConnector/MemoryConnector.xcodeproj'"
BUILD_CMD="$BUILD_CMD -scheme 'MemoryConnector'"
BUILD_CMD="$BUILD_CMD -destination 'platform=macOS'"
# relative after `cd $MAC_PATH` (avoids remote tilde-quoting issues)
BUILD_CMD="$BUILD_CMD -derivedDataPath 'build/DerivedData'"
# Force AD-HOC signing for ssh builds: the login keychain's signing key is not
# reachable over ssh, so Xcode's automatic dev-cert signing fails with
# errSecInternalComponent (notably while re-signing embedded XCTest
# frameworks). Ad-hoc needs no keychain and lets tests run. Product builds are
# re-signed afterwards with the stable identity when a GUI session can do it.
BUILD_CMD="$BUILD_CMD CODE_SIGNING_ALLOWED=YES CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY=-"

if [[ "$RUN_TESTS" == 1 ]]; then
  # Ad-hoc signing of embedded XCTest frameworks fails with hardened runtime
  # ("invalid or unsupported format"); tests do not need it.
  BUILD_CMD="$BUILD_CMD ENABLE_HARDENED_RUNTIME=NO test"
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
REMOTE_CMD="$REMOTE_CMD && $GEN_CMD && $BUILD_CMD"

# ---- automatic stable signing (skip for test-only runs) -----------------
# Sign right after the build, on the Mac, before install. Chain:
#   1. dedicated self-signed identity from tools/mac-setup-signing.sh
#      (its own keychain; works non-interactively, including over ssh),
#   2. Apple Development (usually fails over ssh: errSecInternalComponent),
#   3. ad-hoc (always works; identity changes per rebuild -> Keychain/TCC may
#      re-prompt).
# A signing failure never fails the build.
if [[ "$RUN_TESTS" != 1 ]]; then
  SIGN_CMD="$(cat <<'REMOTE_SIGN'
APP='build/DerivedData/Build/Products/Debug/Memory.app'
SIGN_ENV="$HOME/.config/memory-connector/signing.env"
STABLE=0
if [ -f "$SIGN_ENV" ]; then
  . "$SIGN_ENV"
  PW_FILE="$HOME/.config/memory-connector/signing.keychain.pw"
  if [ -f "$PW_FILE" ]; then
    security unlock-keychain -p "$(cat "$PW_FILE")" "$MEMORY_SIGN_KEYCHAIN" >/dev/null 2>&1 || true
    # codesign resolves identities through the user keychain SEARCH LIST. A
    # dedicated keychain that is not on it yields "no identity found" even when
    # passed via --keychain, so ensure it is unlocked, usable by codesign, and
    # on the list (deterministic; keeps login + System keychains).
    security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$(cat "$PW_FILE")" "$MEMORY_SIGN_KEYCHAIN" >/dev/null 2>&1 || true
    security list-keychains -d user -s \
      "$HOME/Library/Keychains/login.keychain-db" \
      "$MEMORY_SIGN_KEYCHAIN" \
      /Library/Keychains/System.keychain >/dev/null 2>&1 || true
    SIGNED=0
    CS_ERR="/tmp/memory-macbuild-codesign.err"
    if [ -n "${MEMORY_SIGN_REQUIREMENT:-}" ]; then
      # codesign treats a requirement value without a leading '=' as a FILE
      # path; an inline requirement must be '='-prefixed. Normalize so envs
      # written by older setup scripts still work.
      REQ="$MEMORY_SIGN_REQUIREMENT"
      # NB: avoid a `case ... in =*)` pattern — zsh performs `=command`
      # expansion on it (`zsh: * not found`). Parameter expansion is portable.
      [ "${REQ#=}" = "$REQ" ] && REQ="=$REQ"
      codesign --force --options runtime --requirements="$REQ" --keychain "$MEMORY_SIGN_KEYCHAIN" --sign "$MEMORY_SIGN_IDENTITY" "$APP" 2>"$CS_ERR" && SIGNED=1
    else
      codesign --force --options runtime --keychain "$MEMORY_SIGN_KEYCHAIN" --sign "$MEMORY_SIGN_IDENTITY" "$APP" 2>"$CS_ERR" && SIGNED=1
    fi
    if [ "$SIGNED" -eq 1 ]; then
      STABLE=1
      echo "signed with dedicated identity: $MEMORY_SIGN_IDENTITY"
    else
      echo "note: dedicated-identity signing failed:"
      sed 's/^/    /' "$CS_ERR" | head -4
    fi
  fi
fi
if [ "$STABLE" -ne 1 ]; then
  echo "note: no dedicated signing identity — run tools/mac-setup-signing.sh once for a stable identity."
  if codesign --force --options runtime --sign 'Apple Development' "$APP" >/dev/null 2>&1; then
    echo "signed with Apple Development (over ssh this usually only works in a GUI session)."
  else
    echo "note: Apple Development signing unavailable (expected over ssh); leaving ad-hoc signed."
    codesign --force --sign - "$APP" >/dev/null 2>&1 || true
  fi
fi
codesign --display --verbose=2 "$APP" 2>&1 | grep -E 'Identifier=|Authority=|TeamIdentifier=' | head -5
REMOTE_SIGN
)"
  REMOTE_CMD="$REMOTE_CMD && $SIGN_CMD"
fi

echo "==> ssh $MAC_HOST: xcodegen + xcodebuild + codesign"
ssh "$MAC_HOST" "$REMOTE_CMD"

# ---- optional: install to ~/Applications and launch ---------------------
if [[ "$INSTALL" == 1 ]]; then
  APP_SRC="$MAC_PATH/build/DerivedData/Build/Products/Debug/Memory.app"
  echo "==> installing $APP_SRC -> ~/Applications"
  # unquoted $APP_SRC so the remote shell expands the leading ~
  INSTALL_CMD="pkill -f '/Applications/Memory.app' 2>/dev/null; pkill -f '/Applications/MemoryConnector.app' 2>/dev/null; sleep 1; rm -rf ~/Applications/Memory.app ~/Applications/MemoryConnector.app && ditto $APP_SRC ~/Applications/Memory.app && open ~/Applications/Memory.app"
  ssh "$MAC_HOST" "$INSTALL_CMD"
fi
