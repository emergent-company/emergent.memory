#!/usr/bin/env bash
set -euo pipefail

GITHUB_REPO="emergent-company/emergent.memory"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.memory/bin}"

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
        *) error "Unsupported OS: $(uname -s). Supported: linux, darwin";;
    esac
    case "$(uname -m)" in
        x86_64|amd64)  arch=amd64;;
        aarch64|arm64) arch=arm64;;
        *) error "Unsupported architecture: $(uname -m). Supported: amd64, arm64";;
    esac
    # linux/arm64 not distributed — only linux/amd64 and darwin/{amd64,arm64}
    if [ "$os" = "linux" ] && [ "$arch" != "amd64" ]; then
        error "Linux is only supported on amd64. Got: linux/$arch"
    fi
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
    
    download_url="https://github.com/${GITHUB_REPO}/releases/download/${version}/memory-cli-${os}-${arch}.${ext}"
    
    log "Downloading ${version} for ${platform}..."
    tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT
    
    curl -fsSL "$download_url" -o "${tmp_dir}/archive.${ext}" || error "Download failed"
    
    cd "$tmp_dir"
    tar xzf "archive.${ext}"
    
    mkdir -p "$INSTALL_DIR"
    
    local binary
    binary=$(find . -type f -name 'memory-cli*' ! -name '*.tar.gz' ! -name '*.zip' | head -1)
    [ -z "$binary" ] && error "Binary not found in archive"
    
    mv "$binary" "${INSTALL_DIR}/memory"
    chmod +x "${INSTALL_DIR}/memory"
    
    success "Installed to ${INSTALL_DIR}/memory"
    
    setup_path
    setup_completion
    
    echo
    "${INSTALL_DIR}/memory" version
    
    echo
    log "Run 'memory install' to set up a standalone server"
    log "Run 'memory upgrade --force' to update an existing installation"
    
    echo
    prompt_google_api_key
}

setup_path() {
    local path_line="export PATH=\"\$HOME/.memory/bin:\$PATH\""
    local added_to=""
    
    # Helper: try to append PATH config to a file
    # Returns 0 on success, 1 on failure (e.g., symlink to read-only location)
    try_append() {
        local file="$1"
        if [ -f "$file" ] && grep -q "\.memory/bin" "$file" 2>/dev/null; then
            return 0  # Already configured
        fi
        # Try writing; handle read-only symlinks (e.g., Mackup -> iCloud)
        if { echo "" >> "$file" && echo "# Memory CLI" >> "$file" && echo "$path_line" >> "$file"; } 2>/dev/null; then
            return 0
        fi
        return 1
    }
    
    # Zsh: try .zshrc first, fall back to .zshenv (not managed by Mackup, sourced by all zsh sessions)
    if command -v zsh &>/dev/null; then
        if [ -f "$HOME/.zshrc" ]; then
            if grep -q "\.memory/bin" "$HOME/.zshrc" 2>/dev/null; then
                added_to="${added_to} ~/.zshrc"
            elif try_append "$HOME/.zshrc"; then
                added_to="${added_to} ~/.zshrc"
            elif try_append "$HOME/.zshenv"; then
                added_to="${added_to} ~/.zshenv"
            fi
        elif try_append "$HOME/.zshenv"; then
            added_to="${added_to} ~/.zshenv"
        fi
    fi
    
    # Bash: try .bashrc first, fall back to .bash_profile, then .profile
    if [ -f "$HOME/.bashrc" ]; then
        if grep -q "\.memory/bin" "$HOME/.bashrc" 2>/dev/null; then
            added_to="${added_to} ~/.bashrc"
        elif try_append "$HOME/.bashrc"; then
            added_to="${added_to} ~/.bashrc"
        elif try_append "$HOME/.bash_profile"; then
            added_to="${added_to} ~/.bash_profile"
        fi
    elif try_append "$HOME/.bash_profile"; then
        added_to="${added_to} ~/.bash_profile"
    elif try_append "$HOME/.profile"; then
        added_to="${added_to} ~/.profile"
    fi
    
    if [ -n "$added_to" ]; then
        success "Added to PATH in:${added_to}"
        log "Restart your terminal or run 'source <config-file>' to activate"
    elif [[ ":$PATH:" == *":${INSTALL_DIR}:"* ]]; then
        success "PATH already configured"
    else
        echo
        echo "  Could not update shell config (files may be read-only symlinks)."
        echo "  Add to PATH manually: $path_line"
        echo "  Recommended: add the line above to ~/.zshenv or ~/.bash_profile"
    fi
}

setup_completion() {
    local added_to=""

    # Helper: try to append completion config to a file
    # Returns 0 on success or already configured, 1 on failure
    try_append_completion() {
        local file="$1"
        local line="$2"
        if [ -f "$file" ] && grep -q "memory completion" "$file" 2>/dev/null; then
            return 0  # Already configured
        fi
        if { echo "" >> "$file" && echo "# Memory CLI shell completion" >> "$file" && echo "$line" >> "$file"; } 2>/dev/null; then
            return 0
        fi
        return 1
    }

    # Zsh: source <(memory completion zsh)
    if command -v zsh &>/dev/null; then
        local zsh_line='source <(memory completion zsh)'
        if [ -f "$HOME/.zshrc" ]; then
            if grep -q "memory completion" "$HOME/.zshrc" 2>/dev/null; then
                added_to="${added_to} ~/.zshrc"
            elif try_append_completion "$HOME/.zshrc" "$zsh_line"; then
                added_to="${added_to} ~/.zshrc"
            elif try_append_completion "$HOME/.zshenv" "$zsh_line"; then
                added_to="${added_to} ~/.zshenv"
            fi
        elif try_append_completion "$HOME/.zshenv" "$zsh_line"; then
            added_to="${added_to} ~/.zshenv"
        fi
    fi

    # Bash: source <(memory completion bash)
    if command -v bash &>/dev/null; then
        local bash_line='source <(memory completion bash)'
        if [ -f "$HOME/.bashrc" ]; then
            if grep -q "memory completion" "$HOME/.bashrc" 2>/dev/null; then
                added_to="${added_to} ~/.bashrc"
            elif try_append_completion "$HOME/.bashrc" "$bash_line"; then
                added_to="${added_to} ~/.bashrc"
            elif try_append_completion "$HOME/.bash_profile" "$bash_line"; then
                added_to="${added_to} ~/.bash_profile"
            fi
        elif try_append_completion "$HOME/.bash_profile" "$bash_line"; then
            added_to="${added_to} ~/.bash_profile"
        fi
    fi

    echo
    if [ -n "$added_to" ]; then
        success "Shell completion configured in:${added_to}"
    else
        log "Could not auto-configure shell completion."
        log "Add manually to your shell rc file:"
        log "  zsh:  source <(memory completion zsh)"
        log "  bash: source <(memory completion bash)"
    fi
}

prompt_google_api_key() {
    log "Google API Key Setup (optional)"
    echo
    echo "  A Google API key enables AI-powered features including:"
    echo "    - Semantic search with text embeddings"
    echo "    - AI-powered document analysis"
    echo "    - Intelligent entity extraction"
    echo
    echo "  To get a Google API key:"
    echo "    1. Go to https://aistudio.google.com/apikey"
    echo "    2. Click 'Create API Key'"
    echo "    3. Copy the generated key"
    echo
    echo -n "  Enter your Google API key (press Enter to skip): "
    # Skip interactive prompt in non-interactive environments (CI=true or non-TTY stdin)
    if [ "${CI:-false}" = "true" ] || [ ! -t 0 ]; then
        google_api_key=""
        echo "(skipped — non-interactive)"
    else
        read -r google_api_key
    fi
    
    if [ -n "$google_api_key" ]; then
        mkdir -p "${INSTALL_DIR%/bin}"
        local config_dir="${INSTALL_DIR%/bin}"
        
        # Save to env file if it exists, otherwise store for later use
        if [ -f "${config_dir}/config/.env.local" ]; then
            if grep -q "^GOOGLE_API_KEY=" "${config_dir}/config/.env.local" 2>/dev/null; then
                sed -i.bak "s|^GOOGLE_API_KEY=.*|GOOGLE_API_KEY=${google_api_key}|" "${config_dir}/config/.env.local"
                rm -f "${config_dir}/config/.env.local.bak"
            else
                echo "GOOGLE_API_KEY=${google_api_key}" >> "${config_dir}/config/.env.local"
            fi
            success "Google API key saved to configuration"
            log "Run 'emergent install' or restart services to apply"
        else
            success "Google API key noted"
            log "Pass it during server setup: memory install --google-api-key ${google_api_key}"
        fi
    else
        log "Skipped. You can set it later with: memory config set google_api_key YOUR_KEY"
        log "Or pass it during install: memory install --google-api-key YOUR_KEY"
    fi
}

main "$@"
