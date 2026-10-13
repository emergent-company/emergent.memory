#!/usr/bin/env bash
# Build the MemoryConnector macOS menu-bar app on the Mac build machine.
#
# Workflow: rsync local repo changes to the Mac, regenerate the Xcode project
# with XcodeGen, then run xcodebuild there (ad-hoc during the build, then
# re-signed with the stable dev identity — see 'Stable signing' below).
#
# Source: apps/connector.mac/ in this repo; synced into the Mac checkout's
# client/macos/ subtree. The embedded engine's Go sources (apps/connector.linux
# and its sibling module apps/server/pkg/sdk) are synced too, and a minimal
# go.work maps them — MemoryConnector/Scripts/build-engine.sh compiles the
# engine from the checkout, so without them it builds against a stale local
# module set or the module cache and the app ships an outdated engine.
# Everything else on the Mac (client/ios, build/DerivedData, ...) is untouched.
#
# Usage:
#   apps/connector.mac/Scripts/mac-build.sh                  # sync + generate + build
#   apps/connector.mac/Scripts/mac-build.sh --gen            # regenerate project only (no build)
#   apps/connector.mac/Scripts/mac-build.sh --test           # run hosted unit tests (test action)
#   apps/connector.mac/Scripts/mac-build.sh --uitest         # build UI-test bundle only (run it in a GUI session)
#   apps/connector.mac/Scripts/mac-build.sh --install        # build + copy .app to ~/Applications + open
#   apps/connector.mac/Scripts/mac-build.sh --clean          # wipe build/DerivedData before building
#   apps/connector.mac/Scripts/mac-build.sh --no-sync        # skip rsync (build what's on the Mac now)
#   apps/connector.mac/Scripts/mac-build.sh --dry-run        # print the rsync plan only; no ssh/build
#
# Target config (required, no repo-specific defaults — see lib/mac-remote.sh and
# apps/connector.mac/.env.example). Set in the environment or in apps/connector.mac/.env:
#   MEMORY_MAC_HOST     SSH host, e.g. mcj-mini (empty string = local dest)
#   MEMORY_MAC_PATH     project dir on Mac, e.g. ~/code/emergent.memory-macbuild (no spaces)
#   MEMORY_MAC_SUBTREE  Mac connector subtree (default: client/macos)
#
# Other env overrides:
#   VERBOSE=1         full xcodebuild output (default: -quiet, errors only)
#   XCODEBUILD_FLAGS  extra xcodebuild args
#   SIGN_IDENTITY     manual Apple-Development identity override (apps/connector.mac/Scripts/mac-sign.sh)
#
# Stable signing: run apps/connector.mac/Scripts/mac-setup-signing.sh ONCE on the Mac to create a
# dedicated codesigning keychain. mac-build.sh then signs every build with that
# identity automatically (works over ssh), so Keychain tokens and Automation
# (TCC) grants survive rebuilds. Without it, builds fall back to ad-hoc.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)" # monorepo root
MAC_SRC="$ROOT/apps/connector.mac"

# shellcheck source=lib/mac-remote.sh
# shellcheck disable=SC1091
. "$HERE/lib/mac-remote.sh"

SYNC=1
GEN_ONLY=0
RUN_TESTS=0
RUN_UITESTS=0
INSTALL=0
CLEAN=0
MAC_DRY_RUN=0

usage() { sed -n '2,/^set -euo/p' "$0" | sed '$d'; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --gen)      GEN_ONLY=1; shift ;;
    --test)     RUN_TESTS=1; shift ;;
    --uitest)   RUN_UITESTS=1; shift ;;
    --install)  INSTALL=1; shift ;;
    --clean)    CLEAN=1; shift ;;
    --no-sync)  SYNC=0; shift ;;
    --dry-run)  MAC_DRY_RUN=1; shift ;;
    -h|--help)  usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage >&2; exit 2 ;;
  esac
done

# ---- resolve/validate the remote target (fail fast) ---------------------
mac_resolve_target client/macos

# ---- rsync local -> Mac (guarded: scoped --delete + destination guards) --
if [[ "$SYNC" == 1 ]]; then
  mac_rsync_guarded "$MAC_SRC"
  # The engine is built from the checkout, not from the app subtree above, so
  # mirror its Go inputs as well. Only client/macos is owned by this script's
  # app mirror; these two are the additional paths build-engine.sh reads.
  mac_rsync_guarded "$ROOT/apps/connector.linux" "apps/connector.linux"
  mac_rsync_guarded "$ROOT/apps/server/pkg/sdk" "apps/server/pkg/sdk"
fi

if [[ "$MAC_DRY_RUN" == 1 ]]; then
  echo "==> dry run: not generating/building"
  exit 0
fi

# ---- regenerate the Xcode project ---------------------------------------
# xcodegen lives in Homebrew on the Mac; export PATH so it resolves over ssh
# (non-interactive shells may not source the brew shellenv snippet).
# NOTE: $MAC_CHECKOUT_PATH is left UNQUOTED so the remote shell expands the
# leading ~ (a quoted "~/..." is treated as a literal path by zsh/bash).
# xcodegen runs inside a SUBSHELL so the working directory returns to the
# checkout root — the xcodebuild step below resolves its project path relative
# to that root.
GEN_CMD="export PATH=\"\$PATH:/opt/homebrew/bin\" && (cd $MAC_CHECKOUT_PATH/$MAC_SUBTREE/MemoryConnector && xcodegen generate --quiet)"

# Go workspace for the embedded engine. build-engine.sh runs `go build` from
# apps/connector.linux; that module imports the sibling module
# apps/server/pkg/sdk, which must resolve from the working tree we just synced
# rather than the published module cache. The checkout is a partial mirror, so
# regenerate a minimal go.work mapping exactly those two modules (idempotent;
# `go work init` records the toolchain's Go version). Runs in the checkout root,
# where REMOTE_CMD has already `cd`-ed.
GOWORK_CMD="export PATH=\"\$PATH:/opt/homebrew/bin\" && rm -f go.work && go work init ./apps/connector.linux ./apps/server/pkg/sdk"

if [[ "$GEN_ONLY" == 1 ]]; then
  echo "==> ssh $MAC_HOST: xcodegen generate"
  mac_ssh "$GEN_CMD"
  exit 0
fi

# ---- build on Mac -------------------------------------------------------
BUILD_CMD="xcodebuild"
BUILD_CMD="$BUILD_CMD -project '$MAC_SUBTREE/MemoryConnector/MemoryConnector.xcodeproj'"
BUILD_CMD="$BUILD_CMD -scheme 'MemoryConnector'"
BUILD_CMD="$BUILD_CMD -destination 'platform=macOS'"
# relative after `cd $MAC_CHECKOUT_PATH` (avoids remote tilde-quoting issues)
BUILD_CMD="$BUILD_CMD -derivedDataPath 'build/DerivedData'"
# Force AD-HOC signing for ssh builds: the login keychain's signing key is not
# reachable over ssh, so Xcode's automatic dev-cert signing fails with
# errSecInternalComponent (notably while re-signing embedded XCTest
# frameworks). Ad-hoc needs no keychain and lets tests run. Product builds are
# re-signed afterwards with the stable identity when a GUI session can do it.
BUILD_CMD="$BUILD_CMD CODE_SIGNING_ALLOWED=YES CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY=-"

if [[ "$RUN_UITESTS" == 1 ]]; then
  # XCUITest cannot run over ssh (no WindowServer/launchd session); the `test`
  # action would hang or fail. Build the app + UI-test bundle only
  # (`build-for-testing`), then the post-build note prints the GUI-session
  # command to actually run it on the Mac.
  BUILD_CMD="$BUILD_CMD ENABLE_HARDENED_RUNTIME=NO build-for-testing"
  BUILD_CMD="$BUILD_CMD -only-testing:MemoryConnectorUITests"
elif [[ "$RUN_TESTS" == 1 ]]; then
  # Ad-hoc signing of embedded XCTest frameworks fails with hardened runtime
  # ("invalid or unsupported format"); tests do not need it.
  # Run ONLY the hosted unit tests: MemoryConnectorUITests is in the scheme's
  # test action, and XCUITest cannot run over ssh (no WindowServer/launchd
  # session) — running the whole scheme here would hang/fail. The UI suite is
  # built+run in a GUI session instead (see --uitest).
  BUILD_CMD="$BUILD_CMD ENABLE_HARDENED_RUNTIME=NO test -only-testing:MemoryConnectorTests"
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
REMOTE_CMD="$REMOTE_CMD && $GEN_CMD && $GOWORK_CMD && $BUILD_CMD"

# ---- automatic stable signing (skip for test-only runs) -----------------
# Sign right after the build, on the Mac, before install. Chain:
#   1. dedicated self-signed identity from apps/connector.mac/Scripts/mac-setup-signing.sh
#      (its own keychain; works non-interactively, including over ssh),
#   2. Apple Development (usually fails over ssh: errSecInternalComponent),
#   3. ad-hoc (always works; identity changes per rebuild -> Keychain/TCC may
#      re-prompt).
# A signing failure never fails the build.
if [[ "$RUN_TESTS" != 1 && "$RUN_UITESTS" != 1 ]]; then
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
    # NOTE: deliberately NO `--options runtime`. The dedicated identity is
    # self-signed and carries no Team ID, so hardened-runtime library validation
    # refuses to load the app's nested code (Sparkle.framework, the Xcode debug
    # dylib) and the app aborts at launch with "mapping process and mapped file
    # (non-platform) have different Team IDs". Re-signing without the hardened
    # runtime is what keeps the installed dev build launchable; an app that must
    # stay hardened would instead carry the
    # com.apple.security.cs.disable-library-validation entitlement.
    if [ -n "${MEMORY_SIGN_REQUIREMENT:-}" ]; then
      # codesign treats a requirement value without a leading '=' as a FILE
      # path; an inline requirement must be '='-prefixed. Normalize so envs
      # written by older setup scripts still work.
      REQ="$MEMORY_SIGN_REQUIREMENT"
      # NB: avoid a `case ... in =*)` pattern — zsh performs `=command`
      # expansion on it (`zsh: * not found`). Parameter expansion is portable.
      [ "${REQ#=}" = "$REQ" ] && REQ="=$REQ"
      codesign --force --requirements="$REQ" --keychain "$MEMORY_SIGN_KEYCHAIN" --sign "$MEMORY_SIGN_IDENTITY" "$APP" 2>"$CS_ERR" && SIGNED=1
    else
      codesign --force --keychain "$MEMORY_SIGN_KEYCHAIN" --sign "$MEMORY_SIGN_IDENTITY" "$APP" 2>"$CS_ERR" && SIGNED=1
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
  echo "note: no dedicated signing identity — run apps/connector.mac/Scripts/mac-setup-signing.sh once for a stable identity."
  if codesign --force --sign 'Apple Development' "$APP" >/dev/null 2>&1; then
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
mac_ssh "$REMOTE_CMD"

# ---- UI-test runner contract ---------------------------------------------
# XCUITest needs a GUI/WindowServer session, so it cannot run over the headless
# ssh path above — `--uitest` only builds the app + UI-test bundle. Print the
# exact command to run the suite ON THE MAC in a GUI session, plus the stable
# signing requirement (ad-hoc signing resets Automation/TCC grants).
if [[ "$RUN_UITESTS" == 1 ]]; then
  echo "==> UI-test bundle built (NOT run): XCUITest requires a GUI/WindowServer session."
  echo "    Run it on the Mac in a GUI session (e.g. over Screen Sharing / locally):"
  echo "      cd $MAC_CHECKOUT_PATH/$MAC_SUBTREE/MemoryConnector"
  echo "      xcodebuild test -project MemoryConnector.xcodeproj -scheme MemoryConnector \\"
  echo "        -destination 'platform=macOS' -derivedDataPath build/DerivedData \\"
  echo "        -only-testing:MemoryConnectorUITests"
  echo "    Sign with a stable identity first (apps/connector.mac/Scripts/mac-setup-signing.sh,"
  echo "    run once); ad-hoc signing resets Automation (TCC) grants and will fail the suite."
fi

# ---- optional: install to ~/Applications and launch ---------------------
if [[ "$INSTALL" == 1 ]]; then
  APP_SRC="$MAC_CHECKOUT_PATH/build/DerivedData/Build/Products/Debug/Memory.app"
  echo "==> installing $APP_SRC -> ~/Applications"
  # unquoted $APP_SRC so the remote shell expands the leading ~
  INSTALL_CMD="pkill -f '/Applications/Memory.app' 2>/dev/null; pkill -f '/Applications/MemoryConnector.app' 2>/dev/null; sleep 1; rm -rf ~/Applications/Memory.app ~/Applications/MemoryConnector.app && ditto $APP_SRC ~/Applications/Memory.app && open ~/Applications/Memory.app"
  mac_ssh "$INSTALL_CMD"
fi
