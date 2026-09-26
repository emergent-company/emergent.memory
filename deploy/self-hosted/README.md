# Emergent Minimal Standalone Deployment

Single-user deployment with MCP access and secure Tailscale networking.

## One-Command Installation

**Copy and paste this into your terminal:**

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/self-hosted/install-online.sh | bash
```

That's it! The installer will:

- ✅ Pull pre-built Docker images
- ✅ Generate secure passwords automatically
- ✅ Start all services
- ✅ Install the CLI
- ✅ Configure MCP

**Installation takes ~2-3 minutes.**

After installation:

```bash
# Test the installation
emergent projects list

# Your credentials are in the CLI config
cat ~/.emergent/config.yaml
```

## Stack Components

- **Go Backend** - API server (port 3002)
- **Emergent CLI** - Local management tool
- **PostgreSQL** - Database with pgvector extension
- **Kreuzberg** - Document extraction service (56+ file formats)
- **SeaweedFS** - S3-compatible object storage (single node: master + volume + filer + S3)

### CLI Access

The CLI is installed locally at `~/.emergent/bin/emergent`:

```bash
# List projects
emergent projects list

# Check status
emergent status
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
   cd emergent/deploy/self-hosted
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
| Emergent API    | 3002  | Main API server + CLI  |
| PostgreSQL      | 15432 | Database with pgvector |
| SeaweedFS S3    | 19000 | S3-compatible storage  |
| Kreuzberg       | 18000 | Document extraction    |

### Files Created

```
~/emergent-standalone/
├── deploy/self-hosted/
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
docker compose -f ~/emergent-standalone/deploy/self-hosted/docker-compose.local.yml ps
```

### Get Your Credentials

```bash
# View API key and passwords
cat ~/emergent-standalone/deploy/self-hosted/credentials.txt
```

### Common Commands

# - POSTGRES_PASSWORD (generate: openssl rand -hex 32)

# - OBJECT_STORE_SECRET_KEY (generate: openssl rand -hex 32)

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

# SeaweedFS S3 API (host port OBJECT_STORE_API_PORT, default 9000)
curl http://localhost:9000/status
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
cat ~/emergent-standalone/deploy/self-hosted/credentials.txt
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
│  ┌────────┐ ┌──────┐ ┌─────────┐      │
│  │ Postgres│ │Kreuz-│ │SeaweedFS│      │
│  │+pgvector│ │ berg │ │   S3    │      │
│  └────────┘ └──────┘ └─────────┘      │
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

### Scheduled Database Backup

The server image runs a scheduled `pg_dump` backup (see `scheduler.database_backup`). Its `pg_dump` client major is set by the `PG_CLIENT_MAJOR` build argument (default `17`) and must match the major of the `pgvector/pgvector:pg17` database image. The override must name a client major available in the server image's base distribution (`alpine:3.21` ships 15, 16, and 17); requesting a newer major requires bumping the base image first. If the two majors drift apart, `pg_dump` refuses to dump a newer server and the scheduled backup fails with a version-mismatch error. The `/health` endpoint reports a `database_backup` check: a failing backup makes the overall status `degraded`. Failed backup rows are visible at `GET /api/superadmin/database-backups`.

### Kreuzberg

- **Port**: 8000 (internal only)
- **Formats**: PDF, DOCX, PPTX, XLSX, images (OCR), HTML, Markdown
- **Health**: `http://kreuzberg:8000/health`

### SeaweedFS

- **API Port**: 8333 in-container (published on host as `OBJECT_STORE_API_PORT`, default 9000)
- **Single node**: `server -dir=/data -s3` runs master + volume + filer + S3 in one process
- **Buckets**: `documents`, `document-temp` (created by the one-shot `storage-init` service)
- **Access**: `OBJECT_STORE_ACCESS_KEY` / `OBJECT_STORE_SECRET_KEY` from env

### Tailscale

- **Mode**: Sidecar (shares network namespace with server)
- **Hostname**: `emergent` (configurable)
- **Access**: All devices in your Tailscale network

## Configuration Reference

### Required Environment Variables

| Variable                  | Description                  | Generation             |
| ------------------------- | ---------------------------- | ---------------------- |
| `POSTGRES_PASSWORD`       | Database password            | `openssl rand -hex 32` |
| `OBJECT_STORE_SECRET_KEY` | Object store secret key      | `openssl rand -hex 32` |
| `STANDALONE_API_KEY`      | MCP authentication           | `openssl rand -hex 32` |
| `GOOGLE_API_KEY`          | Vertex AI credentials        | Google Cloud Console   |
| `TS_AUTHKEY`              | Tailscale auth key           | Tailscale admin panel  |

### Optional Configuration

| Variable                  | Default                | Description                        |
| ------------------------- | ---------------------- | ---------------------------------- |
| `TAILSCALE_HOSTNAME`      | `emergent`             | Hostname in Tailscale network      |
| `STANDALONE_USER_EMAIL`   | `admin@localhost`      | Default user email                 |
| `STANDALONE_ORG_NAME`     | `Default Organization` | Default org name                   |
| `STANDALONE_PROJECT_NAME` | `Default Project`      | Default project name               |
| `EMBEDDING_DIMENSION`     | `768`                  | Embedding vector size              |
| `KREUZBERG_LOG_LEVEL`     | `info`                 | Kreuzberg logging                  |
| `OBJECT_STORE_ACCESS_KEY` | `emergent`             | Object store access key            |
| `OBJECT_STORE_API_PORT`   | `9000`                 | Host port for the S3 API           |
| `STORAGE_REGION`          | `us-east-1`            | Region used to sign S3 requests    |

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

# Backup object-store data (any generic S3 client, e.g. rclone)
# Configure an S3 remote pointed at the SeaweedFS endpoint, then:
rclone copy seaweedfs:documents ./backup/documents/

# Or archive the raw volume (stop the stack first for a consistent snapshot):
docker compose stop seaweedfs
docker run --rm -v docker_object_store_data:/data -v "$PWD/backup":/backup alpine \
  tar czf /backup/object-store-data.tgz -C /data .
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

### Object Store Access Issues

```bash
# Check SeaweedFS health (master cluster status)
docker compose exec seaweedfs wget -qO- http://127.0.0.1:9333/cluster/status

# Verify buckets were created
docker compose logs storage-init

# Recreate buckets
docker compose run --rm storage-init
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
- Object store API published only on the configured host port
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

### Migrating an existing install from MinIO to SeaweedFS

Existing installs hold objects in the old `minio_data` volume. Mirror them to
the new backend before flipping the endpoint and keep the old volume for
rollback — see [UPGRADING_OBJECT_STORE.md](./UPGRADING_OBJECT_STORE.md) and
`migrate-object-store.sh`.

## Support

For issues specific to:

- **Tailscale**: https://tailscale.com/contact/support
- **Emergent**: GitHub issues or documentation
- **Kreuzberg (v4 LTS)**: https://kreuzberg.dev
- **SeaweedFS**: https://github.com/seaweedfs/seaweedfs
