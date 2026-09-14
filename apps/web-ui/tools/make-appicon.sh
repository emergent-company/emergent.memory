#!/usr/bin/env bash
# Regenerate the MemoryConnector macOS app icon from the web UI's PWA icon.
#
# Source of truth: gateway/webui/static/icon-512.png (the Memory web UI icon).
# Steps: render a 1024 base, then sips-resize into the AppIcon.appiconset.
# Run on macOS (needs sips). The generated PNGs are committed, so a normal
# build does not require this script.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
SRC="$REPO/gateway/webui/static/icon-512.png"
DEST="$REPO/client/macos/MemoryConnector/Resources/Assets.xcassets/AppIcon.appiconset"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

command -v sips >/dev/null || { echo "sips not found (run on macOS)" >&2; exit 1; }
[ -f "$SRC" ] || { echo "missing source icon: $SRC" >&2; exit 1; }

sips -z 1024 1024 "$SRC" --out "$TMP/base1024.png" >/dev/null
for s in 16 32 64 128 256 512 1024; do
  sips -z "$s" "$s" "$TMP/base1024.png" --out "$TMP/icon_$s.png" >/dev/null
done

mkdir -p "$DEST"
cp "$TMP/icon_16.png"   "$DEST/icon_16x16.png"
cp "$TMP/icon_32.png"   "$DEST/icon_16x16@2x.png"
cp "$TMP/icon_32.png"   "$DEST/icon_32x32.png"
cp "$TMP/icon_64.png"   "$DEST/icon_32x32@2x.png"
cp "$TMP/icon_128.png"  "$DEST/icon_128x128.png"
cp "$TMP/icon_256.png"  "$DEST/icon_128x128@2x.png"
cp "$TMP/icon_256.png"  "$DEST/icon_256x256.png"
cp "$TMP/icon_512.png"  "$DEST/icon_256x256@2x.png"
cp "$TMP/icon_512.png"  "$DEST/icon_512x512.png"
cp "$TMP/icon_1024.png" "$DEST/icon_512x512@2x.png"

echo "AppIcon refreshed from $SRC -> $DEST"
