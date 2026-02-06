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

# Configuration
REPO_ORG="Emergent-Comapny"
REPO_NAME="emergent"
REPO_BRANCH="main"
BASE_URL="https://raw.githubusercontent.com/${REPO_ORG}/${REPO_NAME}/${REPO_BRANCH}/deploy/minimal"

# Colors
BOLD='\033[1m'
GREEN='\033[0;32m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Default Install Directory
INSTALL_DIR="${HOME}/emergent-standalone"

echo -e "${BOLD}Emergent Standalone Installer${NC}"
echo "=============================="

# 1. Prerequisites Check
command -v docker >/dev/null 2>&1 || { echo -e "${RED}Error: docker is required.${NC}"; exit 1; }
command -v curl >/dev/null 2>&1 || { echo -e "${RED}Error: curl is required.${NC}"; exit 1; }

# 2. Setup Directory
echo -e "${CYAN}Installing to: ${INSTALL_DIR}${NC}"
mkdir -p "${INSTALL_DIR}"
cd "${INSTALL_DIR}"

# 3. Download Files
echo -e "${CYAN}Downloading configuration...${NC}"
curl -fsSL "${BASE_URL}/docker-compose.yml" -o docker-compose.yml || {
    echo -e "${RED}Failed to download docker-compose.yml from ${BASE_URL}${NC}"
    echo "Check if the repository is public and the URL is correct."
    exit 1
}

# 4. Generate Secrets
echo -e "${CYAN}Generating secure configuration...${NC}"
if [ ! -f .env ]; then
    generate_secret() { openssl rand -hex 32 2>/dev/null || echo "secret-$(date +%s)"; }
    
    POSTGRES_PASSWORD=$(generate_secret)
    MINIO_PASSWORD=$(generate_secret)
    API_KEY=$(generate_secret)
    
    # Prompt for Google API Key (Optional)
    read -p "Enter Google API Key (for embeddings) [leave empty to skip]: " GOOGLE_KEY
    GOOGLE_KEY=${GOOGLE_KEY:-""}

    cat > .env <<EOF
# Emergent Standalone Configuration
# Generated on $(date)

# Passwords
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
MINIO_ROOT_PASSWORD=${MINIO_PASSWORD}
STANDALONE_API_KEY=${API_KEY}

# AI Service
GOOGLE_API_KEY=${GOOGLE_KEY}
EMBEDDING_DIMENSION=768

# Network
SERVER_PORT=3002
POSTGRES_PORT=5432
MINIO_API_PORT=9000
MINIO_CONSOLE_PORT=9001
KREUZBERG_PORT=8000

# Defaults
POSTGRES_USER=emergent
POSTGRES_DB=emergent
MINIO_ROOT_USER=minioadmin
STANDALONE_MODE=true
EOF
    echo "Created .env file."
else
    echo ".env file already exists, skipping generation."
fi

# 5. Start Services
echo -e "${CYAN}Starting services...${NC}"
echo "Pulling images..."
docker compose pull

echo "Starting containers..."
docker compose up -d

# 6. Verification
echo -e "${CYAN}Waiting for services to be ready...${NC}"
sleep 10
if curl -s http://localhost:3002/health >/dev/null; then
    echo -e "${GREEN}Success! Emergent is running.${NC}"
    echo ""
    echo -e "${BOLD}Access Info:${NC}"
    echo "  API URL: http://localhost:3002"
    echo "  API Key: $(grep STANDALONE_API_KEY .env | cut -d= -f2)"
    echo ""
    echo "  MinIO Console: http://localhost:9001 (User: minioadmin, Pass: see .env)"
    echo ""
    echo "To stop services: cd ${INSTALL_DIR} && docker compose down"
else
    echo -e "${RED}Services started but health check failed.${NC}"
    echo "Check logs with: cd ${INSTALL_DIR} && docker compose logs -f"
fi
