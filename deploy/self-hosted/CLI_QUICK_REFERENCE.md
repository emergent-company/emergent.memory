# Emergent CLI Quick Reference

Quick command reference for using `emergent-cli` inside the Docker deployment.

## Basic Usage Pattern

```bash
docker exec emergent-server emergent-cli <command> [options]
```

## Most Common Commands

### Projects

```bash
# List all projects
docker exec emergent-server emergent-cli projects list

# Show project details
docker exec emergent-server emergent-cli projects get <project-id>

# Create project (interactive)
docker exec -it emergent-server emergent-cli projects create
```

### Configuration

```bash
# Show current configuration
docker exec emergent-server emergent-cli config show

# Set default project
docker exec emergent-server emergent-cli config set project_id <id>

# Set default organization
docker exec emergent-server emergent-cli config set org_id <id>
```

### Status

```bash
# Check authentication and connection
docker exec emergent-server emergent-cli status

# Show version
docker exec emergent-server emergent-cli version
```

## Interactive Shell

For multiple commands, open an interactive shell:

```bash
# Open shell
docker exec -it emergent-server sh

# Inside shell, run commands directly
emergent-cli projects list
emergent-cli config show
exit
```

## Output Formats

```bash
# Default: Human-readable table
docker exec emergent-server emergent-cli projects list

# JSON output (for scripts)
docker exec emergent-server emergent-cli projects list --output json

# YAML output
docker exec emergent-server emergent-cli projects list --output yaml
```

## Scripting Examples

### Get Project ID

```bash
PROJECT_ID=$(docker exec emergent-server \
  emergent-cli projects list --output json | \
  jq -r '.[0].id')
echo "First project ID: $PROJECT_ID"
```

### Check Health

```bash
if docker exec emergent-server emergent-cli status &>/dev/null; then
  echo "✅ Connection OK"
else
  echo "❌ Connection failed"
fi
```

### Backup Projects

```bash
docker exec emergent-server \
  emergent-cli projects list --output json \
  > projects-backup-$(date +%Y%m%d).json
```

## Environment Variables

Override defaults with environment variables:

```bash
# Custom server URL
docker exec \
  -e EMERGENT_SERVER_URL=http://custom-url:3002 \
  emergent-server \
  emergent-cli projects list

# Custom API key
docker exec \
  -e EMERGENT_API_KEY=custom-key \
  emergent-server \
  emergent-cli projects list
```

## Troubleshooting

### CLI not found

```bash
# Verify binary exists
docker exec emergent-server which emergent-cli
```

### Connection failed

```bash
# Check server health
docker exec emergent-server wget -qO- http://localhost:3002/health

# Verify API key
docker exec emergent-server env | grep STANDALONE_API_KEY
```

### Permission denied

```bash
# Ensure container is running
docker ps | grep emergent-server

# Check logs
docker logs emergent-server
```

## Docker Compose Alternative

If using docker-compose:

```bash
# All commands work with docker-compose exec
docker-compose exec server emergent-cli projects list
docker-compose exec server emergent-cli config show
```

## Full Documentation

- Complete CLI guide: [CLI_USAGE.md](./CLI_USAGE.md)
- Deployment guide: [DEPLOYMENT_REPORT.md](./DEPLOYMENT_REPORT.md)
- CLI source: `/root/emergent/tools/emergent-cli/README.md`
