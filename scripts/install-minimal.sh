#!/usr/bin/env bash
#
# Emergent Minimal Installation Script
# =====================================
# One-line install: curl -fsSL https://install.emergent.ai/minimal | bash
#
# This script installs the Emergent minimal standalone deployment with:
# - Go Backend (API server)
# - PostgreSQL with pgvector
# - Kreuzberg (document extraction)
# - MinIO (S3 storage)
# - Tailscale (secure networking)

set -euo pipefail

VERSION="0.1.0"
INSTALL_DIR="${INSTALL_DIR:-$HOME/emergent-minimal}"
NON_INTERACTIVE="${NON_INTERACTIVE:-false}"

BOLD='\033[1m'
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

log() {
    echo -e "${CYAN}▶${NC} $*"
}

success() {
    echo -e "${GREEN}✓${NC} $*"
}

error() {
    echo -e "${RED}✗${NC} $*" >&2
}

warn() {
    echo -e "${YELLOW}⚠${NC} $*"
}

banner() {
    echo -e "${BOLD}${BLUE}"
    cat << "EOF"
 ___                                  _   
| __|_ __  ___ _ _ __ _ ___ _ _| |_ 
| _|| '  \/ -_) '_/ _` / -_) ' \  _|
|___|_|_|_\___|_| \__, \___|_||_\__|
                  |___/              
EOF
    echo -e "${NC}"
    echo -e "${BOLD}Emergent Minimal Installation${NC}"
    echo -e "Version: ${VERSION}"
    echo
}

check_prerequisites() {
    log "Checking prerequisites..."
    
    local missing=()
    
    if ! command -v docker &> /dev/null; then
        missing+=("docker")
    fi
    
    if ! command -v docker compose &> /dev/null; then
        missing+=("docker-compose")
    fi
    
    if ! command -v curl &> /dev/null; then
        missing+=("curl")
    fi
    
    if ! command -v openssl &> /dev/null; then
        missing+=("openssl")
    fi
    
    if [ ${#missing[@]} -ne 0 ]; then
        error "Missing required tools: ${missing[*]}"
        echo
        echo "Please install:"
        for tool in "${missing[@]}"; do
            case "$tool" in
                docker)
                    echo "  Docker: https://docs.docker.com/get-docker/"
                    ;;
                docker-compose)
                    echo "  Docker Compose: https://docs.docker.com/compose/install/"
                    ;;
                curl)
                    echo "  curl: apt install curl (Debian/Ubuntu) or yum install curl (CentOS/RHEL)"
                    ;;
                openssl)
                    echo "  openssl: apt install openssl (Debian/Ubuntu) or yum install openssl (CentOS/RHEL)"
                    ;;
            esac
        done
        exit 1
    fi
    
    docker_version=$(docker --version | grep -oP '\d+\.\d+' | head -1)
    if (( $(echo "$docker_version < 20.10" | bc -l) )); then
        warn "Docker version $docker_version is old. Recommended: 20.10+"
    fi
    
    success "All prerequisites met"
}

generate_secret() {
    openssl rand -hex 32
}

prompt_input() {
    local prompt="$1"
    local varname="$2"
    local default="${3:-}"
    local secret="${4:-false}"
    
    if [[ "$NON_INTERACTIVE" == "true" ]]; then
        if [[ -z "${!varname:-}" ]]; then
            error "Non-interactive mode requires $varname to be set"
            exit 1
        fi
        return
    fi
    
    local input_flags=""
    if [[ "$secret" == "true" ]]; then
        input_flags="-s"
    fi
    
    if [[ -n "$default" ]]; then
        read -r $input_flags -p "$(echo -e "${CYAN}?${NC} $prompt [${default}]: ")" input
        eval "$varname=\"\${input:-$default}\""
    else
        while true; do
            read -r $input_flags -p "$(echo -e "${CYAN}?${NC} $prompt: ")" input
            if [[ -n "$input" ]]; then
                eval "$varname=\"$input\""
                break
            else
                warn "This field is required"
            fi
        done
    fi
    
    if [[ "$secret" == "true" ]]; then
        echo
    fi
}

validate_tailscale_key() {
    local key="$1"
    if [[ ! "$key" =~ ^tskey- ]]; then
        error "Invalid Tailscale auth key (should start with 'tskey-')"
        return 1
    fi
    return 0
}

validate_google_api_key() {
    local key="$1"
    if [[ ! "$key" =~ ^AIza ]]; then
        warn "Google API key doesn't look standard (should start with 'AIza')"
        echo -n "Continue anyway? (y/N): "
        read -r response
        if [[ ! "$response" =~ ^[Yy]$ ]]; then
            return 1
        fi
    fi
    return 0
}

collect_configuration() {
    log "Collecting configuration..."
    echo
    
    echo -e "${BOLD}Required Configuration${NC}"
    echo
    
    prompt_input "Tailscale auth key (from https://login.tailscale.com/admin/settings/keys)" \
        TS_AUTHKEY "" true
    
    while ! validate_tailscale_key "$TS_AUTHKEY"; do
        prompt_input "Tailscale auth key" TS_AUTHKEY "" true
    done
    
    prompt_input "Google Cloud API key" GOOGLE_API_KEY "" true
    
    while ! validate_google_api_key "$GOOGLE_API_KEY"; do
        prompt_input "Google Cloud API key" GOOGLE_API_KEY "" true
    done
    
    echo
    echo -e "${BOLD}Optional Configuration${NC}"
    echo
    
    prompt_input "Installation directory" INSTALL_DIR "$HOME/emergent-minimal"
    prompt_input "Tailscale hostname" TAILSCALE_HOSTNAME "emergent"
    prompt_input "Standalone user email" STANDALONE_USER_EMAIL "admin@localhost"
    
    log "Generating secure random secrets..."
    POSTGRES_PASSWORD=$(generate_secret)
    MINIO_ROOT_PASSWORD=$(generate_secret)
    STANDALONE_API_KEY=$(generate_secret)
    
    success "Configuration collected"
}

create_installation_directory() {
    log "Creating installation directory: $INSTALL_DIR"
    
    if [[ -d "$INSTALL_DIR" ]]; then
        warn "Directory already exists: $INSTALL_DIR"
        if [[ "$NON_INTERACTIVE" != "true" ]]; then
            echo -n "Remove existing installation? (y/N): "
            read -r response
            if [[ "$response" =~ ^[Yy]$ ]]; then
                rm -rf "$INSTALL_DIR"
                success "Removed existing installation"
            else
                error "Installation cancelled"
                exit 1
            fi
        else
            error "Directory exists, cannot proceed in non-interactive mode"
            exit 1
        fi
    fi
    
    mkdir -p "$INSTALL_DIR"
    mkdir -p "$INSTALL_DIR/logs/server"
    mkdir -p "$INSTALL_DIR/logs/postgres"
    
    success "Created installation directory"
}

download_files() {
    log "Downloading configuration files..."
    
    local base_url="https://raw.githubusercontent.com/emergent-company/emergent/master/deploy/minimal"
    
    curl -fsSL "${base_url}/docker-compose.yml" -o "$INSTALL_DIR/docker-compose.yml"
    success "Downloaded docker-compose.yml"
}

create_env_file() {
    log "Creating environment configuration..."
    
    cat > "$INSTALL_DIR/.env" << EOF
# Emergent Minimal Deployment - Auto-generated Configuration
# Generated: $(date -u +"%Y-%m-%d %H:%M:%S UTC")

# Database
POSTGRES_USER=emergent
POSTGRES_DB=emergent
POSTGRES_PORT=5432
POSTGRES_PASSWORD=$POSTGRES_PASSWORD

# MinIO Storage
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=$MINIO_ROOT_PASSWORD
MINIO_API_PORT=9000
MINIO_CONSOLE_PORT=9001

# Standalone Configuration
STANDALONE_API_KEY=$STANDALONE_API_KEY
STANDALONE_USER_EMAIL=$STANDALONE_USER_EMAIL
STANDALONE_ORG_NAME=Default Organization
STANDALONE_PROJECT_NAME=Default Project

# Google Cloud
GOOGLE_API_KEY=$GOOGLE_API_KEY
EMBEDDING_DIMENSION=768

# Tailscale
TS_AUTHKEY=$TS_AUTHKEY
TAILSCALE_HOSTNAME=$TAILSCALE_HOSTNAME

# Kreuzberg
KREUZBERG_PORT=8000
KREUZBERG_LOG_LEVEL=info

# Server
SERVER_PORT=3002
EOF

    chmod 600 "$INSTALL_DIR/.env"
    success "Created .env file"
}

pull_images() {
    log "Pulling Docker images (this may take a few minutes)..."
    
    cd "$INSTALL_DIR"
    
    if ! docker compose pull; then
        error "Failed to pull Docker images"
        exit 1
    fi
    
    success "Docker images pulled"
}

start_services() {
    log "Starting services..."
    
    cd "$INSTALL_DIR"
    
    if ! docker compose up -d; then
        error "Failed to start services"
        exit 1
    fi
    
    success "Services started"
}

wait_for_health() {
    local service="$1"
    local max_attempts="${2:-30}"
    local sleep_time="${3:-2}"
    
    log "Waiting for $service to be healthy..."
    
    local attempt=0
    while [ $attempt -lt $max_attempts ]; do
        if docker compose ps "$service" | grep -q "healthy"; then
            success "$service is healthy"
            return 0
        fi
        
        attempt=$((attempt + 1))
        echo -n "."
        sleep $sleep_time
    done
    
    echo
    error "$service failed to become healthy after $((max_attempts * sleep_time)) seconds"
    docker compose logs "$service"
    return 1
}

verify_deployment() {
    log "Verifying deployment health..."
    
    cd "$INSTALL_DIR"
    
    wait_for_health "db" 30 2
    wait_for_health "kreuzberg" 20 2
    wait_for_health "minio" 15 2
    wait_for_health "server" 30 2
    
    log "Testing server endpoint..."
    local server_health=$(curl -s http://localhost:3002/health || echo "failed")
    if [[ "$server_health" == "failed" ]]; then
        error "Server health check failed"
        return 1
    fi
    
    success "All services verified healthy"
}

display_success() {
    echo
    echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}${BOLD}  ✓ Emergent Minimal Installation Complete!${NC}"
    echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo
    echo -e "${BOLD}Installation Directory:${NC}"
    echo "  $INSTALL_DIR"
    echo
    echo -e "${BOLD}API Access:${NC}"
    echo "  Local:     http://localhost:3002"
    echo "  Tailscale: http://${TAILSCALE_HOSTNAME}:3002"
    echo
    echo -e "${BOLD}MCP Configuration:${NC}"
    echo "  Standalone API Key: ${STANDALONE_API_KEY:0:16}..."
    echo "  (Full key in: $INSTALL_DIR/.env)"
    echo
    echo -e "${BOLD}MinIO Console:${NC}"
    echo "  URL: http://localhost:9001"
    echo "  User: minioadmin"
    echo "  Password: (in $INSTALL_DIR/.env)"
    echo
    echo -e "${BOLD}Useful Commands:${NC}"
    echo "  cd $INSTALL_DIR"
    echo "  docker compose logs -f              # View logs"
    echo "  docker compose ps                   # Check status"
    echo "  docker compose restart server       # Restart server"
    echo "  docker compose down                 # Stop all services"
    echo
    echo -e "${BOLD}Next Steps:${NC}"
    echo "  1. Configure your MCP client with the API key above"
    echo "  2. Check Tailscale status: docker exec emergent-tailscale tailscale status"
    echo "  3. Access your instance via Tailscale from any device"
    echo
    echo -e "${BOLD}Documentation:${NC}"
    echo "  https://docs.emergent.ai/deployment/minimal"
    echo
}

cleanup_on_error() {
    error "Installation failed. Cleaning up..."
    
    if [[ -d "$INSTALL_DIR" ]]; then
        cd "$INSTALL_DIR"
        docker compose down -v 2>/dev/null || true
    fi
    
    echo "For support, please visit: https://github.com/emergent-company/emergent/issues"
}

main() {
    trap cleanup_on_error ERR
    
    banner
    check_prerequisites
    collect_configuration
    create_installation_directory
    download_files
    create_env_file
    pull_images
    start_services
    verify_deployment
    display_success
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
