#!/usr/bin/env bash
set -euo pipefail

REPO_ORG="Emergent-Comapny"
REPO_NAME="emergent"
REPO_BRANCH="${EMERGENT_VERSION:-main}"
BASE_URL="https://raw.githubusercontent.com/${REPO_ORG}/${REPO_NAME}/${REPO_BRANCH}/deploy/minimal"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.emergent}"
SERVER_PORT="${SERVER_PORT:-3002}"

BOLD='\033[1m'
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${BOLD}Emergent Standalone Installer${NC}"
echo "=============================="
echo ""

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

echo -e "${CYAN}Installing to: ${INSTALL_DIR}${NC}"
mkdir -p "${INSTALL_DIR}/bin"
mkdir -p "${INSTALL_DIR}/config"
mkdir -p "${INSTALL_DIR}/build"

echo -e "${CYAN}Downloading files...${NC}"

BUILD_FILES=(
    "docker-compose.local.yml"
    "Dockerfile.server-with-cli"
    "entrypoint.sh"
    "emergent-cli-wrapper.sh"
    "init.sql"
)

BIN_FILES=(
    "emergent-ctl.sh:emergent-ctl"
    "emergent-auth.sh:emergent-auth"
)

for file in "${BUILD_FILES[@]}"; do
    download_file "${BASE_URL}/${file}" "${INSTALL_DIR}/build/${file}" || {
        echo -e "${RED}Failed to download ${file}${NC}"
        exit 1
    }
done

for file_map in "${BIN_FILES[@]}"; do
    src="${file_map%%:*}"
    dest="${file_map##*:}"
    download_file "${BASE_URL}/${src}" "${INSTALL_DIR}/bin/${dest}" || {
        echo -e "${RED}Failed to download ${src}${NC}"
        exit 1
    }
    chmod +x "${INSTALL_DIR}/bin/${dest}"
done

echo -e "${GREEN}✓${NC} Files downloaded"

echo ""
echo -e "${CYAN}Downloading source code for build...${NC}"
CLONE_DIR="${INSTALL_DIR}/build/src"
if [ -d "${CLONE_DIR}" ]; then
    rm -rf "${CLONE_DIR}"
fi

git clone --depth 1 --filter=blob:none --sparse \
    "https://github.com/${REPO_ORG}/${REPO_NAME}.git" \
    "${CLONE_DIR}" 2>/dev/null

cd "${CLONE_DIR}"
git sparse-checkout set apps/server-go tools/emergent-cli deploy/minimal 2>/dev/null
echo -e "${GREEN}✓${NC} Source code ready"

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
echo -e "${CYAN}Building Docker image (this may take 2-3 minutes)...${NC}"
cd "${CLONE_DIR}"
docker build -f deploy/minimal/Dockerfile.server-with-cli -t emergent-server-with-cli:latest . 2>&1 | tail -3

echo ""
echo -e "${CYAN}Setting up docker compose...${NC}"
mkdir -p "${INSTALL_DIR}/docker"
cp "${CLONE_DIR}/deploy/minimal/docker-compose.local.yml" "${INSTALL_DIR}/docker/docker-compose.yml"
cp "${CLONE_DIR}/deploy/minimal/init.sql" "${INSTALL_DIR}/docker/"
echo -e "${GREEN}✓${NC} Docker compose ready"

echo ""
echo -e "${CYAN}Starting services...${NC}"
cd "${INSTALL_DIR}/docker"
docker compose -f docker-compose.yml --env-file "${INSTALL_DIR}/config/.env.local" up -d

echo ""
echo -e "${CYAN}Waiting for services to become healthy...${NC}"
sleep 5

MAX_WAIT=90
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
    echo -e "${YELLOW}Server health check timeout after ${MAX_WAIT}s.${NC}"
    echo "Checking logs..."
    docker compose -f "${INSTALL_DIR}/docker/docker-compose.yml" --env-file "${INSTALL_DIR}/config/.env.local" logs server --tail 20
    echo ""
    echo "Installation may still be starting. Check with:"
    echo "  ${INSTALL_DIR}/bin/emergent-ctl logs -f"
else
    echo ""
    echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}${BOLD}  ✓ Emergent Installation Complete!${NC}"
    echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo -e "${BOLD}Server:${NC}"
    echo "  URL: http://localhost:${SERVER_PORT}"
    echo "  API Key: ${API_KEY}"
    echo ""
    echo -e "${BOLD}Quick Commands:${NC}"
    echo "  docker exec emergent-server emergent projects list"
    echo "  docker exec emergent-server emergent status"
    echo ""
    echo -e "${BOLD}Manage Services:${NC}"
    echo "  ${INSTALL_DIR}/bin/emergent-ctl status"
    echo "  ${INSTALL_DIR}/bin/emergent-ctl logs -f"
    echo "  ${INSTALL_DIR}/bin/emergent-ctl restart"
    echo ""
    echo -e "${YELLOW}${BOLD}Enable Embeddings:${NC}"
    echo "  Run: ${INSTALL_DIR}/bin/emergent-ctl auth"
    echo "  This will set up Google Cloud API for embeddings."
    echo ""
fi

cat > "${INSTALL_DIR}/config/credentials.txt" <<EOF
Emergent Standalone - Credentials
Generated: $(date)

Server URL: http://localhost:${SERVER_PORT}
API Key: ${API_KEY}

PostgreSQL:
  Host: localhost:15432
  User: emergent
  Password: ${POSTGRES_PASSWORD}
  Database: emergent

Installation Directory: ${INSTALL_DIR}

Quick Commands:
  docker exec emergent-server emergent projects list
  docker exec emergent-server emergent status

Management:
  ${INSTALL_DIR}/bin/emergent-ctl status
  ${INSTALL_DIR}/bin/emergent-ctl logs -f
  ${INSTALL_DIR}/bin/emergent-ctl restart
  ${INSTALL_DIR}/bin/emergent-ctl auth    # Set up Google Cloud for embeddings

Uninstall:
  curl -fsSL https://raw.githubusercontent.com/${REPO_ORG}/${REPO_NAME}/main/deploy/minimal/uninstall.sh | bash
EOF

echo -e "${CYAN}Credentials saved to: ${INSTALL_DIR}/config/credentials.txt${NC}"
echo ""

echo -e "${CYAN}Cleaning up build files...${NC}"
rm -rf "${INSTALL_DIR}/build"
echo -e "${GREEN}✓${NC} Cleanup complete"
echo ""
