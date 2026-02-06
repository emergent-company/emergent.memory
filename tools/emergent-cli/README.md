# Emergent CLI

Command-line interface for the Emergent Knowledge Base platform. Supports both standalone Docker deployments (API key authentication) and full Zitadel OAuth deployments.

## Features

- **Dual Authentication**: API key for standalone mode, OAuth device flow for Zitadel
- **Project Management**: List, create, and manage projects
- **Cross-Platform**: Pre-built binaries for Linux, macOS, Windows, FreeBSD
- **Docker Support**: Multi-arch container images
- **Configuration**: File-based and environment variable configuration

## Installation

### Quick Install (Recommended)

**One-line install** (Linux/macOS):

```bash
curl -fsSL https://raw.githubusercontent.com/eyedea-io/emergent/master/tools/emergent-cli/install.sh | bash
```

This will:

- Automatically detect your OS and architecture
- Download the latest release
- Install to `/usr/local/bin` (or `~/bin` if not root)
- Verify the installation

**Manual version selection:**

```bash
VERSION=cli-v0.1.0 curl -fsSL https://raw.githubusercontent.com/eyedea-io/emergent/master/tools/emergent-cli/install.sh | bash
```

### Pre-Built Binaries

Download from [GitHub Releases](https://github.com/eyedea-io/emergent/releases):

#### Linux (amd64)

```bash
curl -L -o emergent-cli.tar.gz https://github.com/eyedea-io/emergent/releases/latest/download/emergent-cli-linux-amd64.tar.gz
tar xzf emergent-cli.tar.gz
sudo mv emergent-cli-linux-amd64 /usr/local/bin/emergent-cli
chmod +x /usr/local/bin/emergent-cli
```

#### macOS (Apple Silicon)

```bash
curl -L -o emergent-cli.tar.gz https://github.com/eyedea-io/emergent/releases/latest/download/emergent-cli-darwin-arm64.tar.gz
tar xzf emergent-cli.tar.gz
sudo mv emergent-cli-darwin-arm64 /usr/local/bin/emergent-cli
chmod +x /usr/local/bin/emergent-cli
```

#### Windows (PowerShell)

```powershell
Invoke-WebRequest -Uri "https://github.com/eyedea-io/emergent/releases/latest/download/emergent-cli-windows-amd64.zip" -OutFile "emergent-cli.zip"
Expand-Archive -Path emergent-cli.zip -DestinationPath .
# Add to PATH or move to a directory in your PATH
```

### Docker

```bash
docker pull ghcr.io/eyedea-io/emergent-cli:latest

# Run with environment variables
docker run --rm \
  -e EMERGENT_SERVER_URL=http://localhost:9090 \
  -e EMERGENT_API_KEY=your-api-key \
  ghcr.io/eyedea-io/emergent-cli:latest projects list
```

### Build from Source

Requires Go 1.24+:

```bash
git clone https://github.com/eyedea-io/emergent.git
cd emergent/tools/emergent-cli
go build -o emergent-cli ./cmd
```

## Quick Start

### Standalone Mode (Docker/API Key)

1. Start the standalone server:

```bash
docker run -d -p 9090:9090 \
  -e POSTGRES_URL=postgresql://user:pass@host:5432/db \
  -e API_KEY=your-secure-key \
  emergent-server-standalone:latest
```

2. Configure CLI:

```bash
export EMERGENT_SERVER_URL=http://localhost:9090
export EMERGENT_API_KEY=your-secure-key
```

3. Test connection:

```bash
emergent-cli projects list
```

### OAuth Mode (Zitadel)

1. Configure server URL:

```bash
export EMERGENT_SERVER_URL=https://api.emergent-company.ai
```

2. Login via device flow:

```bash
emergent-cli auth login
# Opens browser for authentication
```

3. Use commands:

```bash
emergent-cli projects list
emergent-cli config show
```

## Configuration

### Environment Variables

```bash
EMERGENT_SERVER_URL       # Required: Base URL of Emergent server
EMERGENT_API_KEY          # Optional: API key for standalone mode
EMERGENT_ORG_ID          # Optional: Default organization ID
EMERGENT_PROJECT_ID      # Optional: Default project ID
```

### Configuration File

Location: `~/.emergent/config.yaml`

```yaml
server_url: https://api.emergent-company.ai
org_id: org_abc123
project_id: proj_xyz789
```

## Commands

### Authentication

```bash
emergent-cli auth login         # Start OAuth device flow
emergent-cli auth logout        # Clear stored credentials
emergent-cli auth status        # Check authentication status
```

### Projects

```bash
emergent-cli projects list      # List all projects
emergent-cli projects create    # Create new project (interactive)
emergent-cli projects get <id>  # Get project details
```

### Configuration

```bash
emergent-cli config show        # Display current configuration
emergent-cli config set <key> <value>  # Set configuration value
```

### General

```bash
emergent-cli version            # Show version information
emergent-cli help               # Show help
```

## Usage Examples

### List Projects

```bash
# All projects
emergent-cli projects list

# With specific org (override default)
EMERGENT_ORG_ID=org_123 emergent-cli projects list

# JSON output
emergent-cli projects list --output json
```

### Create Project

```bash
# Interactive mode
emergent-cli projects create

# With flags
emergent-cli projects create \
  --name "My Knowledge Base" \
  --description "Documentation and research"
```

### CI/CD Usage

```bash
#!/bin/bash
set -e

export EMERGENT_SERVER_URL=https://api.prod.emergent.com
export EMERGENT_API_KEY=$PROD_API_KEY

PROJECT_ID=$(emergent-cli projects create \
  --name "Automated KB" \
  --output json | jq -r '.id')

echo "Created project: $PROJECT_ID"
```

## Authentication Details

### Standalone Mode (API Key)

When `EMERGENT_API_KEY` is set:

- Uses `X-API-Key` header for all requests
- No OAuth flow required
- Ideal for Docker deployments and automation

### OAuth Mode (Zitadel)

When no API key is set:

1. Runs OAuth 2.0 device flow
2. Opens browser for login
3. Stores credentials in `~/.emergent/credentials.json`
4. Auto-refreshes tokens when expired

Credentials structure:

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "expires_at": "2024-02-07T12:00:00Z"
}
```

## Troubleshooting

### Connection Errors

```bash
# Test server connectivity
curl $EMERGENT_SERVER_URL/health

# Check configuration
emergent-cli config show

# Verify API key
curl -H "X-API-Key: $EMERGENT_API_KEY" \
  $EMERGENT_SERVER_URL/api/projects
```

### Authentication Issues

```bash
# OAuth: Re-authenticate
emergent-cli auth logout
emergent-cli auth login

# API Key: Verify key is correct
echo $EMERGENT_API_KEY

# Check server logs for auth errors
docker logs emergent-server
```

### Debug Mode

```bash
# Enable verbose logging
emergent-cli --debug projects list

# Check credential file
cat ~/.emergent/credentials.json

# Verify token validity (OAuth mode)
emergent-cli auth status
```

## Development

### Running Tests

```bash
cd tools/emergent-cli
go test ./...
go test -race ./...
go test -coverprofile=coverage.out ./...
```

### Integration Tests

```bash
# Start test server
docker-compose -f docker/docker-compose.standalone.yml up -d

# Run integration tests
go test -tags=integration ./...
```

### Building

```bash
# Local build
go build -o emergent-cli ./cmd

# Cross-compile
GOOS=linux GOARCH=amd64 go build -o emergent-cli-linux ./cmd
GOOS=darwin GOARCH=arm64 go build -o emergent-cli-macos ./cmd
GOOS=windows GOARCH=amd64 go build -o emergent-cli.exe ./cmd
```

## Contributing

1. Fork the repository
2. Create feature branch: `git checkout -b feature/cli-enhancement`
3. Make changes and add tests
4. Run tests: `go test ./...`
5. Run linter: `golangci-lint run`
6. Submit pull request

## License

See root repository LICENSE file.

## Documentation

- [Standalone Mode Guide](../../docs/EMERGENT_CLI_STANDALONE.md) - Comprehensive standalone deployment guide
- [Technical Implementation](../../docs/STANDALONE_CLI_IMPLEMENTATION.md) - Architecture and implementation details
- [Release Process](../../docs/EMERGENT_CLI_RELEASE_PROCESS.md) - How releases are created and published

## Support

- GitHub Issues: https://github.com/eyedea-io/emergent/issues
- Documentation: https://github.com/eyedea-io/emergent/tree/master/docs
