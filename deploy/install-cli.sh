#!/usr/bin/env bash
set -euo pipefail

REPO_ORG="emergent-company"
REPO_NAME="emergent.memory"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.memory}"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

detect_os() {
    case "$(uname -s)" in
        Linux*)              echo linux ;;
        Darwin*)             echo darwin ;;
        CYGWIN*|MINGW*|MSYS_NT*) echo windows ;;
        FreeBSD*)            echo freebsd ;;
        *) echo -e "${RED}Unsupported OS: $(uname -s)${NC}" >&2; exit 1 ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)   echo amd64 ;;
        aarch64|arm64)  echo arm64 ;;
        armv7l)         echo arm ;;
        i386|i686)      echo 386 ;;
        *) echo -e "${RED}Unsupported architecture: $(uname -m)${NC}" >&2; exit 1 ;;
    esac
}

fetch() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$@"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "$@"
    else
        echo -e "${RED}curl or wget is required${NC}" >&2; exit 1
    fi
}

if [ "$VERSION" = "latest" ]; then
    VERSION=$(fetch "https://api.github.com/repos/${REPO_ORG}/${REPO_NAME}/releases/latest" \
        | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
fi

OS=$(detect_os)
ARCH=$(detect_arch)
EXT="tar.gz"; [ "$OS" = "windows" ] && EXT="zip"

# Detect any previously installed version
PREV_VERSION=""
if [ -x "${INSTALL_DIR}/bin/memory" ]; then
    PREV_VERSION=$("${INSTALL_DIR}/bin/memory" --version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)
fi

if [ -n "$PREV_VERSION" ]; then
    if [ "$PREV_VERSION" = "$VERSION" ]; then
        echo -e "${CYAN}Reinstalling memory ${VERSION} (${OS}/${ARCH})...${NC}"
    else
        echo -e "${CYAN}Updating memory from ${PREV_VERSION} to ${VERSION} (${OS}/${ARCH})...${NC}"
    fi
else
    echo -e "${CYAN}Downloading memory ${VERSION} (${OS}/${ARCH})...${NC}"
fi

URL="https://github.com/${REPO_ORG}/${REPO_NAME}/releases/download/${VERSION}/memory-cli-${OS}-${ARCH}.${EXT}"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$URL" -o "${TMP}/memory-cli.${EXT}"
else
    wget -q "$URL" -O "${TMP}/memory-cli.${EXT}"
fi

if [ "$EXT" = "zip" ]; then
    unzip -q "${TMP}/memory-cli.${EXT}" -d "$TMP"
else
    tar xzf "${TMP}/memory-cli.${EXT}" -C "$TMP"
fi

mkdir -p "${INSTALL_DIR}/bin"
BINARY="memory-cli-${OS}-${ARCH}"; [ "$OS" = "windows" ] && BINARY="${BINARY}.exe"
mv "${TMP}/${BINARY}" "${INSTALL_DIR}/bin/memory"
chmod +x "${INSTALL_DIR}/bin/memory"
if [ -n "$PREV_VERSION" ] && [ "$PREV_VERSION" != "$VERSION" ]; then
    echo -e "${GREEN}✓${NC} Updated to ${INSTALL_DIR}/bin/memory"
else
    echo -e "${GREEN}✓${NC} Installed to ${INSTALL_DIR}/bin/memory"
fi

PATH_LINE="export PATH=\"\$HOME/.memory/bin:\$PATH\""

# Pick the RC file that matches the running shell, falling back to common files
detect_rc() {
    local shell_name
    shell_name="$(basename "${SHELL:-}")"
    case "$shell_name" in
        zsh)   echo "$HOME/.zshrc" ;;
        bash)
            # On macOS, bash login shells use .bash_profile; prefer it if it exists
            if [ "$(uname -s)" = "Darwin" ] && [ -f "$HOME/.bash_profile" ]; then
                echo "$HOME/.bash_profile"
            else
                echo "$HOME/.bashrc"
            fi
            ;;
        fish)  echo "$HOME/.config/fish/config.fish" ;;
        *)
            # Fallback: first existing file wins
            for f in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.bash_profile" "$HOME/.profile"; do
                [ -f "$f" ] && echo "$f" && return
            done
            echo "$HOME/.profile"
            ;;
    esac
}

RC=$(detect_rc)
if ! grep -q '\.memory/bin' "$RC" 2>/dev/null; then
    printf '\n# Memory CLI\n%s\n' "$PATH_LINE" >> "$RC"
    echo -e "${GREEN}✓${NC} Added to PATH in ${RC}"
fi

# Make the binary available in the current session immediately
export PATH="$HOME/.memory/bin:$PATH"

# Install shell completions
SHELL_NAME="$(basename "${SHELL:-}")"
case "$SHELL_NAME" in
    zsh)
        COMP_LINE='source <(memory completion zsh)'
        if ! grep -q 'memory completion' "$RC" 2>/dev/null; then
            printf '\n# Memory CLI completions\n%s\n' "$COMP_LINE" >> "$RC"
            echo -e "${GREEN}✓${NC} Shell completions added to ${RC}"
        fi
        ;;
    bash)
        COMP_LINE='source <(memory completion bash)'
        if ! grep -q 'memory completion' "$RC" 2>/dev/null; then
            printf '\n# Memory CLI completions\n%s\n' "$COMP_LINE" >> "$RC"
            echo -e "${GREEN}✓${NC} Shell completions added to ${RC}"
        fi
        ;;
    fish)
        FISH_COMP_DIR="$HOME/.config/fish/completions"
        mkdir -p "$FISH_COMP_DIR"
        "$INSTALL_DIR/bin/memory" completion fish > "$FISH_COMP_DIR/memory.fish"
        echo -e "${GREEN}✓${NC} Shell completions installed to ${FISH_COMP_DIR}/memory.fish"
        ;;
esac

echo ""
echo -e "${BOLD}Run:${NC} memory --help"
echo -e "${YELLOW}Note:${NC} To apply to future sessions: source ${RC}"
