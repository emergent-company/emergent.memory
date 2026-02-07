#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${SCRIPT_DIR%/bin}"
CONFIG_DIR="$INSTALL_DIR/config"
ENV_FILE="$CONFIG_DIR/.env.local"

find_compose_file() {
    if [ -f "$INSTALL_DIR/build/src/deploy/minimal/docker-compose.local.yml" ]; then
        echo "$INSTALL_DIR/build/src/deploy/minimal/docker-compose.local.yml"
    elif [ -f "$INSTALL_DIR/docker/docker-compose.yml" ]; then
        echo "$INSTALL_DIR/docker/docker-compose.yml"
    else
        for f in "$HOME"/.emergent/*/deploy/minimal/docker-compose.local.yml; do
            if [ -f "$f" ]; then
                echo "$f"
                return
            fi
        done
        echo ""
    fi
}

COMPOSE_FILE=$(find_compose_file)
COMPOSE_DIR=$(dirname "$COMPOSE_FILE" 2>/dev/null || echo "")

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_usage() {
    echo "Usage: emergent-ctl <command> [options]"
    echo ""
    echo "Commands:"
    echo "  start       Start all services"
    echo "  stop        Stop all services"
    echo "  restart     Restart all services"
    echo "  status      Show service status"
    echo "  logs        Show logs (add -f for follow, service name for specific)"
    echo "  cli         Run emergent CLI command"
    echo "  health      Check server health"
    echo "  shell       Open shell in server container"
    echo "  rebuild     Rebuild and restart services"
    echo "  uninstall   Run uninstall script"
    echo "  auth        Set up Google Cloud authentication for embeddings"
    echo ""
    echo "Examples:"
    echo "  emergent-ctl start"
    echo "  emergent-ctl logs -f server"
    echo "  emergent-ctl cli projects list"
    echo "  emergent-ctl health"
}

check_requirements() {
    if ! command -v docker &> /dev/null; then
        echo -e "${RED}Error: Docker is not installed${NC}"
        exit 1
    fi
    if [ -z "$COMPOSE_FILE" ] || [ ! -f "$COMPOSE_FILE" ]; then
        echo -e "${RED}Error: docker-compose file not found${NC}"
        echo "Expected at: $INSTALL_DIR/build/src/deploy/minimal/docker-compose.local.yml"
        echo "or: $INSTALL_DIR/docker/docker-compose.yml"
        exit 1
    fi
}

compose_cmd() {
    if [ -f "$ENV_FILE" ]; then
        docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" "$@"
    else
        docker compose -f "$COMPOSE_FILE" "$@"
    fi
}

cmd_start() {
    echo -e "${BLUE}Starting Emergent services...${NC}"
    compose_cmd up -d
    echo -e "${GREEN}Services started${NC}"
}

cmd_stop() {
    echo -e "${BLUE}Stopping Emergent services...${NC}"
    compose_cmd down
    echo -e "${GREEN}Services stopped${NC}"
}

cmd_restart() {
    echo -e "${BLUE}Restarting Emergent services...${NC}"
    compose_cmd restart
    echo -e "${GREEN}Services restarted${NC}"
}

cmd_status() {
    echo -e "${BLUE}Emergent Service Status:${NC}"
    compose_cmd ps
}

cmd_logs() {
    compose_cmd logs "$@"
}

cmd_cli() {
    if [ $# -eq 0 ]; then
        echo -e "${YELLOW}Usage: emergent-ctl cli <command>${NC}"
        echo "Example: emergent-ctl cli projects list"
        exit 1
    fi
    docker exec emergent-server emergent "$@"
}

cmd_health() {
    SERVER_PORT="${SERVER_PORT:-3002}"
    if [ -f "$ENV_FILE" ]; then
        source "$ENV_FILE" 2>/dev/null || true
    fi
    
    echo -e "${BLUE}Checking server health...${NC}"
    if curl -s -f "http://localhost:${SERVER_PORT}/health" > /dev/null 2>&1; then
        HEALTH=$(curl -s "http://localhost:${SERVER_PORT}/health")
        echo -e "${GREEN}Server is healthy${NC}"
        echo "$HEALTH" | python3 -m json.tool 2>/dev/null || echo "$HEALTH"
    else
        echo -e "${RED}Server is not responding${NC}"
        exit 1
    fi
}

cmd_shell() {
    echo -e "${BLUE}Opening shell in server container...${NC}"
    docker exec -it emergent-server sh
}

cmd_rebuild() {
    echo -e "${BLUE}Rebuilding Emergent services...${NC}"
    compose_cmd up -d --build
    echo -e "${GREEN}Services rebuilt and started${NC}"
}

cmd_uninstall() {
    UNINSTALL_URL="https://raw.githubusercontent.com/Emergent-Comapny/emergent/main/deploy/minimal/uninstall.sh"
    echo -e "${BLUE}Running uninstaller...${NC}"
    curl -fsSL "$UNINSTALL_URL" | bash
}

cmd_auth() {
    if [ -f "$SCRIPT_DIR/emergent-auth" ]; then
        bash "$SCRIPT_DIR/emergent-auth"
    else
        echo -e "${RED}Auth script not found${NC}"
        exit 1
    fi
}

check_requirements

case "${1:-}" in
    start)
        cmd_start
        ;;
    stop)
        cmd_stop
        ;;
    restart)
        cmd_restart
        ;;
    status)
        cmd_status
        ;;
    logs)
        shift
        cmd_logs "$@"
        ;;
    cli)
        shift
        cmd_cli "$@"
        ;;
    health)
        cmd_health
        ;;
    shell)
        cmd_shell
        ;;
    rebuild)
        cmd_rebuild
        ;;
    uninstall)
        cmd_uninstall
        ;;
    auth)
        cmd_auth
        ;;
    -h|--help|help|"")
        print_usage
        ;;
    *)
        echo -e "${RED}Unknown command: $1${NC}"
        print_usage
        exit 1
        ;;
esac
