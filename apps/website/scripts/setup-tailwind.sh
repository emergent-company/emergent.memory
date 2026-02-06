#!/bin/bash

# Detect platform
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

# Map architecture names
case "$ARCH" in
    x86_64)
        ARCH="x64"
        ;;
    arm64|aarch64)
        ARCH="arm64"
        ;;
esac

# Map OS names
case "$OS" in
    darwin)
        OS="macos"
        ;;
    linux)
        OS="linux"
        ;;
    mingw*|msys*|cygwin*)
        OS="windows"
        ;;
esac

BINARY_NAME="tailwindcss-${OS}-${ARCH}"
if [ "$OS" = "windows" ]; then
    BINARY_NAME="${BINARY_NAME}.exe"
fi

DOWNLOAD_URL="https://github.com/tailwindlabs/tailwindcss/releases/latest/download/${BINARY_NAME}"

echo "Downloading Tailwind CSS standalone binary for ${OS}-${ARCH}..."
mkdir -p bin
curl -sLo bin/tailwindcss "$DOWNLOAD_URL"

chmod +x bin/tailwindcss

echo "✓ Tailwind CSS binary installed to bin/tailwindcss"
./bin/tailwindcss --help
