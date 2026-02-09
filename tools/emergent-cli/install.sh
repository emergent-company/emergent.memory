#!/usr/bin/env bash
set -euo pipefail

GITHUB_REPO="emergent-company/emergent"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.emergent/bin}"

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

log() { echo -e "${CYAN}>${NC} $*" >&2; }
success() { echo -e "${GREEN}OK${NC} $*" >&2; }
error() { echo -e "${RED}ERROR${NC} $*" >&2; exit 1; }

detect_platform() {
    local os arch
    case "$(uname -s)" in
        Linux*)  os=linux;;
        Darwin*) os=darwin;;
        CYGWIN*|MINGW*|MSYS_NT*) os=windows;;
        FreeBSD*) os=freebsd;;
        *) error "Unsupported OS: $(uname -s)";;
    esac
    case "$(uname -m)" in
        x86_64|amd64)  arch=amd64;;
        aarch64|arm64) arch=arm64;;
        armv7l)        arch=arm;;
        i386|i686)     arch=386;;
        *) error "Unsupported architecture: $(uname -m)";;
    esac
    echo "${os}/${arch}"
}

main() {
    command -v curl &>/dev/null || error "curl is required"
    command -v tar &>/dev/null || error "tar is required"

    local platform version download_url tmp_dir
    platform=$(detect_platform)
    
    log "Fetching latest version..."
    version=$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    [ -z "$version" ] && error "Failed to fetch latest version"
    
    local os=${platform%/*}
    local arch=${platform#*/}
    local ext="tar.gz"
    [ "$os" = "windows" ] && ext="zip"
    
    download_url="https://github.com/${GITHUB_REPO}/releases/download/${version}/emergent-cli-${os}-${arch}.${ext}"
    
    log "Downloading ${version} for ${platform}..."
    tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT
    
    curl -fsSL "$download_url" -o "${tmp_dir}/archive.${ext}" || error "Download failed"
    
    cd "$tmp_dir"
    if [ "$os" = "windows" ]; then
        unzip -q "archive.${ext}"
    else
        tar xzf "archive.${ext}"
    fi
    
    mkdir -p "$INSTALL_DIR"
    
    local binary
    binary=$(find . -type f -name 'emergent-cli*' ! -name '*.tar.gz' ! -name '*.zip' | head -1)
    [ -z "$binary" ] && error "Binary not found in archive"
    
    mv "$binary" "${INSTALL_DIR}/emergent"
    chmod +x "${INSTALL_DIR}/emergent"
    
    success "Installed to ${INSTALL_DIR}/emergent"
    
    if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
        echo
        echo "Add to PATH: export PATH=\"${INSTALL_DIR}:\$PATH\""
    fi
    
    echo
    "${INSTALL_DIR}/emergent" version
}

main "$@"
