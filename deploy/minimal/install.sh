#!/bin/bash
set -e

EMERGENT_VERSION="${EMERGENT_VERSION:-main}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/emergent-standalone}"
SERVER_PORT="${SERVER_PORT:-3002}"

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Emergent Standalone Installation"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

if ! command -v docker &> /dev/null; then
    echo "❌ Docker is not installed. Please install Docker first:"
    echo "   https://docs.docker.com/get-docker/"
    exit 1
fi

if ! docker compose version &> /dev/null; then
    echo "❌ Docker Compose is not installed or too old."
    echo "   Please install Docker Compose v2+:"
    echo "   https://docs.docker.com/compose/install/"
    exit 1
fi

echo "✅ Docker detected: $(docker --version)"
echo "✅ Docker Compose detected: $(docker compose version)"
echo ""

generate_password() {
    if command -v openssl &> /dev/null; then
        openssl rand -hex 32
    else
        cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 64 | head -n 1
    fi
}

echo "📂 Installation directory: $INSTALL_DIR"
mkdir -p "$INSTALL_DIR"
cd "$INSTALL_DIR"

if [ -d ".git" ]; then
    echo "📦 Updating existing installation..."
    git pull
else
    echo "📦 Downloading Emergent..."
    if command -v git &> /dev/null; then
        git clone --depth 1 --branch "$EMERGENT_VERSION" https://github.com/Emergent-Comapny/emergent.git .
    else
        echo "❌ Git is not installed. Please install git first."
        exit 1
    fi
fi

cd deploy/minimal

echo ""
echo "🔐 Generating secure passwords..."
POSTGRES_PASSWORD=$(generate_password)
MINIO_PASSWORD=$(generate_password)
API_KEY=$(generate_password)

echo "📝 Creating environment configuration..."
cat > .env.local <<EOF
POSTGRES_USER=emergent
POSTGRES_PASSWORD=$POSTGRES_PASSWORD
POSTGRES_DB=emergent
POSTGRES_PORT=15432

MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=$MINIO_PASSWORD
MINIO_API_PORT=19000
MINIO_CONSOLE_PORT=19001

STANDALONE_MODE=true
STANDALONE_API_KEY=$API_KEY
STANDALONE_USER_EMAIL=admin@localhost
STANDALONE_ORG_NAME=My Organization
STANDALONE_PROJECT_NAME=Default Project

KREUZBERG_PORT=18000
SERVER_PORT=$SERVER_PORT

GOOGLE_API_KEY=${GOOGLE_API_KEY:-}
EMBEDDING_DIMENSION=768

KREUZBERG_LOG_LEVEL=info
EOF

echo "✅ Configuration created"
echo ""

read -p "📋 Do you have a Google API key for embeddings? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    read -p "🔑 Enter your Google API key: " GOOGLE_KEY
    sed -i.bak "s|GOOGLE_API_KEY=.*|GOOGLE_API_KEY=$GOOGLE_KEY|" .env.local
    rm -f .env.local.bak
    echo "✅ Google API key configured"
else
    echo "⚠️  Skipping Google API key (embeddings will be disabled)"
    echo "   You can add it later by editing: $INSTALL_DIR/deploy/minimal/.env.local"
fi

echo ""
echo "🏗️  Building Docker image with embedded CLI..."
if [ -f "build-server-with-cli.sh" ]; then
    chmod +x build-server-with-cli.sh
    ./build-server-with-cli.sh
else
    echo "⚠️  Build script not found, using docker compose build..."
    docker compose -f docker-compose.local.yml build
fi

echo ""
echo "🚀 Starting services..."
docker compose -f docker-compose.local.yml --env-file .env.local up -d

echo ""
echo "⏳ Waiting for services to become healthy..."
sleep 5

MAX_WAIT=60
WAITED=0
while [ $WAITED -lt $MAX_WAIT ]; do
    if curl -sf http://localhost:$SERVER_PORT/health > /dev/null 2>&1; then
        echo "✅ Server is healthy!"
        break
    fi
    echo "   Waiting for server... ($WAITED/$MAX_WAIT seconds)"
    sleep 5
    WAITED=$((WAITED + 5))
done

if [ $WAITED -ge $MAX_WAIT ]; then
    echo "⚠️  Server health check timeout. Checking logs..."
    docker compose -f docker-compose.local.yml logs server
    echo ""
    echo "Installation may have issues. Please check the logs above."
else
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  ✅ Installation Complete!"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "📍 Installation directory: $INSTALL_DIR"
    echo "🌐 Server URL: http://localhost:$SERVER_PORT"
    echo "🔑 API Key: $API_KEY"
    echo ""
    echo "Quick Commands:"
    echo "  # List projects"
    echo "  docker exec emergent-server emergent-cli projects list"
    echo ""
    echo "  # Check status"
    echo "  docker exec emergent-server emergent-cli status"
    echo ""
    echo "  # View logs"
    echo "  docker compose -f docker-compose.local.yml logs -f"
    echo ""
    echo "  # Stop services"
    echo "  docker compose -f docker-compose.local.yml down"
    echo ""
    echo "  # Restart services"
    echo "  docker compose -f docker-compose.local.yml restart"
    echo ""
    echo "Configuration saved to:"
    echo "  $INSTALL_DIR/deploy/minimal/.env.local"
    echo ""
    echo "Documentation:"
    echo "  $INSTALL_DIR/deploy/minimal/INDEX.md"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
fi

cat > "$INSTALL_DIR/deploy/minimal/credentials.txt" <<EOF
Emergent Standalone - Credentials
Generated: $(date)

Server URL: http://localhost:$SERVER_PORT
API Key: $API_KEY

PostgreSQL:
  Host: localhost:15432
  User: emergent
  Password: $POSTGRES_PASSWORD
  Database: emergent

MinIO:
  Console: http://localhost:19001
  API: http://localhost:19000
  User: minioadmin
  Password: $MINIO_PASSWORD

Installation Directory: $INSTALL_DIR

Quick Start:
  docker exec emergent-server emergent-cli projects list
EOF

echo "🔐 Credentials saved to: $INSTALL_DIR/deploy/minimal/credentials.txt"
echo ""
