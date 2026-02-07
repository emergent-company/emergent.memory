#!/usr/bin/env bash
#
# Emergent Standalone Installer
# ============================
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Emergent-Comapny/emergent/main/deploy/minimal/install-online.sh | bash
#
# This script installs the Emergent standalone stack:
# - Go Backend (API + CLI)
# - PostgreSQL + pgvector
# - MinIO (Object Storage)
# - Kreuzberg (Document Extraction)
#

set -euo pipefail

REPO_ORG="Emergent-Comapny"
REPO_NAME="emergent"
REPO_BRANCH="${EMERGENT_VERSION:-main}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/emergent-standalone}"
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

command -v docker >/dev/null 2>&1 || { echo -e "${RED}Error: docker is required.${NC}"; echo "Install: https://docs.docker.com/get-docker/"; exit 1; }
command -v git >/dev/null 2>&1 || { echo -e "${RED}Error: git is required.${NC}"; exit 1; }

if ! docker compose version >/dev/null 2>&1; then
    echo -e "${RED}Error: docker compose v2 is required.${NC}"
    echo "Install: https://docs.docker.com/compose/install/"
    exit 1
fi

echo -e "${GREEN}✓${NC} Docker: $(docker --version | cut -d' ' -f3 | tr -d ',')"
echo -e "${GREEN}✓${NC} Docker Compose: $(docker compose version --short)"
echo ""

generate_secret() {
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex 32
    else
        head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
    fi
}

echo -e "${CYAN}Installing to: ${INSTALL_DIR}${NC}"
mkdir -p "${INSTALL_DIR}"
cd "${INSTALL_DIR}"

if [ -d ".git" ]; then
    echo -e "${CYAN}Updating existing installation...${NC}"
    git pull --quiet
else
    echo -e "${CYAN}Downloading Emergent...${NC}"
    git clone --depth 1 --branch "${REPO_BRANCH}" "https://github.com/${REPO_ORG}/${REPO_NAME}.git" . 2>/dev/null || {
        echo -e "${RED}Failed to clone repository.${NC}"
        echo "Check if https://github.com/${REPO_ORG}/${REPO_NAME} is accessible."
        exit 1
    }
fi

cd deploy/minimal

echo ""
echo -e "${CYAN}Generating secure configuration...${NC}"
POSTGRES_PASSWORD=$(generate_secret)
MINIO_PASSWORD=$(generate_secret)
API_KEY=$(generate_secret)

cat > .env.local <<EOF
POSTGRES_USER=emergent
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_DB=emergent
POSTGRES_PORT=15432

MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=${MINIO_PASSWORD}
MINIO_API_PORT=19000
MINIO_CONSOLE_PORT=19001

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

if [ -z "${GOOGLE_API_KEY:-}" ]; then
    echo ""
    echo -e "${YELLOW}Note:${NC} No GOOGLE_API_KEY provided. Embeddings will be disabled."
    echo "You can add it later: edit ${INSTALL_DIR}/deploy/minimal/.env.local"
fi

echo ""
echo -e "${CYAN}Building Docker image (this may take 2-3 minutes)...${NC}"
if [ -f "build-server-with-cli.sh" ]; then
    chmod +x build-server-with-cli.sh
    ./build-server-with-cli.sh 2>&1 | tail -5
else
    docker compose -f docker-compose.local.yml build 2>&1 | tail -5
fi

echo ""
echo -e "${CYAN}Starting services...${NC}"
docker compose -f docker-compose.local.yml --env-file .env.local up -d

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
    docker compose -f docker-compose.local.yml logs server --tail 20
    echo ""
    echo "Installation may still be starting. Check with:"
    echo "  cd ${INSTALL_DIR}/deploy/minimal && docker compose -f docker-compose.local.yml logs -f"
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
    echo -e "${BOLD}Manage Services (emergent-ctl):${NC}"
    echo "  ${INSTALL_DIR}/deploy/minimal/emergent-ctl.sh status"
    echo "  ${INSTALL_DIR}/deploy/minimal/emergent-ctl.sh logs -f"
    echo "  ${INSTALL_DIR}/deploy/minimal/emergent-ctl.sh restart"
    echo "  ${INSTALL_DIR}/deploy/minimal/emergent-ctl.sh health"
    echo ""
    echo -e "${BOLD}Uninstall:${NC}"
    echo "  curl -fsSL https://raw.githubusercontent.com/Emergent-Comapny/emergent/main/deploy/minimal/uninstall.sh | bash"
    echo ""
fi

cat > "${INSTALL_DIR}/deploy/minimal/credentials.txt" <<EOF
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

Management (emergent-ctl):
  ${INSTALL_DIR}/deploy/minimal/emergent-ctl.sh status
  ${INSTALL_DIR}/deploy/minimal/emergent-ctl.sh logs -f server
  ${INSTALL_DIR}/deploy/minimal/emergent-ctl.sh restart
  ${INSTALL_DIR}/deploy/minimal/emergent-ctl.sh health

Google Cloud Setup (for embeddings):
  ${INSTALL_DIR}/deploy/minimal/emergent-ctl.sh auth

Uninstall:
  curl -fsSL https://raw.githubusercontent.com/Emergent-Comapny/emergent/main/deploy/minimal/uninstall.sh | bash
EOF

echo -e "${CYAN}Credentials saved to: ${INSTALL_DIR}/deploy/minimal/credentials.txt${NC}"
echo ""
