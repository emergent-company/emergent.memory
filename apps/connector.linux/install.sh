#!/usr/bin/env bash
set -euo pipefail

GITHUB_REPO="emergent-company/emergent.memory"
API_URL="https://api.github.com/repos/${GITHUB_REPO}/releases"
# Base URL for release assets. Override with MEMORY_CONNECTOR_BASE_URL to point
# the installer at a mirror (or a local HTTP server, for smoke tests). The script
# appends /v<VERSION>/<asset>.
BASE_URL="${MEMORY_CONNECTOR_BASE_URL:-https://github.com/${GITHUB_REPO}/releases/download}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
tmp_dir=""

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

log() { echo -e "${CYAN}>${NC} $*" >&2; }
success() { echo -e "${GREEN}OK${NC} $*" >&2; }
error() { echo -e "${RED}ERROR${NC} $*" >&2; exit 1; }

usage() {
    cat <<EOF
memory-connector installer

Usage: install.sh [--version X.Y.Z] [--channel CH] [--dir DIR]

Options:
  --version X.Y.Z   Install a specific version (default: newest connector release)
  --channel CH      Release channel: 'stable' (default) or 'dev'
  --dir DIR         Install directory (default: \$HOME/.local/bin)
  -h, --help        Show this help

Channels:
  stable  Install the newest v* release (default).
  dev     Install the rolling dev build from the 'dev' tag.

Environment:
  INSTALL_DIR                 Same as --dir
  MEMORY_CONNECTOR_BASE_URL   Alternate asset base URL. The script fetches
                              \${BASE_URL}/v<VERSION>/<asset>. Defaults
                              to the GitHub release download URL. Useful for
                              mirrors and local smoke tests.

Supported platforms: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
EOF
}

detect_platform() {
    local os arch
    case "$(uname -s)" in
        Linux*)  os=linux;;
        Darwin*) os=darwin;;
        *) error "Unsupported OS: $(uname -s). Supported: linux, darwin";;
    esac
    case "$(uname -m)" in
        x86_64|amd64)  arch=amd64;;
        aarch64|arm64) arch=arm64;;
        *) error "Unsupported architecture: $(uname -m). Supported: amd64, arm64";;
    esac
    echo "${os}/${arch}"
}

resolve_version() {
    # Newest monorepo release (single vX.Y.Z tag carries all artifacts).
    local version
    version=$(curl -fsS -o /dev/null -w '%{redirect_url}' "https://github.com/${GITHUB_REPO}/releases/latest")
    [ -z "$version" ] && error "No release found. Pass --version X.Y.Z."
    version="${version##*/}"
    echo "${version#v}"
}

verify_checksum() {
    local file="$1" checksum_url="$2" expected actual
    if ! expected=$(curl -fsSL "$checksum_url" 2>/dev/null) || [ -z "$expected" ]; then
        log "Checksum file not available; skipping verification"
        return 0
    fi
    expected=$(echo "$expected" | awk '{print $1}')
    if command -v sha256sum &>/dev/null; then
        actual=$(sha256sum "$file" | awk '{print $1}')
    elif command -v shasum &>/dev/null; then
        actual=$(shasum -a 256 "$file" | awk '{print $1}')
    else
        log "No sha256 tool found (sha256sum/shasum); skipping verification"
        return 0
    fi
    [ "$expected" = "$actual" ] || error "Checksum mismatch for $(basename "$file")"
    success "Checksum verified"
}

main() {
    local version="" channel="stable" platform os arch asset download_url binary

    while [ $# -gt 0 ]; do
        case "$1" in
            --version)  [ $# -ge 2 ] || error "--version requires a value"; version="$2"; shift 2;;
            --version=*) version="${1#*=}"; shift;;
            --channel)  [ $# -ge 2 ] || error "--channel requires a value"; channel="$2"; shift 2;;
            --channel=*) channel="${1#*=}"; shift;;
            --dir)      [ $# -ge 2 ] || error "--dir requires a value"; INSTALL_DIR="$2"; shift 2;;
            --dir=*)    INSTALL_DIR="${1#*=}"; shift;;
            -h|--help)  usage; exit 0;;
            *) error "Unknown argument: $1 (try --help)";;
        esac
    done

    command -v curl &>/dev/null || error "curl is required"
    command -v tar &>/dev/null || error "tar is required"

    platform=$(detect_platform)
    os="${platform%/*}"
    arch="${platform#*/}"

    if [ "$channel" = "dev" ]; then
        [ -n "$version" ] && error "--version cannot be combined with --channel dev"
        log "Resolving newest dev build (tag dev)..."
        dev_asset=$(curl -fsSL "${API_URL}/tags/dev" \
            | grep -o '"name":[[:space:]]*"memory-connector_[^"]*_'"${os}"'_'"${arch}"'\.tar\.gz"' \
            | sed -e 's/.*"name":[[:space:]]*"//' -e 's/"$//' \
            | head -1)
        [ -z "$dev_asset" ] && error "No dev build for ${os}/${arch} (tag dev)."
        version=$(printf '%s' "$dev_asset" \
            | sed -e 's/^memory-connector_//' -e 's/_'"${os}"'_'"${arch}"'\.tar\.gz$//')
        asset="$dev_asset"
        download_url="${BASE_URL}/dev/${asset}"
    else
        if [ -z "$version" ]; then
            log "Resolving newest connector release..."
            version=$(resolve_version)
        fi
        version="${version#v}"
        version="${version#v}"

        asset="memory-connector_${version}_${os}_${arch}.tar.gz"
        download_url="${BASE_URL}/v${version}/${asset}"
    fi

    log "Installing memory-connector ${version} for ${platform}..."
    tmp_dir=$(mktemp -d)
    trap 'rm -rf "${tmp_dir:-}"' EXIT

    curl -fsSL "$download_url" -o "${tmp_dir}/${asset}" \
        || error "Download failed: ${download_url}"
    verify_checksum "${tmp_dir}/${asset}" "${download_url}.sha256"

    tar xzf "${tmp_dir}/${asset}" -C "${tmp_dir}" || error "Failed to extract ${asset}"
    binary="${tmp_dir}/memory-connector"
    [ -f "$binary" ] || error "Binary 'memory-connector' not found in ${asset}"

    mkdir -p "$INSTALL_DIR"
    install -m 0755 "$binary" "${INSTALL_DIR}/memory-connector" 2>/dev/null \
        || { mv "$binary" "${INSTALL_DIR}/memory-connector"; chmod +x "${INSTALL_DIR}/memory-connector"; }

    success "Installed memory-connector ${version} to ${INSTALL_DIR}/memory-connector"

    if [[ ":${PATH}:" != *":${INSTALL_DIR}:"* ]]; then
        echo
        log "Add ${INSTALL_DIR} to your PATH:"
        echo "    export PATH=\"${INSTALL_DIR}:\$PATH\""
        log "Add that line to your shell profile (~/.zshrc, ~/.bashrc) to make it permanent."
    fi
}

main "$@"
