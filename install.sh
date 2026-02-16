#!/bin/sh
# Emergent Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent/main/install.sh | sh
#
# Environment variables:
#   EMERGENT_VERSION  - Specific version to install (default: latest)
#   EMERGENT_DIR      - Installation directory (default: ~/.emergent)

set -e

# Configuration
GITHUB_REPO="emergent-company/emergent"
BINARY_NAME="emergent"
DEFAULT_INSTALL_DIR="${HOME}/.emergent"

# Colors (if terminal supports them)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BLUE='\033[0;34m'
    MUTED='\033[0;2m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    MUTED=''
    NC=''
fi

info() {
    printf "${BLUE}==>${NC} %s\n" "$1"
}

success() {
    printf "${GREEN}==>${NC} %s\n" "$1"
}

warn() {
    printf "${YELLOW}Warning:${NC} %s\n" "$1"
}

error() {
    printf "${RED}Error:${NC} %s\n" "$1" >&2
    exit 1
}

# Detect OS and architecture
detect_platform() {
    OS="$(uname -s)"
    ARCH="$(uname -m)"

    case "$OS" in
        Darwin)
            OS="darwin"
            ;;
        Linux)
            OS="linux"
            ;;
        *)
            error "Unsupported operating system: $OS"
            ;;
    esac

    case "$ARCH" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        arm64|aarch64)
            ARCH="arm64"
            ;;
        *)
            error "Unsupported architecture: $ARCH"
            ;;
    esac

    PLATFORM="${OS}-${ARCH}"
}

# Check for Arch Linux / Pacman
is_arch_linux() {
    if [ -f "/etc/arch-release" ] || [ -f "/etc/manjaro-release" ]; then
        return 0
    fi
    if command -v pacman >/dev/null 2>&1; then
        return 0
    fi
    return 1
}

# Get latest version from GitHub releases
get_latest_version() {
    printf "${BLUE}==>${NC} Fetching latest version...\n" >&2
    LATEST=$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    
    if [ -z "$LATEST" ]; then
        error "Failed to get latest version. Check your internet connection."
    fi
    
    echo "$LATEST"
}

# Install using Pacman (Arch Linux)
install_arch() {
    VERSION="${EMERGENT_VERSION:-$(get_latest_version)}"
    # Strip 'v' prefix if present
    CLEAN_VERSION="${VERSION#v}"
    
    info "Arch Linux detected. Installing Emergent ${VERSION}..."

    TMP_DIR=$(mktemp -d)
    trap "rm -rf ${TMP_DIR}" EXIT
    
    # URL for the pre-built package
    ARCH_PKG_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/emergent-${CLEAN_VERSION}-1-x86_64.pkg.tar.zst"
    
    if [ "$(uname -m)" != "x86_64" ]; then
        warn "Pre-built Arch packages are only available for x86_64. Falling back to generic install."
        install_generic
        return
    fi
    
    info "Downloading pre-built package: ${ARCH_PKG_URL}"
    
    if curl -fsSL "${ARCH_PKG_URL}" -o "${TMP_DIR}/emergent.pkg.tar.zst"; then
        info "Installing package..."
        if command -v sudo >/dev/null 2>&1; then
            sudo pacman -U --noconfirm --overwrite '*' "${TMP_DIR}/emergent.pkg.tar.zst"
        else
            if [ "$(id -u)" -eq 0 ]; then
                pacman -U --noconfirm --overwrite '*' "${TMP_DIR}/emergent.pkg.tar.zst"
            else
                su -c "pacman -U --noconfirm --overwrite '*' ${TMP_DIR}/emergent.pkg.tar.zst"
            fi
        fi
        success "Emergent installed successfully via Pacman!"
        
        echo ""
        info "Services installed:"
        echo "  - emergent-server.service (Server)"
        echo "  - emergent-admin.service (Admin UI)"
        info "Configuration at /etc/emergent/"
        echo ""
        info "Next steps:"
        echo "  1. Configure /etc/emergent/.env with your settings"
        echo "  2. Enable and start services:"
        echo "     sudo systemctl enable --now emergent-server"
        echo "     sudo systemctl enable --now emergent-admin"
        echo ""
    else
        warn "Pre-built package not found (maybe release is still building?). Falling back to generic install."
        install_generic
    fi
}

# Generic Download and install (macOS, non-Arch Linux)
install_generic() {
    INSTALL_DIR="${EMERGENT_DIR:-$DEFAULT_INSTALL_DIR}"
    VERSION="${EMERGENT_VERSION:-$(get_latest_version)}"
    
    detect_platform
    
    info "Installing Emergent ${VERSION} to ${INSTALL_DIR}..."
    
    # Create installation directory
    mkdir -p "${INSTALL_DIR}/bin"
    mkdir -p "${INSTALL_DIR}/data"
    mkdir -p "${INSTALL_DIR}/logs"
    
    # Construct download URL for CLI
    DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/emergent-cli-${PLATFORM}.tar.gz"
    
    info "Downloading CLI from: ${DOWNLOAD_URL}"
    
    # Download and extract
    TMP_DIR=$(mktemp -d)
    trap "rm -rf ${TMP_DIR}" EXIT
    
    curl -fsSL "${DOWNLOAD_URL}" -o "${TMP_DIR}/emergent-cli.tar.gz" || error "Download failed. Check if version ${VERSION} exists."
    
    # Extract
    tar -xzf "${TMP_DIR}/emergent-cli.tar.gz" -C "${TMP_DIR}"
    
    # Install binary
    if [ -f "${TMP_DIR}/emergent" ]; then
        mv "${TMP_DIR}/emergent" "${INSTALL_DIR}/bin/emergent"
    elif [ -f "${TMP_DIR}/emergent-cli" ]; then
        mv "${TMP_DIR}/emergent-cli" "${INSTALL_DIR}/bin/emergent"
    else
        error "Binary not found in tarball"
    fi
    chmod +x "${INSTALL_DIR}/bin/emergent"
    
    success "Emergent CLI installed to ${INSTALL_DIR}/bin/emergent"
    
    # Check if emergent is in PATH
    case ":${PATH}:" in
        *":${INSTALL_DIR}/bin:"*)
            success "emergent is in your PATH"
            ;;
        *)
            echo ""
            warn "Add ${INSTALL_DIR}/bin to your PATH:"
            echo ""
            echo "  export PATH=\"\${HOME}/.emergent/bin:\${PATH}\""
            echo ""
            echo "Add this to your shell profile (~/.bashrc, ~/.zshrc, etc.) to make it permanent"
            echo ""
            ;;
    esac

    # Verify installation
    if "${INSTALL_DIR}/bin/emergent" version >/dev/null 2>&1; then
        success "Installation verified"
    else
        warn "Installation complete but verification failed. Please check the binary."
    fi

    echo ""
    info "Next steps:"
    echo "  1. Run 'emergent install' to set up the server"
    echo "  2. Or use 'emergent --help' to see available commands"
    echo ""
}

# Uninstall
uninstall() {
    detect_platform
    
    if [ "$OS" = "linux" ] && is_arch_linux; then
        info "Uninstalling via pacman..."
        if command -v sudo >/dev/null 2>&1; then
            sudo pacman -Rns emergent
        else
            if [ "$(id -u)" -eq 0 ]; then
                pacman -Rns emergent
            else
                su -c "pacman -Rns emergent"
            fi
        fi
        success "Uninstalled."
    else
        INSTALL_DIR="${EMERGENT_DIR:-$DEFAULT_INSTALL_DIR}"
        if [ ! -d "${INSTALL_DIR}" ]; then
            error "Emergent is not installed at ${INSTALL_DIR}"
        fi
        
        info "Uninstalling from ${INSTALL_DIR}..."
        rm -rf "${INSTALL_DIR}/bin/emergent"
        
        success "Binaries removed."
        warn "Data directory preserved at ${INSTALL_DIR}/data"
        echo "  To remove completely: rm -rf ${INSTALL_DIR}"
    fi
}

version() {
    detect_platform
    if [ "$OS" = "linux" ] && is_arch_linux; then
        if pacman -Qi emergent >/dev/null 2>&1; then
            pacman -Qi emergent | grep Version
        else
            echo "emergent is not installed via pacman"
        fi
    else
        INSTALL_DIR="${EMERGENT_DIR:-$DEFAULT_INSTALL_DIR}"
        if [ -x "${INSTALL_DIR}/bin/emergent" ]; then
            "${INSTALL_DIR}/bin/emergent" version
        else
            echo "emergent is not installed at ${INSTALL_DIR}/bin/emergent"
        fi
    fi
}

# Main
main() {
    CMD="${1:-install}"
    detect_platform
    
    case "$CMD" in
        install|upgrade)
            # Check for Arch
            if [ "$OS" = "linux" ] && is_arch_linux; then
                install_arch
            else
                install_generic
            fi
            ;;
        uninstall)
            uninstall
            ;;
        version)
            version
            ;;
        --help|-h)
            echo "Emergent Installer"
            echo "Usage: $0 [install|upgrade|uninstall|version]"
            ;;
        *)
            # If no argument or unknown, default to install
            if [ "$OS" = "linux" ] && is_arch_linux; then
                install_arch
            else
                install_generic
            fi
            ;;
    esac
}

main "$@"
