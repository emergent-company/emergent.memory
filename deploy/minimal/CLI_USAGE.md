# Emergent CLI Usage in Docker Deployment

This guide explains how to use the `emergent` command inside your Docker deployment.

## Overview

The `emergent-server` container includes both:

- **Server** (runs as default entrypoint on port 3002)
- **CLI** (`emergent` wrapper at `/usr/local/bin/emergent` that auto-configures connection)

This allows you to run CLI commands inside the container without needing a separate CLI installation.

## Quick Start

### Run CLI in Running Container

```bash
# List projects
docker exec emergent-server emergent projects list

# Show configuration
docker exec emergent-server emergent config show

# Check status
docker exec emergent-server emergent status
```

### Interactive Shell Access

```bash
# Open shell inside container
docker exec -it emergent-server sh

# Inside container, run any CLI commands
emergent projects list
emergent config show
```

## Configuration

The CLI inside the container is pre-configured to connect to the local server automatically.

### Automatic Configuration

When running inside the `emergent-server` container, the CLI automatically uses:

- Config file at `/root/.emergent/config.yaml` (created on container startup)
- Environment variables as fallback:
  - `EMERGENT_SERVER_URL=http://localhost:3002`
  - `EMERGENT_API_KEY=[value from STANDALONE_API_KEY env var]`

### Manual Configuration

You can override with environment variables:

```bash
docker exec \
  -e EMERGENT_SERVER_URL=http://localhost:3002 \
  -e EMERGENT_API_KEY=your-custom-key \
  emergent-server \
  emergent projects list
```

Or use a config file mounted into the container:

```bash
# Create config file
cat > ~/.emergent/config.yaml <<EOF
server_url: http://localhost:3002
api_key: test-api-key-12345
EOF

# Mount and use
docker run --rm \
  -v ~/.emergent:/root/.emergent:ro \
  emergent-server \
  emergent projects list
```

## Common Commands

### Project Management

```bash
# List all projects
docker exec emergent-server emergent projects list

# Create new project (interactive)
docker exec -it emergent-server emergent projects create

# Get project details
docker exec emergent-server emergent projects get <project-id>
```

### Configuration

```bash
# Show current config
docker exec emergent-server emergent config show

# Set default project
docker exec emergent-server \
  emergent config set project_id <project-id>
```

### Authentication Status

```bash
# Check auth status (shows API key mode)
docker exec emergent-server emergent status
```

## Docker Compose Integration

If using docker-compose, you can add CLI service aliases:

```yaml
# docker-compose.yml
services:
  server:
    image: emergent-server-with-cli:latest
    # ... server config ...

# Usage
docker compose exec server emergent projects list
```

## Standalone CLI Container

You can also run the CLI as a one-off container:

```bash
# Run single command
docker run --rm \
  --network emergent \
  -e EMERGENT_SERVER_URL=http://server:3002 \
  -e EMERGENT_API_KEY=test-api-key-12345 \
  emergent-server-with-cli:latest \
  emergent projects list

# Interactive session
docker run --rm -it \
  --network emergent \
  -e EMERGENT_SERVER_URL=http://server:3002 \
  -e EMERGENT_API_KEY=test-api-key-12345 \
  emergent-server-with-cli:latest \
  sh
```

## Automation Examples

### Backup Script

```bash
#!/bin/bash
# Export all projects as JSON

docker exec emergent-server emergent projects list --output json > projects-backup.json
```

### CI/CD Pipeline

```bash
#!/bin/bash
# Create project in deployment pipeline

PROJECT_ID=$(docker exec emergent-server \
  emergent projects create \
    --name "Production KB" \
    --output json | jq -r '.id')

echo "Created project: $PROJECT_ID"
```

### Health Check Script

```bash
#!/bin/bash
# Verify CLI can connect

if docker exec emergent-server emergent projects list &>/dev/null; then
  echo "CLI connection OK"
else
  echo "CLI connection failed"
  exit 1
fi
```

## Troubleshooting

### CLI Not Found

If `emergent: not found`:

```bash
# Verify binary exists
docker exec emergent-server which emergent
# Should show: /usr/local/bin/emergent

# Check if using correct image
docker inspect emergent-server | grep Image
# Should show: emergent-server-with-cli
```

### Connection Errors

If CLI can't connect to server:

```bash
# Check server is running
docker exec emergent-server wget -qO- http://localhost:3002/health

# Verify API key
docker exec emergent-server env | grep STANDALONE_API_KEY
```

### Permission Errors

If config file is read-only:

```bash
# Mount config as read-write
docker run -v ~/.emergent:/root/.emergent:rw ...
```

## Building Custom Image

To build the image with both server and CLI:

```bash
cd /root/emergent/deploy/minimal
./build-server-with-cli.sh

# Tag for registry
docker tag emergent-server-with-cli:latest \
  ghcr.io/Emergent-Comapny/emergent-server-with-cli:latest

# Push to registry
docker push ghcr.io/Emergent-Comapny/emergent-server-with-cli:latest
```

## Alternative: Separate CLI Container

If you prefer a dedicated CLI container:

```dockerfile
FROM ghcr.io/Emergent-Comapny/emergent-cli:latest

ENV EMERGENT_SERVER_URL=http://server:3002
ENV EMERGENT_API_KEY=test-api-key-12345

ENTRYPOINT ["emergent-cli"]
CMD ["projects", "list"]
```

Build and use:

```bash
docker build -t my-emergent-cli .

docker run --rm --network emergent my-emergent-cli
```

## Best Practices

1. **Use environment variables** for non-sensitive config (server URL)
2. **Mount config file** for sensitive data (API keys) - never hardcode
3. **Use Docker secrets** in production for API keys
4. **Run CLI in same network** as server (`--network emergent`)
5. **Verify connectivity** with `emergent-cli status` before automation
6. **Use JSON output** (`--output json`) for programmatic processing
7. **Check exit codes** in scripts: CLI returns 0 on success, 1 on error

## Example: Full Automation Workflow

```bash
#!/bin/bash
set -e

# Start deployment
docker compose up -d

# Wait for server health
until docker exec emergent-server emergent-cli status; do
  echo "Waiting for server..."
  sleep 2
done

# Create project
PROJECT_ID=$(docker exec emergent-server \
  emergent-cli projects create \
    --name "Auto-Generated KB" \
    --description "Created by automation" \
    --output json | jq -r '.id')

echo "✅ Created project: $PROJECT_ID"

# Set as default
docker exec emergent-server \
  emergent-cli config set project_id "$PROJECT_ID"

# Verify
docker exec emergent-server emergent-cli config show

echo "✅ Deployment complete"
```

## Support

- GitHub Issues: https://github.com/Emergent-Comapny/emergent/issues
- Full CLI docs: `/root/emergent/tools/emergent-cli/README.md`
- Server docs: `/root/emergent/deploy/minimal/DEPLOYMENT_REPORT.md`
