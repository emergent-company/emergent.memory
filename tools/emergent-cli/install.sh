#!/usr/bin/env bash
#
# Emergent CLI Install Script
# ===========================
# One-line install: curl -fsSL https://install.emergent.ai/cli | bash
#
# Automatically detects your OS and architecture, downloads the latest release,
# and installs to /usr/local/bin (or ~/bin if not root)

set -euo pipefail

SCRIPT_VERSION="0.3.0"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
GITHUB_REPO="emergent-company/emergent"

# Colors
BOLD='\033[1m'
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

log() {
    echo -e "${CYAN}▶${NC} $*" >&2
}

success() {
    echo -e "${GREEN}✓${NC} $*" >&2
}

error() {
    echo -e "${RED}✗${NC} $*" >&2
}

warn() {
    echo -e "${YELLOW}⚠${NC} $*" >&2
}

banner() {
    echo -e "${BOLD}${BLUE}"
    cat <<'EOF'
 ___                                  _    ___ _    ___ 
| __|_ __  ___ _ _ __ _ ___ _ _| |_ / __| |  |_ _|
| _|| '  \/ -_) '_/ _` / -_) ' \  _| | (__| |__ | | 
|___|_|_|_\___|_| \__, \___|_||_\__| \___|____|___|
                  |___/                             
EOF
    echo -e "${NC}"
    echo -e "${BOLD}Emergent CLI Installer v${SCRIPT_VERSION}${NC}"
    echo
}

detect_os() {
    local os
    case "$(uname -s)" in
        Linux*)     os=linux;;
        Darwin*)    os=darwin;;
        CYGWIN*|MINGW*|MSYS_NT*) os=windows;;
        FreeBSD*)   os=freebsd;;
        *)
            error "Unsupported OS: $(uname -s)"
            exit 1
            ;;
    esac
    echo "$os"
}

detect_arch() {
    local arch
    case "$(uname -m)" in
        x86_64|amd64)   arch=amd64;;
        aarch64|arm64)  arch=arm64;;
        armv7l)         arch=arm;;
        i386|i686)      arch=386;;
        *)
            error "Unsupported architecture: $(uname -m)"
            exit 1
            ;;
    esac
    echo "$arch"
}

check_dependencies() {
    log "Checking dependencies..."
    
    local missing=()
    
    if ! command -v curl &> /dev/null; then
        missing+=("curl")
    fi
    
    if ! command -v tar &> /dev/null; then
        missing+=("tar")
    fi
    
    if [ ${#missing[@]} -ne 0 ]; then
        error "Missing required tools: ${missing[*]}"
        echo
        echo "Please install:"
        for tool in "${missing[@]}"; do
            case "$tool" in
                curl)
                    echo "  curl: apt install curl (Debian/Ubuntu) or yum install curl (CentOS/RHEL)"
                    ;;
                tar)
                    echo "  tar: apt install tar (Debian/Ubuntu) or yum install tar (CentOS/RHEL)"
                    ;;
            esac
        done
        exit 1
    fi
    
    success "All dependencies met"
}

get_latest_version() {
    log "Fetching latest release version..."
    
    local latest
    latest=$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    
    if [ -z "$latest" ]; then
        error "Failed to fetch latest version"
        exit 1
    fi
    
    echo "$latest"
}

download_and_install() {
    local os=$1
    local arch=$2
    local version=$3
    
    local binary_name="emergent-cli"
    local archive_name="${binary_name}-${os}-${arch}"
    local extension="tar.gz"
    
    if [ "$os" = "windows" ]; then
        extension="zip"
    fi
    
    local download_url="https://github.com/${GITHUB_REPO}/releases/download/${version}/${archive_name}.${extension}"
    
    log "Downloading from ${download_url}..."
    
    local tmp_dir
    tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT
    
    # Retry logic for when releases aren't ready yet
    local max_retries=5
    local retry_interval=30
    local attempt=1
    local downloaded=false
    
    while [ $attempt -le $max_retries ]; do
        if curl -fsSL "$download_url" -o "${tmp_dir}/${archive_name}.${extension}" 2>/dev/null; then
            downloaded=true
            break
        fi
        
        if [ $attempt -eq $max_retries ]; then
            error "Failed to download after $max_retries attempts"
            error "URL: $download_url"
            error "The release may not be ready yet. Try again in a few minutes."
            exit 1
        fi
        
        warn "Download failed (attempt $attempt/$max_retries)"
        warn "Release may still be building. Retrying in ${retry_interval}s..."
        sleep $retry_interval
        attempt=$((attempt + 1))
    done
    
    if [ "$downloaded" = true ]; then
        success "Downloaded successfully"
    fi
    
    log "Extracting archive..."
    
    cd "$tmp_dir"
    
    if [ "$os" = "windows" ]; then
        unzip -q "${archive_name}.${extension}"
    else
        tar xzf "${archive_name}.${extension}"
    fi
    
    success "Extracted archive"
    
    # Determine install directory (fallback to ~/bin if not root)
    local install_path="$INSTALL_DIR"
    if [ ! -w "$INSTALL_DIR" ] && [ "$INSTALL_DIR" = "/usr/local/bin" ]; then
        warn "No write permission to /usr/local/bin, installing to ~/bin instead"
        install_path="$HOME/bin"
        mkdir -p "$install_path"
    fi
    
    log "Installing to ${install_path}..."
    
    # Find the binary (handle different archive structures)
    local binary_path
    if [ -f "${binary_name}" ]; then
        binary_path="${binary_name}"
    elif [ -f "${archive_name}" ]; then
        binary_path="${archive_name}"
    elif [ -f "${binary_name}.exe" ]; then
        binary_path="${binary_name}.exe"
    else
        error "Binary not found in archive"
        ls -la
        exit 1
    fi
    
    # Move and make executable
    mv "$binary_path" "${install_path}/${binary_name}"
    chmod +x "${install_path}/${binary_name}"
    
    success "Installed to ${install_path}/${binary_name}"
    
    # Check if install_path is in PATH
    if [[ ":$PATH:" != *":${install_path}:"* ]]; then
        warn "${install_path} is not in your PATH"
        echo
        echo "Add this to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
        echo "  export PATH=\"${install_path}:\$PATH\""
    fi
}

verify_installation() {
    log "Verifying installation..."
    
    if command -v emergent-cli &> /dev/null; then
        success "Installation successful!"
        echo
        emergent-cli version
    else
        warn "emergent-cli not found in PATH"
        echo
        echo "Try running: export PATH=\"${INSTALL_DIR}:\$PATH\""
        echo "Or restart your terminal"
    fi
}

main() {
    banner
    
    check_dependencies
    
    local os
    local arch
    local version
    
    os=$(detect_os)
    arch=$(detect_arch)
    
    log "Detected platform: ${os}/${arch}"
    
    if [ "$VERSION" = "latest" ]; then
        version=$(get_latest_version)
        success "Latest version: ${version}"
    else
        version="$VERSION"
        log "Installing version: ${version}"
    fi
    
    download_and_install "$os" "$arch" "$version"
    verify_installation
    
    echo
    echo -e "${BOLD}Quick Start:${NC}"
    echo
    echo "  # Configure CLI (standalone mode)"
    echo "  export EMERGENT_SERVER_URL=http://localhost:9090"
    echo "  export EMERGENT_API_KEY=your-api-key"
    echo
    echo "  # Test connection"
    echo "  emergent-cli config show"
    echo
    echo -e "${BOLD}Documentation:${NC}"
    echo "  https://github.com/${GITHUB_REPO}/tree/master/tools/emergent-cli"
    echo
}

# When piped (curl | bash), BASH_SOURCE[0] is empty, so default to running main
if [[ "${BASH_SOURCE[0]:-}" == "${0:-}" ]] || [[ -z "${BASH_SOURCE[0]:-}" ]]; then
    main "$@"
fi
