#!/usr/bin/env bash
# Install a pinned, checksum-verified gitleaks release binary.
#
# Single source of truth for the gitleaks version used in the required `ci`
# job (.github/workflows/ci.yml). Do NOT bump GITLEAKS_VERSION without also
# updating GITLEAKS_SHA256 to the matching linux_x64 tarball digest (see the
# release's gitleaks_<version>_checksums.txt). This pin is the point of the
# script: gitleaks must be installed explicitly, never assumed to be on the
# runner's PATH (see emergent-company/emergent.memory#1917).
set -euo pipefail

GITLEAKS_VERSION="8.30.1"
GITLEAKS_SHA256="551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb"

# The required `ci` job runs on the self-hosted [linux, x64, memory] runner.
GITLEAKS_ASSET="gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz"
GITLEAKS_URL="https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/${GITLEAKS_ASSET}"

# Install dir. Overridable so callers (CI or a dev shell) can pick a writable,
# PATH-visible location without editing the script.
DEST_DIR="${GITLEAKS_INSTALL_DIR:-$HOME/.local/bin}"

# Idempotent: if the exact pinned version is already on PATH, do nothing.
if command -v gitleaks >/dev/null 2>&1 && [ "$(gitleaks version 2>/dev/null || true)" = "$GITLEAKS_VERSION" ]; then
  echo "gitleaks ${GITLEAKS_VERSION} already installed at $(command -v gitleaks)"
  exit 0
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Downloading gitleaks v${GITLEAKS_VERSION} (${GITLEAKS_ASSET})"
curl -fsSL --retry 3 -o "${TMP_DIR}/${GITLEAKS_ASSET}" "${GITLEAKS_URL}"

echo "Verifying sha256 checksum"
echo "${GITLEAKS_SHA256}  ${GITLEAKS_ASSET}" | (cd "${TMP_DIR}" && sha256sum -c -)

echo "Extracting to ${DEST_DIR}"
mkdir -p "${DEST_DIR}"
tar -xzf "${TMP_DIR}/${GITLEAKS_ASSET}" -C "${TMP_DIR}"
install -m 0755 "${TMP_DIR}/gitleaks" "${DEST_DIR}/gitleaks"

# In CI, make the install dir visible to subsequent steps. Locally, print the
# path so the caller can add it to PATH themselves.
if [ -n "${GITHUB_PATH:-}" ]; then
  echo "${DEST_DIR}" >> "${GITHUB_PATH}"
fi

echo "gitleaks ${GITLEAKS_VERSION} installed to ${DEST_DIR}/gitleaks"
"${DEST_DIR}/gitleaks" version
