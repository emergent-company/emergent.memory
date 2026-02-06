#!/usr/bin/env bash
#
# Emergent Standalone Uninstaller
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Emergent-Comapny/emergent/main/deploy/minimal/uninstall.sh | bash
#
# Or run locally:
#   ~/emergent-standalone/deploy/minimal/uninstall.sh
#

set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-$HOME/emergent-standalone}"

BOLD='\033[1m'
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${BOLD}Emergent Standalone Uninstaller${NC}"
echo "================================"
echo ""

if [ ! -d "${INSTALL_DIR}" ]; then
    echo -e "${YELLOW}Installation directory not found: ${INSTALL_DIR}${NC}"
    echo "Nothing to uninstall."
    exit 0
fi

cd "${INSTALL_DIR}/deploy/minimal" 2>/dev/null || {
    echo -e "${YELLOW}Deploy directory not found.${NC}"
    echo "Removing installation directory only..."
    rm -rf "${INSTALL_DIR}"
    echo -e "${GREEN}Done!${NC}"
    exit 0
}

echo -e "${CYAN}Stopping services...${NC}"
docker compose -f docker-compose.local.yml --env-file .env.local down 2>/dev/null || true

echo ""
echo -e "${YELLOW}Do you want to remove all data (database, files)?${NC}"
echo "  This will permanently delete all stored documents and database contents."
echo ""

if [ -t 0 ]; then
    read -p "Remove all data? (y/N): " -r REMOVE_DATA
else
    REMOVE_DATA="${REMOVE_DATA:-N}"
    echo "Non-interactive mode: REMOVE_DATA=${REMOVE_DATA}"
fi

if [[ "$REMOVE_DATA" =~ ^[Yy]$ ]]; then
    echo -e "${CYAN}Removing Docker volumes...${NC}"
    docker compose -f docker-compose.local.yml --env-file .env.local down -v 2>/dev/null || true
    
    docker volume rm minimal_postgres_data 2>/dev/null || true
    docker volume rm minimal_minio_data 2>/dev/null || true
    docker volume rm minimal_emergent_cli_config 2>/dev/null || true
    
    echo -e "${GREEN}Volumes removed.${NC}"
fi

echo ""

if [ -t 0 ]; then
    read -p "Remove Docker image (emergent-server-with-cli:latest)? (y/N): " -r REMOVE_IMAGE
else
    REMOVE_IMAGE="${REMOVE_IMAGE:-N}"
    echo "Non-interactive mode: REMOVE_IMAGE=${REMOVE_IMAGE}"
fi

if [[ "$REMOVE_IMAGE" =~ ^[Yy]$ ]]; then
    echo -e "${CYAN}Removing Docker image...${NC}"
    docker rmi emergent-server-with-cli:latest 2>/dev/null || true
    echo -e "${GREEN}Image removed.${NC}"
fi

echo ""
echo -e "${CYAN}Removing installation directory...${NC}"
rm -rf "${INSTALL_DIR}"

echo ""
echo -e "${GREEN}${BOLD}Emergent has been uninstalled.${NC}"
echo ""
echo "To reinstall, run:"
echo "  curl -fsSL https://raw.githubusercontent.com/Emergent-Comapny/emergent/main/deploy/minimal/install-online.sh | bash"
echo ""
