#!/bin/bash
# build-dmg.sh — Build, sign, notarize, and package the Memory macOS connector
# app as a .dmg. Ported from emergent.memory.mac (now archived).
#
# Usage: ./Scripts/build-dmg.sh [--notarize]
#
# Environment variables (for CI / notarization):
#   VERSION             Marketing version (defaults to Info.plist value)
#   DEVELOPMENT_TEAM    Apple Developer Team ID (e.g. "XXXXXXXXXX")
#   APP_CERT_NAME       Certificate name for app signing (e.g. "Developer ID Application: ...")
#   NOTARIZE_KEY        Path to App Store Connect API key (.p8)
#   NOTARIZE_KEY_ID     App Store Connect API key ID
#   NOTARIZE_ISSUER     App Store Connect API issuer ID

set -euo pipefail

SCHEME="MemoryConnector"
PROJECT_DIR="MemoryConnector"
PROJECT="${PROJECT_DIR}/${SCHEME}.xcodeproj"
DERIVED_DATA="build/DerivedData"
ARCHIVE_PATH="build/Memory.xcarchive"
EXPORT_PATH="build/Export"
DMG_NAME="Memory"
VERSION="${VERSION:-$(defaults read "$(pwd)/${PROJECT_DIR}/Info.plist" CFBundleShortVersionString 2>/dev/null || echo "0.1.0")}"
CONFIGURATION="Release"

echo "==> Building Memory v${VERSION}"

# Step 1: Generate Xcode project (requires xcodegen)
if command -v xcodegen &>/dev/null; then
    echo "==> Generating Xcode project with XcodeGen..."
    (cd "$PROJECT_DIR" && xcodegen generate)
else
    echo "⚠️  xcodegen not found — using existing .xcodeproj"
fi

# Step 2: Archive
echo "==> Archiving..."
xcodebuild archive \
    -project "${PROJECT}" \
    -scheme "${SCHEME}" \
    -configuration "${CONFIGURATION}" \
    -archivePath "${ARCHIVE_PATH}" \
    -derivedDataPath "${DERIVED_DATA}" \
    DEVELOPMENT_TEAM="${DEVELOPMENT_TEAM:-}" \
    CODE_SIGN_STYLE=Manual \
    CODE_SIGN_IDENTITY="Developer ID Application" \
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
  [ -f "$bin" ] && codesign --force --options runtime --sign "Developer ID Application" "$bin"
done
codesign --force --options runtime --sign "Developer ID Application" "${APP_PATH}"

# Step 4: Notarize (optional)
if [[ "${1:-}" == "--notarize" ]]; then
    echo "==> Notarizing..."
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
DMG_PATH="build/${DMG_NAME}-${VERSION}.dmg"
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

echo ""
echo "✓ Built: ${DMG_PATH}"
