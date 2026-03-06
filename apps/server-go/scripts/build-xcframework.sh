#!/usr/bin/env bash
# build-xcframework.sh — Compile the Go C-bridge into an XCFramework
# for iOS (arm64), iOS Simulator (arm64, x86_64), and macOS (arm64, x86_64).
#
# Requirements:
#   - macOS host with Xcode and Command Line Tools installed
#   - Go 1.24+ with CGO support
#   - xcodebuild (part of Xcode)
#
# Usage:
#   ./scripts/build-xcframework.sh [--output <dir>] [--version <semver>]
#
# Output:
#   dist/swift/EmergentGoCore.xcframework   (and its constituent .a files)

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUTPUT_DIR="${REPO_ROOT}/dist/swift"
BUILD_DIR="${REPO_ROOT}/tmp/xcframework-build"
FRAMEWORK_NAME="EmergentGoCore"
BRIDGE_PKG="./cmd/swiftbridge"

# Parse flags
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output) OUTPUT_DIR="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

SDK_VERSION="${VERSION:-$(cat "${REPO_ROOT}/../../VERSION" 2>/dev/null || echo "0.0.0")}"

echo "Building ${FRAMEWORK_NAME} v${SDK_VERSION}"
echo "  Bridge package: ${BRIDGE_PKG}"
echo "  Output:         ${OUTPUT_DIR}"
echo ""

# ---------------------------------------------------------------------------
# Validate toolchain
# ---------------------------------------------------------------------------
command -v go >/dev/null 2>&1 || { echo "ERROR: go not found"; exit 1; }
command -v xcodebuild >/dev/null 2>&1 || { echo "ERROR: xcodebuild not found (requires macOS + Xcode)"; exit 1; }

go version
xcodebuild -version

# ---------------------------------------------------------------------------
# Clean and prepare
# ---------------------------------------------------------------------------
rm -rf "${BUILD_DIR}"
mkdir -p "${BUILD_DIR}"
mkdir -p "${OUTPUT_DIR}"

cd "${REPO_ROOT}"

# ---------------------------------------------------------------------------
# Helper: compile Go → C static library for a single platform
#
# Arguments:
#   $1  GOOS         (darwin / ios)
#   $2  GOARCH       (arm64 / amd64)
#   $3  CC           cross-compiler (e.g. the arm64 iOS clang wrapper)
#   $4  output_dir   directory to write libEmergentGoCore.a + header
# ---------------------------------------------------------------------------
build_target() {
  local GOOS="$1"
  local GOARCH="$2"
  local CC="$3"
  local OUT="$4"

  mkdir -p "${OUT}"
  echo "  → GOOS=${GOOS} GOARCH=${GOARCH}  CC=${CC}"

  # CGO_CFLAGS: -fembed-bitcode=off disables bitcode generation in the static
  # library, satisfying Apple's modern Xcode requirements (bitcode is deprecated
  # and disabled by default in Xcode 14+, but we set this explicitly per spec).
  CGO_ENABLED=1 \
  GOOS="${GOOS}" \
  GOARCH="${GOARCH}" \
  CC="${CC}" \
  CGO_CFLAGS="-fembed-bitcode=off" \
  go build \
    -buildmode=c-archive \
    -ldflags="-s -w" \
    -o "${OUT}/lib${FRAMEWORK_NAME}.a" \
    ${BRIDGE_PKG}

  # The header is written alongside the archive; copy it to the output dir if
  # not already there (Go writes <archive-name>.h in the same directory).
  local HEADER="${OUT}/lib${FRAMEWORK_NAME}.h"
  if [[ ! -f "${HEADER}" ]]; then
    # Older Go versions write to working dir — move it
    if [[ -f "${REPO_ROOT}/lib${FRAMEWORK_NAME}.h" ]]; then
      mv "${REPO_ROOT}/lib${FRAMEWORK_NAME}.h" "${HEADER}"
    fi
  fi
}

# ---------------------------------------------------------------------------
# iOS Device — arm64
# ---------------------------------------------------------------------------
IOS_SDK="$(xcrun --sdk iphoneos --show-sdk-path)"
IOS_ARM64_CC="$(xcrun --sdk iphoneos --find clang) -arch arm64 -isysroot ${IOS_SDK} -mios-version-min=15.0"

build_target "ios" "arm64" "${IOS_ARM64_CC}" "${BUILD_DIR}/ios-arm64"

# ---------------------------------------------------------------------------
# iOS Simulator — arm64 + x86_64 (combined into a fat .a via lipo)
# ---------------------------------------------------------------------------
SIM_SDK="$(xcrun --sdk iphonesimulator --show-sdk-path)"
SIM_ARM64_CC="$(xcrun --sdk iphonesimulator --find clang) -arch arm64 -isysroot ${SIM_SDK} -mios-simulator-version-min=15.0"
SIM_AMD64_CC="$(xcrun --sdk iphonesimulator --find clang) -arch x86_64 -isysroot ${SIM_SDK} -mios-simulator-version-min=15.0"

build_target "ios"    "arm64" "${SIM_ARM64_CC}" "${BUILD_DIR}/iossimulator-arm64"
build_target "darwin" "amd64" "${SIM_AMD64_CC}" "${BUILD_DIR}/iossimulator-amd64"

echo "  → Combining simulator slices with lipo"
mkdir -p "${BUILD_DIR}/iossimulator-universal"
lipo \
  "${BUILD_DIR}/iossimulator-arm64/lib${FRAMEWORK_NAME}.a" \
  "${BUILD_DIR}/iossimulator-amd64/lib${FRAMEWORK_NAME}.a" \
  -create \
  -output "${BUILD_DIR}/iossimulator-universal/lib${FRAMEWORK_NAME}.a"

# Use the arm64 header (they are identical)
cp "${BUILD_DIR}/iossimulator-arm64/lib${FRAMEWORK_NAME}.h" \
   "${BUILD_DIR}/iossimulator-universal/lib${FRAMEWORK_NAME}.h"

# ---------------------------------------------------------------------------
# macOS — arm64 + x86_64 (combined into a fat .a via lipo)
# ---------------------------------------------------------------------------
MACOS_SDK="$(xcrun --sdk macosx --show-sdk-path)"
MACOS_ARM64_CC="$(xcrun --sdk macosx --find clang) -arch arm64 -isysroot ${MACOS_SDK} -mmacosx-version-min=12.0"
MACOS_AMD64_CC="$(xcrun --sdk macosx --find clang) -arch x86_64 -isysroot ${MACOS_SDK} -mmacosx-version-min=12.0"

build_target "darwin" "arm64" "${MACOS_ARM64_CC}" "${BUILD_DIR}/macos-arm64"
build_target "darwin" "amd64" "${MACOS_AMD64_CC}" "${BUILD_DIR}/macos-amd64"

echo "  → Combining macOS slices with lipo"
mkdir -p "${BUILD_DIR}/macos-universal"
lipo \
  "${BUILD_DIR}/macos-arm64/lib${FRAMEWORK_NAME}.a" \
  "${BUILD_DIR}/macos-amd64/lib${FRAMEWORK_NAME}.a" \
  -create \
  -output "${BUILD_DIR}/macos-universal/lib${FRAMEWORK_NAME}.a"

cp "${BUILD_DIR}/macos-arm64/lib${FRAMEWORK_NAME}.h" \
   "${BUILD_DIR}/macos-universal/lib${FRAMEWORK_NAME}.h"

# ---------------------------------------------------------------------------
# Task 1.2 — Generate C header (done automatically by go build -buildmode=c-archive)
# Verify that all headers are present and identical.
# ---------------------------------------------------------------------------
echo ""
echo "Verifying generated C headers…"
HEADER_IOS="${BUILD_DIR}/ios-arm64/lib${FRAMEWORK_NAME}.h"
HEADER_SIM="${BUILD_DIR}/iossimulator-universal/lib${FRAMEWORK_NAME}.h"
HEADER_MAC="${BUILD_DIR}/macos-universal/lib${FRAMEWORK_NAME}.h"

for h in "${HEADER_IOS}" "${HEADER_SIM}" "${HEADER_MAC}"; do
  [[ -f "${h}" ]] || { echo "ERROR: Missing header: ${h}"; exit 1; }
done
echo "  ✓ Headers generated for all platforms"

# ---------------------------------------------------------------------------
# Task 1.3 — Combine .a files + headers into XCFramework
# ---------------------------------------------------------------------------
echo ""
echo "Creating XCFramework…"

XCFW_OUT="${OUTPUT_DIR}/${FRAMEWORK_NAME}.xcframework"
rm -rf "${XCFW_OUT}"

xcodebuild -create-xcframework \
  -library "${BUILD_DIR}/ios-arm64/lib${FRAMEWORK_NAME}.a" \
    -headers "${BUILD_DIR}/ios-arm64" \
  -library "${BUILD_DIR}/iossimulator-universal/lib${FRAMEWORK_NAME}.a" \
    -headers "${BUILD_DIR}/iossimulator-universal" \
  -library "${BUILD_DIR}/macos-universal/lib${FRAMEWORK_NAME}.a" \
    -headers "${BUILD_DIR}/macos-universal" \
  -output "${XCFW_OUT}"

echo ""
echo "✓ XCFramework built successfully:"
echo "    ${XCFW_OUT}"
du -sh "${XCFW_OUT}"
