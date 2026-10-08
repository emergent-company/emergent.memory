#!/bin/bash
# build-dmg.sh — Build, sign, notarize, and package the Memory macOS connector
# app as a .dmg. Ported from emergent.memory.mac (now archived).
#
# Usage: ./Scripts/build-dmg.sh [--notarize]
#
# Environment variables (for CI / notarization):
#   VERSION             Release tag (vX.Y.Z) or bare semver (X.Y.Z).
#   DEVELOPMENT_TEAM    Apple Developer Team ID. Defaults to this repo's team
#                       (74LC88G9SC) so forks only set the env/secret — no
#                       tracked-file edits. Substituted into a generated
#                       build/ExportOptions.plist (xcodebuild -exportOptionsPlist
#                       cannot read the environment).
#   APP_CERT_NAME       Certificate name for app signing (e.g. "Developer ID Application: ...")
#   NOTARIZE_KEY        Path to App Store Connect API key (.p8)
#   NOTARIZE_KEY_ID     App Store Connect API key ID
#   NOTARIZE_ISSUER     App Store Connect API issuer ID
#
# Signing/notarization ordering is load-bearing: ANY byte change after signing
# invalidates both the code signature and the notarization ticket. The order
# must therefore be:
#   finalize bundle → sign nested Mach-Os → sign app → notarize+staple app →
#   build DMG → sign DMG → notarize+staple DMG.
#
# NOTE: the Sparkle appcast (generate_appcast) is intentionally NOT generated
# here. It needs previously published archives that only the release workflow
# has, and MUST run after this script finishes and staples the DMG. The
# appcast step lives in .github/workflows/mac-release.yml.

set -euo pipefail

SCHEME="MemoryConnector"
PROJECT_DIR="MemoryConnector"
PROJECT="${PROJECT_DIR}/${SCHEME}.xcodeproj"
# Hardened-runtime Enhanced Security entitlements. The re-sign below MUST pass
# --entitlements, otherwise codesign drops them from the shipped app.
ENTITLEMENTS="${PROJECT_DIR}/MemoryConnector.entitlements"
DERIVED_DATA="build/DerivedData"
ARCHIVE_PATH="build/Memory.xcarchive"
EXPORT_PATH="build/Export"
DMG_NAME="Memory"
CONFIGURATION="Release"
# Signing identity: APP_CERT_NAME (documented above) overrides the generic
# default so a machine holding several Developer ID identities signs
# deterministically instead of relying on name-substring matching.
SIGN_IDENTITY="${APP_CERT_NAME:-Developer ID Application}"
# Development team: env/secret override with the repo's team as the documented
# default. Used for both the archive build setting and the generated export
# options, so forks never edit a tracked file.
DEVELOPMENT_TEAM="${DEVELOPMENT_TEAM:-74LC88G9SC}"

# --- Version handling -----------------------------------------------------
# Two channel modes:
#   stable (default): VERSION is a release tag (v0.2.0) or bare semver (0.2.0).
#   dev:              CHANNEL=dev with an explicit BUILD_NUMBER; VERSION may be
#                     a non-semver marketing string (e.g. 0.0.0-dev.abc1234).
#
# Derive two values:
#   MARKETING_VERSION        = CFBundleShortVersionString
#   CURRENT_PROJECT_VERSION  = CFBundleVersion, encoded as:
#                              version_code * 100000 + variant
#                              version_code = major*1000000 + minor*1000 + patch
#                              variant      = 99999 for stable, 1..99998 for dev
# The encoding is shared by both channels and is REQUIRED: Sparkle 2's
# generate_appcast orders updates by sparkle:version (the app's CFBundleVersion),
# which must strictly increase per published release. Sharing the space makes a
# dev build of version V+1 sort above stable V and below stable V+1, so a machine
# can switch channels in either direction (see the mac-update-channel change).
# Fallback for local runs without VERSION. Info.plist stores the literal
# build-setting placeholder "$(MARKETING_VERSION)", so it cannot be read
# directly; prefer the newest release tag, then project.yml's default.
CHANNEL="${CHANNEL:-stable}"
RAW_VERSION="${VERSION:-}"
if [ -z "$RAW_VERSION" ]; then
    RAW_VERSION="$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true)"
fi
if [ -z "$RAW_VERSION" ]; then
    RAW_VERSION="$(awk '/^[[:space:]]*MARKETING_VERSION:/ {gsub(/"/, "", $2); print $2; exit}' "${PROJECT_DIR}/project.yml" 2>/dev/null || true)"
fi
if [[ "$RAW_VERSION" == v* ]]; then
    MARKETING_VERSION="${RAW_VERSION#v}"
else
    MARKETING_VERSION="$RAW_VERSION"
fi

if [[ -n "${BUILD_NUMBER:-}" ]]; then
    # Dev path: the caller supplies a monotonic build number and the marketing
    # version may be non-semver. Guard only against an unresolved placeholder.
    if [[ -z "$MARKETING_VERSION" || "$MARKETING_VERSION" == \$\(* ]]; then
        echo "warning: no dev marketing version resolvable — using 0.0.0-dev.local" >&2
        MARKETING_VERSION="0.0.0-dev.local"
    fi
    if ! [[ "$BUILD_NUMBER" =~ ^[0-9]+$ ]]; then
        echo "error: BUILD_NUMBER '$BUILD_NUMBER' is not an integer" >&2
        exit 1
    fi
    CURRENT_PROJECT_VERSION="$BUILD_NUMBER"
else
    # Stable path: VERSION must be parseable X.Y.Z semver.
    # Guard against an empty or unresolved build-setting placeholder (e.g.
    # "$(MARKETING_VERSION)"). This only happens on local runs where VERSION, a
    # release tag, and a concrete project.yml MARKETING_VERSION are all
    # unavailable; treat it as a local/dev build with a safe default.
    if [[ -z "$MARKETING_VERSION" || "$MARKETING_VERSION" == \$\(* ]]; then
        echo "warning: no release version resolvable — using local/dev version 0.0.0" >&2
        MARKETING_VERSION="0.0.0"
    fi
    if [[ ! "$MARKETING_VERSION" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
        echo "error: VERSION '$RAW_VERSION' is not a parseable X.Y.Z semver" >&2
        exit 1
    fi
    MAJOR="${BASH_REMATCH[1]}"
    MINOR="${BASH_REMATCH[2]}"
    PATCH="${BASH_REMATCH[3]}"
    VERSION_CODE=$(( MAJOR * 1000000 + MINOR * 1000 + PATCH ))
    CURRENT_PROJECT_VERSION=$(( VERSION_CODE * 100000 + 99999 ))
fi

# Per-channel Sparkle feed URL. Written into the exported app's Info.plist below
# (before it is re-signed) so the bundled default feed matches the channel.
case "$CHANNEL" in
    stable) FEED_URL="https://github.com/emergent-company/emergent.memory/releases/download/memory-appcast/appcast.xml" ;;
    dev)    FEED_URL="https://github.com/emergent-company/emergent.memory/releases/download/mac-dev/appcast.xml" ;;
    *)      echo "error: CHANNEL must be 'stable' or 'dev' (got '$CHANNEL')" >&2; exit 1 ;;
esac

echo "==> Building Memory v${MARKETING_VERSION} (build ${CURRENT_PROJECT_VERSION}, channel ${CHANNEL})"

# Step 1: Generate Xcode project (requires xcodegen)
if command -v xcodegen &>/dev/null; then
    echo "==> Generating Xcode project with XcodeGen..."
    (cd "$PROJECT_DIR" && xcodegen generate)
else
    echo "⚠️  xcodegen not found — using existing .xcodeproj"
fi

# Step 2: Archive. MARKETING_VERSION / CURRENT_PROJECT_VERSION override the
# target settings in project.yml so the built app's Info.plist carries the
# exact release version and monotonic build number.
echo "==> Archiving..."
xcodebuild archive \
    -project "${PROJECT}" \
    -scheme "${SCHEME}" \
    -configuration "${CONFIGURATION}" \
    -archivePath "${ARCHIVE_PATH}" \
    -derivedDataPath "${DERIVED_DATA}" \
    DEVELOPMENT_TEAM="${DEVELOPMENT_TEAM}" \
    CODE_SIGN_STYLE=Manual \
    CODE_SIGN_IDENTITY="${SIGN_IDENTITY}" \
    MARKETING_VERSION="${MARKETING_VERSION}" \
    CURRENT_PROJECT_VERSION="${CURRENT_PROJECT_VERSION}"

# Step 3: Export .app
echo "==> Exporting .app..."
mkdir -p "${EXPORT_PATH}"
# xcodebuild -exportOptionsPlist cannot read the environment, so render a
# resolved copy of the tracked template with the configured team.
EXPORT_OPTIONS_PLIST="build/ExportOptions.plist"
cp Scripts/ExportOptions.plist "${EXPORT_OPTIONS_PLIST}"
plutil -replace teamID -string "${DEVELOPMENT_TEAM}" "${EXPORT_OPTIONS_PLIST}"
xcodebuild -exportArchive \
    -archivePath "${ARCHIVE_PATH}" \
    -exportPath "${EXPORT_PATH}" \
    -exportOptionsPlist "${EXPORT_OPTIONS_PLIST}"

APP_PATH="${EXPORT_PATH}/Memory.app"

# Bundle this channel's feed URL as the default SUFeedURL. MUST run before the
# codesign steps below — the code signature seals Info.plist, so any byte change
# after signing invalidates it (and the notarization ticket).
PLIST="${APP_PATH}/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :SUFeedURL ${FEED_URL}" "${PLIST}" 2>/dev/null \
    || /usr/libexec/PlistBuddy -c "Add :SUFeedURL string ${FEED_URL}" "${PLIST}"
echo "==> Bundled SUFeedURL: ${FEED_URL}"

# Re-sign the whole bundle with hardened runtime before notarization. The
# embedded Go engine + reminders helper are produced unsigned by the pre-build
# scripts, and notarization rejects the app if any Mach-O lacks a hardened
# runtime signature.
# Sign embedded binaries (deepest first), then the app. `codesign --deep` is
# deprecated and unreliable for notarization.
for bin in "${APP_PATH}/Contents/Resources/memory-connector" "${APP_PATH}/Contents/Resources/memory-reminders"; do
  [ -f "$bin" ] && codesign --force --options runtime --timestamp --sign "${SIGN_IDENTITY}" "$bin"
done
codesign --force --options runtime --timestamp --entitlements "${ENTITLEMENTS}" --sign "${SIGN_IDENTITY}" "${APP_PATH}"

# Verify the re-sign before spending time on notarization. `codesign` does not
# preserve entitlements by default, so confirm the Enhanced Security
# entitlements were actually embedded — dropping them would silently make the
# hardened-process claim false — and that the signature is valid.
codesign --verify --verbose=2 "${APP_PATH}"
if ! codesign -d --entitlements :- "${APP_PATH}" 2>/dev/null | grep -q 'com.apple.security.hardened-process'; then
    echo "error: hardened-process entitlements missing from the signed app after re-sign" >&2
    exit 1
fi

# Step 4: Notarize + staple the app (optional)
if [[ "${1:-}" == "--notarize" ]]; then
    echo "==> Notarizing app..."
    ditto -c -k --keepParent "${APP_PATH}" "${EXPORT_PATH}/Memory.zip"
    xcrun notarytool submit "${EXPORT_PATH}/Memory.zip" \
        --key "${NOTARIZE_KEY}" \
        --key-id "${NOTARIZE_KEY_ID}" \
        --issuer "${NOTARIZE_ISSUER}" \
        --wait
    xcrun stapler staple "${APP_PATH}"
fi

# Step 5: Create .dmg
echo "==> Creating .dmg..."
DMG_PATH="build/${DMG_NAME}-${MARKETING_VERSION}.dmg"
if command -v create-dmg &>/dev/null; then
    create-dmg \
        --volname "Memory" \
        --window-pos 200 120 \
        --window-size 600 400 \
        --icon-size 100 \
        --icon "Memory.app" 175 190 \
        --hide-extension "Memory.app" \
        --app-drop-link 425 190 \
        "${DMG_PATH}" \
        "${EXPORT_PATH}/"
else
    hdiutil create -volname "Memory" \
        -srcfolder "${EXPORT_PATH}" \
        -ov -format UDZO \
        "${DMG_PATH}"
fi

# Step 6: Sign the DMG. Must happen AFTER the DMG is finalized.
echo "==> Signing DMG..."
codesign --force --timestamp --sign "${SIGN_IDENTITY}" "${DMG_PATH}"

# Step 7: Notarize + staple the DMG (optional). notarytool accepts the .dmg
# directly; no need to re-zip.
if [[ "${1:-}" == "--notarize" ]]; then
    echo "==> Notarizing DMG..."
    xcrun notarytool submit "${DMG_PATH}" \
        --key "${NOTARIZE_KEY}" \
        --key-id "${NOTARIZE_KEY_ID}" \
        --issuer "${NOTARIZE_ISSUER}" \
        --wait
    xcrun stapler staple "${DMG_PATH}"
fi

echo ""
echo "✓ Built: ${DMG_PATH}"
echo "  (appcast generation runs separately — see .github/workflows/mac-release.yml)"
