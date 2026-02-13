#!/usr/bin/env bash
set -euo pipefail

REPO_ORG="emergent-company"
REPO_NAME="emergent"
REPO_BRANCH="${EMERGENT_VERSION:-main}"
CLI_VERSION="${CLI_VERSION:-latest}"
BASE_URL="https://raw.githubusercontent.com/${REPO_ORG}/${REPO_NAME}/${REPO_BRANCH}/deploy/minimal"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.emergent}"
SERVER_PORT="${SERVER_PORT:-3002}"
IMAGE_ORG=$(echo "$REPO_ORG" | tr '[:upper:]' '[:lower:]')

# Determine image tag from version (use version tag if available, otherwise latest)
get_image_tag() {
    local version="$1"
    if [ -z "$version" ] || [ "$version" = "latest" ] || [ "$version" = "main" ]; then
        echo "latest"
    else
        # Strip 'v' prefix if present for image tag (images use 0.3.8 not v0.3.8)
        echo "${version#v}"
    fi
}

# Will be set after determining CLI_VERSION
SERVER_IMAGE_BASE="ghcr.io/${IMAGE_ORG}/emergent-server-with-cli"

BOLD='\033[1m'
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${BOLD}Emergent Standalone Installer${NC}"
echo "=============================="
echo ""

generate_secret() {
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex 32
    else
        head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
    fi
}

download_file() {
    local url="$1"
    local dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url" -o "$dest"
    elif command -v wget >/dev/null 2>&1; then
        wget -q "$url" -O "$dest"
    else
        echo -e "${RED}Error: curl or wget required${NC}"
        exit 1
    fi
}

detect_os() {
    local os
    case "$(uname -s)" in
        Linux*)     os=linux;;
        Darwin*)    os=darwin;;
        CYGWIN*|MINGW*|MSYS_NT*) os=windows;;
        FreeBSD*)   os=freebsd;;
        *)
            echo -e "${RED}Unsupported OS: $(uname -s)${NC}"
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
            echo -e "${RED}Unsupported architecture: $(uname -m)${NC}"
            exit 1
            ;;
    esac
    echo "$arch"
}

get_latest_cli_version() {
    local version
    if command -v curl >/dev/null 2>&1; then
        version=$(curl -fsSL "https://api.github.com/repos/${REPO_ORG}/${REPO_NAME}/releases/latest" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || echo "")
    elif command -v wget >/dev/null 2>&1; then
        version=$(wget -qO- "https://api.github.com/repos/${REPO_ORG}/${REPO_NAME}/releases/latest" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || echo "")
    fi
    echo "$version"
}

install_cli() {
    local os="$1"
    local arch="$2"
    local version="$3"
    
    if [ -z "$version" ]; then
        echo -e "${YELLOW}⚠${NC} CLI release not found, skipping CLI installation"
        echo "  You can still use: docker exec emergent-server emergent <command>"
        return 1
    fi
    
    local ext="tar.gz"
    [ "$os" = "windows" ] && ext="zip"
    
    local download_url="https://github.com/${REPO_ORG}/${REPO_NAME}/releases/download/${version}/emergent-cli-${os}-${arch}.${ext}"
    local tmp_dir=$(mktemp -d)
    
    echo -e "${CYAN}Downloading CLI ${version} for ${os}/${arch}...${NC}"
    
    if ! download_file "$download_url" "${tmp_dir}/emergent-cli.${ext}" 2>/dev/null; then
        echo -e "${YELLOW}⚠${NC} Failed to download CLI binary"
        echo "  You can still use: docker exec emergent-server emergent <command>"
        rm -rf "$tmp_dir"
        return 1
    fi
    
    if [ "$ext" = "zip" ]; then
        unzip -q "${tmp_dir}/emergent-cli.${ext}" -d "$tmp_dir" 2>/dev/null || { rm -rf "$tmp_dir"; return 1; }
    else
        tar xzf "${tmp_dir}/emergent-cli.${ext}" -C "$tmp_dir" 2>/dev/null || { rm -rf "$tmp_dir"; return 1; }
    fi
    
    local binary_name="emergent-cli-${os}-${arch}"
    [ "$os" = "windows" ] && binary_name="${binary_name}.exe"
    
    if [ -f "${tmp_dir}/${binary_name}" ]; then
        mv "${tmp_dir}/${binary_name}" "${INSTALL_DIR}/bin/emergent"
        chmod +x "${INSTALL_DIR}/bin/emergent"
        echo -e "${GREEN}✓${NC} CLI installed to ${INSTALL_DIR}/bin/emergent"
    else
        echo -e "${YELLOW}⚠${NC} CLI binary not found in archive (expected ${binary_name})"
        rm -rf "$tmp_dir"
        return 1
    fi
    
    rm -rf "$tmp_dir"
    return 0
}

setup_path() {
    local path_line="export PATH=\"\$HOME/.emergent/bin:\$PATH\""
    local added_to=""
    
    # Add to .bashrc if it exists
    if [ -f "$HOME/.bashrc" ]; then
        if ! grep -q "\.emergent/bin" "$HOME/.bashrc" 2>/dev/null; then
            echo "" >> "$HOME/.bashrc"
            echo "# Emergent CLI" >> "$HOME/.bashrc"
            echo "$path_line" >> "$HOME/.bashrc"
            added_to="${added_to} ~/.bashrc"
        fi
    fi
    
    # Add to .zshrc if it exists
    if [ -f "$HOME/.zshrc" ]; then
        if ! grep -q "\.emergent/bin" "$HOME/.zshrc" 2>/dev/null; then
            echo "" >> "$HOME/.zshrc"
            echo "# Emergent CLI" >> "$HOME/.zshrc"
            echo "$path_line" >> "$HOME/.zshrc"
            added_to="${added_to} ~/.zshrc"
        fi
    fi
    
    # If neither exists, try .bash_profile or .profile
    if [ -z "$added_to" ]; then
        if [ -f "$HOME/.bash_profile" ]; then
            if ! grep -q "\.emergent/bin" "$HOME/.bash_profile" 2>/dev/null; then
                echo "" >> "$HOME/.bash_profile"
                echo "# Emergent CLI" >> "$HOME/.bash_profile"
                echo "$path_line" >> "$HOME/.bash_profile"
                added_to=" ~/.bash_profile"
            fi
        elif [ -f "$HOME/.profile" ]; then
            if ! grep -q "\.emergent/bin" "$HOME/.profile" 2>/dev/null; then
                echo "" >> "$HOME/.profile"
                echo "# Emergent CLI" >> "$HOME/.profile"
                echo "$path_line" >> "$HOME/.profile"
                added_to=" ~/.profile"
            fi
        fi
    fi
    
    if [ -n "$added_to" ]; then
        echo -e "${GREEN}✓${NC} Added to PATH in:${added_to}"
        echo "  Restart your terminal or run 'source <config-file>' to activate"
    elif echo "$PATH" | grep -q "\.emergent/bin"; then
        echo -e "${GREEN}✓${NC} PATH already configured"
    else
        echo -e "${YELLOW}⚠${NC} Could not detect shell config file"
        echo "  Add this to your shell config manually: $path_line"
        return 1
    fi
    
    return 0
}

prompt_google_api_key() {
    echo ""
    echo -e "${CYAN}${BOLD}Google API Key Setup (optional)${NC}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "A Google API key enables AI-powered features including:"
    echo "  - Semantic search with text embeddings"
    echo "  - AI-powered document analysis"
    echo "  - Intelligent entity extraction"
    echo ""
    echo "To get a Google API key:"
    echo "  1. Go to https://aistudio.google.com/apikey"
    echo "  2. Click 'Create API Key'"
    echo "  3. Copy the generated key"
    echo ""
    echo -n "Enter your Google API key (press Enter to skip): "
    read -r google_api_key
    
    if [ -n "$google_api_key" ]; then
        # Update the .env.local file with the Google API key
        if [ -f "${INSTALL_DIR}/config/.env.local" ]; then
            if grep -q "^GOOGLE_API_KEY=" "${INSTALL_DIR}/config/.env.local" 2>/dev/null; then
                sed -i.bak "s|^GOOGLE_API_KEY=.*|GOOGLE_API_KEY=${google_api_key}|" "${INSTALL_DIR}/config/.env.local"
                rm -f "${INSTALL_DIR}/config/.env.local.bak"
            else
                echo "GOOGLE_API_KEY=${google_api_key}" >> "${INSTALL_DIR}/config/.env.local"
            fi
            echo -e "${GREEN}✓${NC} Google API key saved to configuration"
            
            # Restart server container if it's running to pick up the key
            if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "emergent-server"; then
                echo -e "${CYAN}Restarting server to apply Google API key...${NC}"
                cd "${INSTALL_DIR}/docker"
                docker compose --env-file "${INSTALL_DIR}/config/.env.local" restart server 2>/dev/null || true
                echo -e "${GREEN}✓${NC} Server restarted with Google API key"
            fi
        fi
    else
        echo -e "${YELLOW}⚠${NC} Skipped. You can set it later:"
        echo "  emergent config set google_api_key YOUR_KEY"
        echo "  Or re-run: emergent install --google-api-key YOUR_KEY"
    fi
}

if [ "${CLIENT_ONLY:-}" = "true" ] || [ "${CLIENT_ONLY:-}" = "1" ]; then
    echo -e "${CYAN}Running in Client-Only Mode${NC}"
    echo "Skipping server and Docker checks..."
    
    echo -e "${CYAN}Installing CLI to: ${INSTALL_DIR}${NC}"
    mkdir -p "${INSTALL_DIR}/bin"
    
    HOST_OS=$(detect_os)
    HOST_ARCH=$(detect_arch)
    
    # Resolve "latest" to actual version tag
    if [ "$CLI_VERSION" = "latest" ]; then
        CLI_VERSION=$(get_latest_cli_version)
    fi
    
    if install_cli "$HOST_OS" "$HOST_ARCH" "$CLI_VERSION"; then
        setup_path
        echo ""
        echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
        echo -e "${GREEN}${BOLD}  ✓ Emergent CLI Installed!${NC}"
        echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
        echo ""
        echo -e "${CYAN}Next Steps:${NC}"
        echo ""
        echo "1. Connect to an Emergent server:"
        echo "   emergent login --server-url <SERVER_URL> --api-key <API_KEY>"
        echo ""
        echo "2. Configure MCP for your AI agent:"
        echo ""
        cat << 'CLIENTMCPEOF'
{
  "mcpServers": {
    "emergent": {
      "command": "/path/to/emergent",
      "args": ["mcp"]
    }
  }
}
CLIENTMCPEOF
        echo ""
        echo "   Replace /path/to/emergent with: ${INSTALL_DIR}/bin/emergent"
        echo ""
        echo -e "${YELLOW}Note:${NC} Restart your terminal for 'emergent' to be in PATH."
        echo ""
        prompt_google_api_key
        echo ""
        exit 0
    else
        echo -e "${RED}Failed to install CLI${NC}"
        exit 1
    fi
fi

if ! command -v docker >/dev/null 2>&1; then
    echo -e "${RED}Error: Docker is required but not installed.${NC}"
    echo "Install: https://docs.docker.com/get-docker/"
    exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
    echo -e "${RED}Error: Docker Compose v2 is required but not installed.${NC}"
    echo "Install: https://docs.docker.com/compose/install/"
    exit 1
fi

# Detect existing installation for upgrade mode
UPGRADE_MODE=false
if [ -f "${INSTALL_DIR}/docker/docker-compose.yml" ]; then
    UPGRADE_MODE=true
    echo -e "${CYAN}Existing installation detected at ${INSTALL_DIR}${NC}"
    echo -e "${BOLD}Running in UPGRADE mode${NC}"
    echo ""
    
    CURRENT_VERSION=$(cat "${INSTALL_DIR}/version" 2>/dev/null || echo "unknown")
    echo -e "${CYAN}Current version: ${CURRENT_VERSION}${NC}"
    
    echo -e "${CYAN}Determining latest version...${NC}"
    if [ "$CLI_VERSION" = "latest" ]; then
        CLI_VERSION=$(get_latest_cli_version)
    fi
    IMAGE_TAG=$(get_image_tag "$CLI_VERSION")
    SERVER_IMAGE="${SERVER_IMAGE_BASE}:${IMAGE_TAG}"
    echo -e "${GREEN}✓${NC} Target version: ${CLI_VERSION} (image tag: ${IMAGE_TAG})"
    
    echo -e "${CYAN}Updating docker-compose.yml with versioned image...${NC}"
    sed -i.bak "s|image: ghcr.io/.*/emergent-server-with-cli:.*|image: ${SERVER_IMAGE}|g" "${INSTALL_DIR}/docker/docker-compose.yml"
    rm -f "${INSTALL_DIR}/docker/docker-compose.yml.bak"
    
    echo -e "${CYAN}Pulling Docker images...${NC}"
    cd "${INSTALL_DIR}/docker"
    docker compose --env-file "${INSTALL_DIR}/config/.env.local" pull
    echo -e "${GREEN}✓${NC} Images updated"
    
    echo -e "${CYAN}Restarting services...${NC}"
    docker compose --env-file "${INSTALL_DIR}/config/.env.local" up -d
    echo -e "${GREEN}✓${NC} Services restarted"
    
    echo -e "${CYAN}Waiting for services to become healthy...${NC}"
    sleep 5
    MAX_WAIT=60
    WAITED=0
    while [ $WAITED -lt $MAX_WAIT ]; do
        SERVER_PORT_VAL=$(grep "^SERVER_PORT=" "${INSTALL_DIR}/config/.env.local" 2>/dev/null | cut -d'=' -f2 || echo "3002")
        SERVER_PORT_VAL="${SERVER_PORT_VAL:-3002}"
        if curl -sf "http://localhost:${SERVER_PORT_VAL}/health" > /dev/null 2>&1; then
            echo -e "${GREEN}✓${NC} Server is healthy!"
            break
        fi
        echo -n "."
        sleep 3
        WAITED=$((WAITED + 3))
    done
    echo ""
    
    echo -e "${CYAN}Updating CLI binary...${NC}"
    HOST_OS=$(detect_os)
    HOST_ARCH=$(detect_arch)
    
    if install_cli "$HOST_OS" "$HOST_ARCH" "$CLI_VERSION"; then
        echo -e "${GREEN}✓${NC} CLI updated to ${CLI_VERSION}"
    else
        echo -e "${YELLOW}⚠${NC} CLI update skipped (you can still use: docker exec emergent-server emergent <command>)"
    fi
    
    echo "${CLI_VERSION}" > "${INSTALL_DIR}/version"
    
    echo ""
    echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}${BOLD}  ✓ Emergent Upgrade Complete!${NC}"
    echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo -e "${CYAN}Upgraded components:${NC}"
    echo "  • Docker images (${IMAGE_TAG})"
    echo "  • Services (restarted)"
    echo "  • CLI binary (${CLI_VERSION})"
    echo ""
    echo -e "${CYAN}Your existing configuration and data were preserved.${NC}"
    echo ""
    exit 0
fi

# Fresh install mode continues below...

echo -e "${CYAN}Installing to: ${INSTALL_DIR}${NC}"
mkdir -p "${INSTALL_DIR}/bin"
mkdir -p "${INSTALL_DIR}/config"
mkdir -p "${INSTALL_DIR}/docker"

echo -e "${CYAN}Determining latest version...${NC}"
if [ "$CLI_VERSION" = "latest" ]; then
    CLI_VERSION=$(get_latest_cli_version)
fi
IMAGE_TAG=$(get_image_tag "$CLI_VERSION")
SERVER_IMAGE="${SERVER_IMAGE_BASE}:${IMAGE_TAG}"
echo -e "${GREEN}✓${NC} Version: ${CLI_VERSION:-latest} (image tag: ${IMAGE_TAG})"

echo -e "${CYAN}Downloading helper scripts...${NC}"
BIN_FILES=("emergent-ctl.sh:emergent-ctl" "emergent-auth.sh:emergent-auth")
for file_map in "${BIN_FILES[@]}"; do
    src="${file_map%%:*}"
    dest="${file_map##*:}"
    download_file "${BASE_URL}/${src}" "${INSTALL_DIR}/bin/${dest}" || {
        echo -e "${RED}Failed to download ${src}${NC}"
        exit 1
    }
    chmod +x "${INSTALL_DIR}/bin/${dest}"
done

echo -e "${CYAN}Downloading init.sql...${NC}"
download_file "${BASE_URL}/init.sql" "${INSTALL_DIR}/docker/init.sql" || {
    echo -e "${RED}Failed to download init.sql${NC}"
    exit 1
}

echo -e "${GREEN}✓${NC} Files downloaded"

echo ""
echo -e "${CYAN}Generating secure configuration...${NC}"
POSTGRES_PASSWORD=$(generate_secret)
MINIO_PASSWORD=$(generate_secret)
API_KEY=$(generate_secret)

cat > "${INSTALL_DIR}/config/.env.local" <<EOF
POSTGRES_USER=emergent
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_DB=emergent
POSTGRES_PORT=15432

MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=${MINIO_PASSWORD}
MINIO_API_PORT=19000

STANDALONE_MODE=true
STANDALONE_API_KEY=${API_KEY}
STANDALONE_USER_EMAIL=admin@localhost
STANDALONE_ORG_NAME=My Organization
STANDALONE_PROJECT_NAME=Default Project

KREUZBERG_PORT=18000
SERVER_PORT=${SERVER_PORT}

GOOGLE_API_KEY=${GOOGLE_API_KEY:-}
EMBEDDING_DIMENSION=768
KREUZBERG_LOG_LEVEL=info
EOF

echo -e "${GREEN}✓${NC} Configuration created"

echo ""
echo -e "${CYAN}Generating Docker Compose file...${NC}"
cat > "${INSTALL_DIR}/docker/docker-compose.yml" <<EOF
services:
  db:
    image: pgvector/pgvector:pg16
    container_name: emergent-db
    restart: unless-stopped
    environment:
      POSTGRES_USER: \${POSTGRES_USER:-emergent}
      POSTGRES_PASSWORD: \${POSTGRES_PASSWORD:-changeme}
      POSTGRES_DB: \${POSTGRES_DB:-emergent}
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./init.sql:/docker-entrypoint-initdb.d/00-init.sql:ro
    ports:
      - '\${POSTGRES_PORT:-5432}:5432'
    healthcheck:
      test: ['CMD-SHELL', 'pg_isready -U \${POSTGRES_USER:-emergent} -d \${POSTGRES_DB:-emergent}']
      interval: 5s
      timeout: 5s
      retries: 10
    networks:
      - emergent

  kreuzberg:
    image: goldziher/kreuzberg:latest
    container_name: emergent-kreuzberg
    restart: unless-stopped
    ports:
      - '\${KREUZBERG_PORT:-8000}:8000'
    environment:
      - LOG_LEVEL=\${KREUZBERG_LOG_LEVEL:-info}
    healthcheck:
      test: ['CMD', 'curl', '-f', 'http://localhost:8000/health']
      interval: 30s
      timeout: 10s
      retries: 3
    deploy:
      resources:
        limits:
          memory: 2G
        reservations:
          memory: 512M
    networks:
      - emergent

  minio:
    image: minio/minio:latest
    container_name: emergent-minio
    restart: unless-stopped
    command: server /data --console-address ':9001'
    environment:
      MINIO_ROOT_USER: \${MINIO_ROOT_USER:-minioadmin}
      MINIO_ROOT_PASSWORD: \${MINIO_ROOT_PASSWORD:-changeme}
    ports:
      - '\${MINIO_API_PORT:-9000}:9000'
    volumes:
      - minio_data:/data
    healthcheck:
      test: ['CMD', 'curl', '-f', 'http://localhost:9000/minio/health/live']
      interval: 30s
      timeout: 10s
      retries: 3
    networks:
      - emergent

  minio-init:
    image: minio/mc:latest
    container_name: emergent-minio-init
    depends_on:
      minio:
        condition: service_healthy
    entrypoint: >
      /bin/sh -c "
      sleep 2;
      /usr/bin/mc alias set myminio http://minio:9000 \$\${MINIO_ROOT_USER:-minioadmin} \$\${MINIO_ROOT_PASSWORD:-changeme};
      /usr/bin/mc mb myminio/documents --ignore-existing;
      /usr/bin/mc mb myminio/document-temp --ignore-existing;
      echo 'MinIO buckets initialized';
      exit 0;
      "
    networks:
      - emergent

  server:
    image: ${SERVER_IMAGE}
    container_name: emergent-server
    restart: unless-stopped
    ports:
      - '\${SERVER_PORT:-3002}:3002'
    volumes:
      - emergent_cli_config:/root/.emergent
    environment:
      STANDALONE_MODE: 'true'
      STANDALONE_API_KEY: \${STANDALONE_API_KEY}
      STANDALONE_USER_EMAIL: \${STANDALONE_USER_EMAIL}
      STANDALONE_ORG_NAME: \${STANDALONE_ORG_NAME}
      STANDALONE_PROJECT_NAME: \${STANDALONE_PROJECT_NAME}
      POSTGRES_HOST: db
      POSTGRES_PORT: 5432
      POSTGRES_USER: \${POSTGRES_USER:-emergent}
      POSTGRES_PASSWORD: \${POSTGRES_PASSWORD:-changeme}
      POSTGRES_DB: \${POSTGRES_DB:-emergent}
      PORT: 3002
      GO_ENV: production
      KREUZBERG_SERVICE_URL: http://kreuzberg:8000
      KREUZBERG_ENABLED: 'true'
      STORAGE_PROVIDER: minio
      STORAGE_ENDPOINT: http://minio:9000
      STORAGE_ACCESS_KEY: \${MINIO_ROOT_USER:-minioadmin}
      STORAGE_SECRET_KEY: \${MINIO_ROOT_PASSWORD:-changeme}
      STORAGE_BUCKET_DOCUMENTS: documents
      STORAGE_BUCKET_TEMP: document-temp
      STORAGE_USE_SSL: 'false'
      GOOGLE_API_KEY: \${GOOGLE_API_KEY:-}
      EMBEDDING_DIMENSION: \${EMBEDDING_DIMENSION:-768}
      DB_AUTOINIT: 'true'
      SCOPES_DISABLED: 'true'
    depends_on:
      db:
        condition: service_healthy
      kreuzberg:
        condition: service_healthy
      minio:
        condition: service_healthy
    healthcheck:
      test: ['CMD', 'curl', '-sf', 'http://localhost:3002/health']
      interval: 30s
      timeout: 10s
      retries: 3
    networks:
      - emergent

volumes:
  postgres_data:
  minio_data:
  emergent_cli_config:

networks:
  emergent:
EOF

echo ""
echo -e "${CYAN}Starting services...${NC}"
cd "${INSTALL_DIR}/docker"
docker compose -f docker-compose.yml --env-file "${INSTALL_DIR}/config/.env.local" up -d

echo ""
echo -e "${CYAN}Waiting for services to become healthy...${NC}"
sleep 5

MAX_WAIT=120
WAITED=0
while [ $WAITED -lt $MAX_WAIT ]; do
    if curl -sf "http://localhost:${SERVER_PORT}/health" > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC} Server is healthy!"
        break
    fi
    echo -n "."
    sleep 5
    WAITED=$((WAITED + 5))
done
echo ""

if [ $WAITED -ge $MAX_WAIT ]; then
    echo -e "${YELLOW}Server health check timeout.${NC}"
    echo "Checking logs..."
    docker compose -f "${INSTALL_DIR}/docker/docker-compose.yml" --env-file "${INSTALL_DIR}/config/.env.local" logs server --tail 20
fi

echo ""
echo -e "${CYAN}Installing Emergent CLI...${NC}"
HOST_OS=$(detect_os)
HOST_ARCH=$(detect_arch)

if [ "$CLI_VERSION" = "latest" ]; then
    CLI_VERSION=$(get_latest_cli_version)
fi

# Always create CLI config (API key is known regardless of CLI binary install)
cat > "${INSTALL_DIR}/config.yaml" <<EOF
server_url: http://localhost:${SERVER_PORT}
api_key: ${API_KEY}
EOF
chmod 600 "${INSTALL_DIR}/config.yaml"
echo -e "${GREEN}✓${NC} CLI config created at ${INSTALL_DIR}/config.yaml"

CLI_INSTALLED=false
if install_cli "$HOST_OS" "$HOST_ARCH" "$CLI_VERSION"; then
    CLI_INSTALLED=true
    setup_path
fi

echo "${CLI_VERSION:-latest}" > "${INSTALL_DIR}/version"
echo -e "${GREEN}✓${NC} Version recorded: ${CLI_VERSION:-latest}"

echo ""
echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}${BOLD}  ✓ Emergent Installation Complete!${NC}"
echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo "Server URL: http://localhost:${SERVER_PORT}"
echo "API Key: ${API_KEY}"
echo ""
echo -e "${CYAN}${BOLD}🔌 MCP Configuration${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "To connect your AI agent to Emergent, add this configuration:"
echo ""
echo -e "${BOLD}For Claude Desktop:${NC}"
echo "  File: ~/.config/claude/claude_desktop_config.json (macOS/Linux)"
echo "  File: %APPDATA%\\Claude\\claude_desktop_config.json (Windows)"
echo ""
echo -e "${BOLD}For Cline (VS Code):${NC}"
echo "  Settings → MCP Servers → Edit in settings.json"
echo ""
echo -e "${BOLD}Configuration (copy this JSON):${NC}"
echo ""
cat << MCPEOF
{
  "mcpServers": {
    "emergent": {
      "command": "${INSTALL_DIR}/bin/emergent",
      "args": ["mcp"],
      "env": {
        "EMERGENT_SERVER_URL": "http://localhost:${SERVER_PORT}",
        "EMERGENT_API_KEY": "${API_KEY}"
      }
    }
  }
}
MCPEOF
echo ""
echo -e "${YELLOW}Note:${NC} Restart your AI agent (Claude Desktop/VS Code) after adding this config."
echo ""
echo -e "${CYAN}Alternative: Use CLI login for simpler config${NC}"
echo ""
echo "  1. Run login command (restart terminal first):"
echo "     emergent login --server-url http://localhost:${SERVER_PORT} --api-key ${API_KEY}"
echo ""
echo "  2. Then use this simplified MCP config:"
echo ""
cat << 'SIMPLEEOF'
{
  "mcpServers": {
    "emergent": {
      "command": "emergent",
      "args": ["mcp"]
    }
  }
}
SIMPLEEOF
echo ""
echo -e "${CYAN}📚 Documentation:${NC} https://github.com/emergent-company/emergent"
echo ""

# Prompt for Google API key
prompt_google_api_key
echo ""
