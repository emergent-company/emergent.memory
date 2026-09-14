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
#   NOTARIZE_APPLE_ID   Apple ID for notarization
#   NOTARIZE_PASSWORD   App-specific password for notarization
#   NOTARIZE_TEAM_ID    Team ID for notarization

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
    CODE_SIGN_STYLE="${DEVELOPMENT_TEAM:+Manual}" \
    | xcpretty || true

# Step 3: Export .app
echo "==> Exporting .app..."
mkdir -p "${EXPORT_PATH}"
xcodebuild -exportArchive \
    -archivePath "${ARCHIVE_PATH}" \
    -exportPath "${EXPORT_PATH}" \
    -exportOptionsPlist Scripts/ExportOptions.plist

APP_PATH="${EXPORT_PATH}/Memory.app"

# Step 4: Notarize (optional)
if [[ "${1:-}" == "--notarize" ]]; then
    echo "==> Notarizing..."
    ditto -c -k --keepParent "${APP_PATH}" "${EXPORT_PATH}/Memory.zip"
    xcrun notarytool submit "${EXPORT_PATH}/Memory.zip" \
        --apple-id "${NOTARIZE_APPLE_ID}" \
        --password "${NOTARIZE_PASSWORD}" \
        --team-id "${NOTARIZE_TEAM_ID}" \
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
