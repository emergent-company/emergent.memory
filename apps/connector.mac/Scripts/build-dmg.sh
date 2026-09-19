#!/bin/bash
# build-dmg.sh — Build, sign, notarize, and package the Memory macOS connector
# app as a .dmg. Ported from emergent.memory.mac (now archived).
#
# Usage: ./Scripts/build-dmg.sh [--notarize]
#
# Environment variables (for CI / notarization):
#   VERSION             Release tag (vX.Y.Z) or bare semver (X.Y.Z).
#   DEVELOPMENT_TEAM    Apple Developer Team ID (e.g. "XXXXXXXXXX")
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
DERIVED_DATA="build/DerivedData"
ARCHIVE_PATH="build/Memory.xcarchive"
EXPORT_PATH="build/Export"
DMG_NAME="Memory"
CONFIGURATION="Release"
# Signing identity: APP_CERT_NAME (documented above) overrides the generic
# default so a machine holding several Developer ID identities signs
# deterministically instead of relying on name-substring matching.
SIGN_IDENTITY="${APP_CERT_NAME:-Developer ID Application}"

# --- Version handling -----------------------------------------------------
# VERSION is a release tag (v0.2.0) or bare semver (0.2.0). Derive two values:
#   MARKETING_VERSION        = the semver WITHOUT the leading 'v'
#                              (CFBundleShortVersionString)
#   CURRENT_PROJECT_VERSION  = a monotonic integer (CFBundleVersion), encoded as
#                              major*1000000 + minor*1000 + patch
# The monotonic encoding is REQUIRED: Sparkle 2's generate_appcast orders
# updates by sparkle:version (the app's CFBundleVersion), which must strictly
# increase per published app release.
# Fallback for local runs without VERSION. Info.plist stores the literal
# build-setting placeholder "$(MARKETING_VERSION)", so it cannot be read
# directly; prefer the newest release tag, then project.yml's default.
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
# Guard against an empty or unresolved build-setting placeholder (e.g.
# "$(MARKETING_VERSION)") reaching semver validation. This only happens on
# local runs where VERSION, a release tag, and a concrete project.yml
# MARKETING_VERSION are all unavailable; treat it as a local/dev build with a
# safe default rather than failing validation.
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
CURRENT_PROJECT_VERSION=$(( MAJOR * 1000000 + MINOR * 1000 + PATCH ))

echo "==> Building Memory v${MARKETING_VERSION} (build ${CURRENT_PROJECT_VERSION})"

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
    DEVELOPMENT_TEAM="${DEVELOPMENT_TEAM:-}" \
    CODE_SIGN_STYLE=Manual \
    CODE_SIGN_IDENTITY="${SIGN_IDENTITY}" \
    MARKETING_VERSION="${MARKETING_VERSION}" \
    CURRENT_PROJECT_VERSION="${CURRENT_PROJECT_VERSION}" \
    SWIFT_VERSION=5

# Step 3: Export .app
echo "==> Exporting .app..."
mkdir -p "${EXPORT_PATH}"
xcodebuild -exportArchive \
    -archivePath "${ARCHIVE_PATH}" \
    -exportPath "${EXPORT_PATH}" \
    -exportOptionsPlist Scripts/ExportOptions.plist

APP_PATH="${EXPORT_PATH}/Memory.app"

# Re-sign the whole bundle with hardened runtime before notarization. The
# embedded Go engine + reminders helper are produced unsigned by the pre-build
# scripts, and notarization rejects the app if any Mach-O lacks a hardened
# runtime signature.
# Sign embedded binaries (deepest first), then the app. `codesign --deep` is
# deprecated and unreliable for notarization.
for bin in "${APP_PATH}/Contents/Resources/memory-connector" "${APP_PATH}/Contents/Resources/memory-reminders"; do
  [ -f "$bin" ] && codesign --force --options runtime --sign "${SIGN_IDENTITY}" "$bin"
done
codesign --force --options runtime --sign "${SIGN_IDENTITY}" "${APP_PATH}"

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
