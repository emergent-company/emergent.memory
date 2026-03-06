#!/usr/bin/env bash
# release-xcframework.sh — Package the XCFramework into a versioned release ZIP,
# compute the SHA256 checksum, and print the Package.swift snippet needed to
# distribute the SDK via a remote binary target.
#
# Expects the XCFramework to already exist at:
#   dist/swift/EmergentGoCore.xcframework
#
# Run after build-xcframework.sh (or `task server:swift:xcframework`):
#   ./scripts/release-xcframework.sh [--version <semver>] [--output <dir>]
#
# Outputs:
#   dist/swift/EmergentGoCore-<version>.zip
#   dist/swift/EmergentGoCore-<version>.zip.sha256

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUTPUT_DIR="${REPO_ROOT}/dist/swift"
FRAMEWORK_NAME="EmergentGoCore"
XCFW="${OUTPUT_DIR}/${FRAMEWORK_NAME}.xcframework"

# Parse flags
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --output)  OUTPUT_DIR="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

SDK_VERSION="${VERSION:-$(cat "${REPO_ROOT}/../../VERSION" 2>/dev/null || echo "0.0.0")}"
ZIP_NAME="${FRAMEWORK_NAME}-${SDK_VERSION}.zip"
ZIP_PATH="${OUTPUT_DIR}/${ZIP_NAME}"
SHA_PATH="${ZIP_PATH}.sha256"

# ---------------------------------------------------------------------------
# Task 7.1 — Create release ZIP
# ---------------------------------------------------------------------------
[[ -d "${XCFW}" ]] || { echo "ERROR: XCFramework not found at ${XCFW}"; echo "Run: task server:swift:xcframework"; exit 1; }

echo "Creating release ZIP for v${SDK_VERSION}…"
(cd "${OUTPUT_DIR}" && zip -r "${ZIP_NAME}" "${FRAMEWORK_NAME}.xcframework")
echo "  ✓ ${ZIP_PATH}"

# ---------------------------------------------------------------------------
# Task 7.2 — Calculate SHA256 checksum
# ---------------------------------------------------------------------------
echo ""
echo "Computing SHA256 checksum…"
if command -v sha256sum >/dev/null 2>&1; then
  CHECKSUM="$(sha256sum "${ZIP_PATH}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  CHECKSUM="$(shasum -a 256 "${ZIP_PATH}" | awk '{print $1}')"
else
  echo "ERROR: neither sha256sum nor shasum found"
  exit 1
fi

echo "${CHECKSUM}  ${ZIP_NAME}" > "${SHA_PATH}"
echo "  ✓ SHA256: ${CHECKSUM}"
echo "  ✓ Saved to: ${SHA_PATH}"

# ---------------------------------------------------------------------------
# Task 7.3 — Print Package.swift remote binary target snippet
# ---------------------------------------------------------------------------
RELEASE_URL="https://github.com/emergent-company/emergent.memory/releases/download/sdk-v${SDK_VERSION}/${ZIP_NAME}"

cat <<EOF

╔══════════════════════════════════════════════════════════════════════════════╗
║  Package.swift remote binary target snippet (v${SDK_VERSION})
╚══════════════════════════════════════════════════════════════════════════════╝

Upload ${ZIP_NAME} to:
  ${RELEASE_URL}

Then replace the local binaryTarget in swift/EmergentKit/Package.swift with:

    .binaryTarget(
        name: "EmergentGoCore",
        url: "${RELEASE_URL}",
        checksum: "${CHECKSUM}"
    ),

EOF
