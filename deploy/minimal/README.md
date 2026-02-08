# Emergent Minimal Standalone Deployment

Single-user deployment with MCP access and secure Tailscale networking.

## One-Command Installation

**Copy and paste this into your terminal:**

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent/main/deploy/minimal/install.sh | bash
```

That's it! The installer will:

- ✅ Download Emergent
- ✅ Generate secure passwords automatically
- ✅ Build Docker images
- ✅ Start all services
- ✅ Verify installation

**Installation takes ~2-3 minutes.**

After installation:

```bash
# Your API key and credentials are saved to:
~/emergent-standalone/deploy/minimal/credentials.txt

# Test the installation
docker exec emergent-server emergent-cli projects list
```

## Stack Components

- **Go Backend with CLI** - API server + emergent-cli binary (port 3002)
- **PostgreSQL** - Database with pgvector extension
- **Kreuzberg** - Document extraction service (56+ file formats)
- **MinIO** - S3-compatible object storage
- **Tailscale** - Secure network access (optional)

### CLI Access

The server container includes the `emergent-cli` binary for management:

```bash
# List projects
docker exec emergent-server emergent-cli projects list

# Interactive shell
docker exec -it emergent-server sh
```

See [CLI_USAGE.md](./CLI_USAGE.md) for complete CLI documentation.

## Manual Installation

### Prerequisites

- Docker and Docker Compose v2+
- Git (optional, for updates)
- Google API key (optional, for embeddings)

### Manual Installation Steps

1. **Clone repository**

   ```bash
   git clone https://github.com/emergent-company/emergent.git
   cd emergent/deploy/minimal
   ```

2. **Run installer**

   ```bash
   ./install.sh
   ```

   Or manually configure:

   ```bash
   cp .env.example .env
   # Edit .env with your values
   docker compose -f docker-compose.local.yml up -d
   ```

### Custom Configuration

The installer accepts environment variables:

### Custom Configuration

The installer accepts environment variables:

```bash
# Custom installation directory (default: ~/emergent-standalone)
INSTALL_DIR=/opt/emergent curl -fsSL ... | bash

# Custom server port (default: 3002)
SERVER_PORT=8080 curl -fsSL ... | bash

# Provide Google API key during install
GOOGLE_API_KEY=your-key curl -fsSL ... | bash

# Use specific version/branch (default: main)
EMERGENT_VERSION=v1.0.0 curl -fsSL ... | bash
```

## What Gets Installed

### Services

| Service       | Port  | Purpose                |
| ------------- | ----- | ---------------------- |
| Emergent API  | 3002  | Main API server + CLI  |
| PostgreSQL    | 15432 | Database with pgvector |
| MinIO API     | 19000 | S3-compatible storage  |
| MinIO Console | 19001 | Web UI for MinIO       |
| Kreuzberg     | 18000 | Document extraction    |

### Files Created

```
~/emergent-standalone/
├── deploy/minimal/
│   ├── .env.local              # Generated configuration
│   ├── credentials.txt         # Your API keys and passwords
│   ├── docker-compose.local.yml
│   └── [documentation files]
└── [source code]
```

## Post-Installation

### Verify Installation

```bash
# Check server health
curl http://localhost:3002/health

# List projects
docker exec emergent-server emergent-cli projects list

# View all services
docker compose -f ~/emergent-standalone/deploy/minimal/docker-compose.local.yml ps
```

### Get Your Credentials

```bash
# View API key and passwords
cat ~/emergent-standalone/deploy/minimal/credentials.txt
```

### Common Commands

# - POSTGRES_PASSWORD (generate: openssl rand -hex 32)

# - MINIO_ROOT_PASSWORD (generate: openssl rand -hex 32)

# - STANDALONE_API_KEY (generate: openssl rand -hex 32)

# - GOOGLE_API_KEY (from Google Cloud Console)

# - TS_AUTHKEY (from Tailscale admin panel)

````

### 3. Start Services

```bash
docker compose up -d
````

### 4. Verify Deployment

```bash
# Check all services are running
docker compose ps

# Check server health
curl http://localhost:3002/health

# Check Tailscale status
docker exec emergent-tailscale tailscale status
```

## Accessing Your Deployment

### Via Tailscale Network

Once deployed, your Emergent instance will appear in your Tailscale network as `emergent` (or your custom hostname).

From any device in your Tailscale network:

```bash
# Access API
curl http://emergent:3002/health

# Configure MCP client
# Host: emergent:3002
# API Key: (from .env STANDALONE_API_KEY)
```

### Via Localhost (on deployment server)

```bash
# API server
curl http://localhost:3002/health

# MinIO console
open http://localhost:9001
```

## MCP Configuration

Configure your MCP client (Claude Desktop, Cursor, etc.) with SSE transport:

```json
{
  "servers": {
    "emergent": {
      "type": "sse",
      "url": "http://localhost:3002/api/mcp/sse/<PROJECT_ID>",
      "headers": {
        "X-API-Key": "<YOUR_API_KEY>"
      }
    }
  }
}
```

**To get your Project ID and API Key:**

```bash
# Get your project ID
docker exec emergent-server emergent-cli projects list

# Get your API key (saved during installation)
cat ~/emergent-standalone/deploy/minimal/credentials.txt
```

**Via Tailscale network:**

```json
{
  "servers": {
    "emergent": {
      "type": "sse",
      "url": "http://emergent:3002/api/mcp/sse/<PROJECT_ID>",
      "headers": {
        "X-API-Key": "<YOUR_API_KEY>"
      }
    }
  }
}
```

## Architecture

```
┌─────────────────────────────────────────┐
│         Tailscale Network               │
│  (secure overlay, no port exposure)     │
│                                         │
│  ┌──────────────────────────────────┐  │
│  │  Tailscale Sidecar               │  │
│  │  hostname: emergent              │  │
│  │  shares network with server      │  │
│  └──────────────────────────────────┘  │
│              │                          │
│              ▼                          │
│  ┌──────────────────────────────────┐  │
│  │  Go Backend (port 3002)          │  │
│  │  - API endpoints                 │  │
│  │  - Standalone auth (API key)     │  │
│  └──────────────────────────────────┘  │
│       │         │         │             │
│       ▼         ▼         ▼             │
│  ┌────────┐ ┌──────┐ ┌───────┐         │
│  │ Postgres│ │Kreuz-│ │ MinIO │         │
│  │+pgvector│ │ berg │ │  S3   │         │
│  └────────┘ └──────┘ └───────┘         │
└─────────────────────────────────────────┘
```

## Service Details

### Go Backend

- **Port**: 3002 (accessible via Tailscale)
- **Health**: `http://emergent:3002/health`
- **Logs**: `./logs/server/`

### PostgreSQL

- **Port**: 5432 (internal only)
- **Database**: `emergent`
- **Extensions**: pgvector

### Kreuzberg

- **Port**: 8000 (internal only)
- **Formats**: PDF, DOCX, PPTX, XLSX, images (OCR), HTML, Markdown
- **Health**: `http://kreuzberg:8000/health`

### MinIO

- **API Port**: 9000 (internal only)
- **Console**: 9001 (accessible via `localhost:9001` on host)
- **Buckets**: `documents`, `document-temp`
- **Access**: Admin user from env

### Tailscale

- **Mode**: Sidecar (shares network namespace with server)
- **Hostname**: `emergent` (configurable)
- **Access**: All devices in your Tailscale network

## Configuration Reference

### Required Environment Variables

| Variable              | Description           | Generation             |
| --------------------- | --------------------- | ---------------------- |
| `POSTGRES_PASSWORD`   | Database password     | `openssl rand -hex 32` |
| `MINIO_ROOT_PASSWORD` | MinIO admin password  | `openssl rand -hex 32` |
| `STANDALONE_API_KEY`  | MCP authentication    | `openssl rand -hex 32` |
| `GOOGLE_API_KEY`      | Vertex AI credentials | Google Cloud Console   |
| `TS_AUTHKEY`          | Tailscale auth key    | Tailscale admin panel  |

### Optional Configuration

| Variable                  | Default                | Description                   |
| ------------------------- | ---------------------- | ----------------------------- |
| `TAILSCALE_HOSTNAME`      | `emergent`             | Hostname in Tailscale network |
| `STANDALONE_USER_EMAIL`   | `admin@localhost`      | Default user email            |
| `STANDALONE_ORG_NAME`     | `Default Organization` | Default org name              |
| `STANDALONE_PROJECT_NAME` | `Default Project`      | Default project name          |
| `EMBEDDING_DIMENSION`     | `768`                  | Embedding vector size         |
| `KREUZBERG_LOG_LEVEL`     | `info`                 | Kreuzberg logging             |

## Management

### View Logs

```bash
# All services
docker compose logs -f

# Specific service
docker compose logs -f server
docker compose logs -f tailscale

# Server application logs
tail -f ./logs/server/app.log
```

### Restart Services

```bash
# All services
docker compose restart

# Specific service
docker compose restart server
```

### Update Deployment

```bash
# Pull latest images
docker compose pull

# Rebuild and restart
docker compose up -d --build
```

### Backup

```bash
# Backup database
docker compose exec db pg_dump -U emergent emergent > backup.sql

# Backup MinIO data
docker compose exec minio mc mirror myminio/documents ./backup/documents/
```

## Troubleshooting

### Tailscale Not Connecting

```bash
# Check Tailscale logs
docker compose logs tailscale

# Verify auth key is valid
# Auth keys expire - generate new one if needed

# Check container has network capabilities
docker inspect emergent-tailscale | grep -A 10 CapAdd
```

### Server Not Accessible via Tailscale

```bash
# Verify Tailscale hostname
docker exec emergent-tailscale tailscale status

# Check server is running
curl http://localhost:3002/health

# Verify network_mode in docker-compose.yml
docker inspect emergent-tailscale | grep NetworkMode
```

### Database Connection Issues

```bash
# Check database is healthy
docker compose ps db

# Test connection
docker compose exec db psql -U emergent -d emergent -c "SELECT 1"

# View database logs
docker compose logs db
```

### Kreuzberg Extraction Failing

```bash
# Check Kreuzberg health
curl http://localhost:8000/health

# View logs
docker compose logs kreuzberg

# Check memory (needs 512MB minimum)
docker stats emergent-kreuzberg
```

### MinIO Access Issues

```bash
# Check MinIO health
curl http://localhost:9000/minio/health/live

# Verify buckets were created
docker compose logs minio-init

# Recreate buckets
docker compose run --rm minio-init
```

## Security Considerations

### Tailscale Security

- Auth keys should be rotated every 90 days
- Use tags (`tag:emergent`) for ACL management
- Never commit auth keys to git
- Use ephemeral keys for temporary access

### API Security

- `STANDALONE_API_KEY` grants full system access
- Rotate API key periodically
- Use strong random keys (32+ bytes)
- Store securely in MCP client config

### Network Security

- No ports exposed to public internet
- All access via Tailscale encrypted network
- MinIO console only on localhost
- Internal services (DB, Kreuzberg) not exposed

## Upgrading

### Minor Updates (patch versions)

```bash
docker compose pull
docker compose up -d
```

### Major Updates (breaking changes)

1. Backup data first
2. Review CHANGELOG for migration steps
3. Run database migrations if needed
4. Update docker-compose.yml if required
5. Test with `docker compose up -d`

## Support

For issues specific to:

- **Tailscale**: https://tailscale.com/contact/support
- **Emergent**: GitHub issues or documentation
- **Kreuzberg**: https://github.com/Striveworks/kreuzberg
- **MinIO**: https://min.io/docs/minio/linux/
