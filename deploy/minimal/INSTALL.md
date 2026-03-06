# Emergent One-Command Installation Guide

## The Simplest Way to Install Emergent

Copy and paste this single command into your terminal:

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/minimal/install.sh | bash
```

## What Happens During Installation

The installer automatically:

1. ✅ **Checks prerequisites** (Docker, Docker Compose)
2. ✅ **Downloads Emergent** (latest version)
3. ✅ **Generates secure passwords** (PostgreSQL, MinIO, API key)
4. ✅ **Creates configuration** (.env.local file)
5. ✅ **Builds Docker images** (server with embedded CLI)
6. ✅ **Starts all services** (API, database, storage, document processing)
7. ✅ **Verifies health** (waits for server to be ready)
8. ✅ **Saves credentials** (to credentials.txt)

**Total time: ~2-3 minutes**

## Installation Output

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Emergent Standalone Installation
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✅ Docker detected: Docker version 24.0.7
✅ Docker Compose detected: Docker Compose version v2.23.0

📂 Installation directory: /home/user/emergent-standalone
📦 Downloading Emergent...
🔐 Generating secure passwords...
📝 Creating environment configuration...
✅ Configuration created

📋 Do you have a Google API key for embeddings? (y/N): n
⚠️  Skipping Google API key (embeddings will be disabled)

🏗️  Building Docker image with embedded CLI...
🚀 Starting services...
⏳ Waiting for services to become healthy...
✅ Server is healthy!

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ✅ Installation Complete!
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📍 Installation directory: /home/user/emergent-standalone
🌐 Server URL: http://localhost:3002
🔑 API Key: a1b2c3d4e5f6...

Quick Commands:
  docker exec emergent-server emergent-cli projects list
  docker exec emergent-server emergent-cli status
```

## After Installation

### 1. Get Your Credentials

```bash
cat ~/emergent-standalone/deploy/minimal/credentials.txt
```

Output:

```
Emergent Standalone - Credentials
Generated: 2026-02-06 12:00:00

Server URL: http://localhost:3002
API Key: your-generated-api-key

PostgreSQL:
  Host: localhost:15432
  User: emergent
  Password: your-generated-password
  Database: emergent

MinIO:
  Console: http://localhost:19001
  API: http://localhost:19000
  User: minioadmin
  Password: your-generated-password
```

### 2. Verify Installation

```bash
# Automatic verification
~/emergent-standalone/deploy/minimal/verify-install.sh

# Or manually
curl http://localhost:3002/health
docker exec emergent-server emergent-cli projects list
```

### 3. Start Using Emergent

```bash
# List projects
docker exec emergent-server emergent-cli projects list

# Check authentication
docker exec emergent-server emergent-cli status

# View configuration
docker exec emergent-server emergent-cli config show
```

## Customization Options

### Custom Installation Directory

```bash
INSTALL_DIR=/opt/emergent curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/minimal/install.sh | bash
```

### Custom Server Port

```bash
SERVER_PORT=8080 curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/minimal/install.sh | bash
```

### Provide Google API Key

```bash
GOOGLE_API_KEY=your-key curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/minimal/install.sh | bash
```

### Specific Version

```bash
EMERGENT_VERSION=v1.0.0 curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/minimal/install.sh | bash
```

### Multiple Options

```bash
INSTALL_DIR=/opt/emergent SERVER_PORT=8080 GOOGLE_API_KEY=your-key curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/minimal/install.sh | bash
```

## What Gets Installed

### Docker Containers

| Container          | Purpose               | Port         |
| ------------------ | --------------------- | ------------ |
| emergent-server    | API + CLI             | 3002         |
| emergent-db        | PostgreSQL + pgvector | 15432        |
| emergent-minio     | S3 storage            | 19000, 19001 |
| emergent-kreuzberg | Document extraction   | 18000        |

### Files and Directories

```
~/emergent-standalone/
├── .git/                       # Git repository (for updates)
├── apps/                       # Source code
├── tools/                      # CLI source
├── deploy/minimal/
│   ├── .env.local             # Generated configuration
│   ├── credentials.txt        # YOUR API KEYS (keep secure!)
│   ├── docker-compose.local.yml
│   ├── verify-install.sh
│   └── [documentation]
└── [other project files]
```

## Prerequisites

The installer checks for:

- ✅ **Docker** (v20.10+)
- ✅ **Docker Compose** (v2.0+)
- ✅ **Git** (optional, for updates)
- ✅ **curl** (to download installer)
- ✅ **openssl** or `/dev/urandom` (for password generation)

### Install Docker

**Linux:**

```bash
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER
```

**macOS:**

```bash
brew install --cask docker
```

**Windows:**
Download from https://docs.docker.com/desktop/install/windows-install/

## Common Operations

### View Logs

```bash
cd ~/emergent-standalone/deploy/minimal
docker compose -f docker-compose.local.yml logs -f
```

### Stop Services

```bash
cd ~/emergent-standalone/deploy/minimal
docker compose -f docker-compose.local.yml down
```

### Restart Services

```bash
cd ~/emergent-standalone/deploy/minimal
docker compose -f docker-compose.local.yml restart
```

### Update to Latest Version

```bash
cd ~/emergent-standalone
git pull
cd deploy/minimal
docker compose -f docker-compose.local.yml down
./build-server-with-cli.sh
docker compose -f docker-compose.local.yml up -d
```

### Uninstall

```bash
cd ~/emergent-standalone/deploy/minimal
docker compose -f docker-compose.local.yml down -v
cd ~
rm -rf ~/emergent-standalone
```

## Troubleshooting

### Installation Fails

**Check Docker:**

```bash
docker --version
docker compose version
```

**Check logs:**

```bash
cd ~/emergent-standalone/deploy/minimal
docker compose -f docker-compose.local.yml logs
```

### Server Not Responding

```bash
# Check if running
docker ps | grep emergent-server

# View logs
docker logs emergent-server

# Restart
cd ~/emergent-standalone/deploy/minimal
docker compose -f docker-compose.local.yml restart server
```

### CLI Commands Fail

```bash
# Verify API key
cat ~/emergent-standalone/deploy/minimal/credentials.txt

# Test authentication
docker exec emergent-server emergent-cli status

# Check server health
curl http://localhost:3002/health
```

### Port Already in Use

Change the port:

```bash
# Edit .env.local
cd ~/emergent-standalone/deploy/minimal
nano .env.local
# Change SERVER_PORT=3002 to SERVER_PORT=8080

# Restart
docker compose -f docker-compose.local.yml down
docker compose -f docker-compose.local.yml up -d
```

## Security Notes

### Generated Passwords

The installer generates **cryptographically secure passwords** using:

- `openssl rand -hex 32` (64 characters)
- OR `/dev/urandom` fallback (64 characters)

### Credentials File

**IMPORTANT**: The `credentials.txt` file contains sensitive information!

```bash
# Keep it secure
chmod 600 ~/emergent-standalone/deploy/minimal/credentials.txt

# Back it up securely
cp ~/emergent-standalone/deploy/minimal/credentials.txt ~/safe-backup-location/
```

### Network Exposure

By default, services are **only accessible from localhost**:

- ✅ Server: `localhost:3002` (not exposed to network)
- ✅ PostgreSQL: `localhost:15432` (not exposed)
- ✅ MinIO: `localhost:19000, 19001` (not exposed)

To expose to network, see [README.md](./README.md) Tailscale section.

## Next Steps

1. **Read the quick reference**: `~/emergent-standalone/deploy/minimal/CLI_QUICK_REFERENCE.md`
2. **Explore CLI commands**: `docker exec emergent-server emergent-cli --help`
3. **Create a project**: `docker exec -it emergent-server emergent-cli projects create`
4. **Upload documents**: See [CLI_USAGE.md](./CLI_USAGE.md) for examples
5. **Set up automation**: Use examples from CLI_USAGE.md

## Support

- **Documentation**: `~/emergent-standalone/deploy/minimal/INDEX.md`
- **GitHub Issues**: https://github.com/emergent-company/emergent.memory/issues
- **Verification**: `~/emergent-standalone/deploy/minimal/verify-install.sh`

---

**Installation URL**:

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/minimal/install.sh | bash
```

**That's it! One command, ~3 minutes, ready to use.** 🚀
