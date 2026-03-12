# Memory CLI

Command-line interface for the Memory knowledge graph platform. Supports API key authentication (standalone/self-hosted) and OAuth via Zitadel.

## Installation

### CLI only (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/install-cli.sh | bash
```

Detects OS and architecture, downloads the latest pre-built binary from GitHub Releases, and installs it to `~/.memory/bin/memory`. Add `~/.memory/bin` to your `PATH` if it isn't already.

Pin a specific version:

```bash
VERSION=v0.32.4 curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/install-cli.sh | bash
```

### Pre-built binaries

Download from [GitHub Releases](https://github.com/emergent-company/emergent.memory/releases):

#### Linux (amd64)

```bash
curl -L -o memory-cli.tar.gz https://github.com/emergent-company/emergent.memory/releases/latest/download/memory-cli-linux-amd64.tar.gz
tar xzf memory-cli.tar.gz
sudo mv memory-cli-linux-amd64 /usr/local/bin/memory
```

#### macOS (Apple Silicon)

```bash
curl -L -o memory-cli.tar.gz https://github.com/emergent-company/emergent.memory/releases/latest/download/memory-cli-darwin-arm64.tar.gz
tar xzf memory-cli.tar.gz
sudo mv memory-cli-darwin-arm64 /usr/local/bin/memory
```

#### Windows (PowerShell)

```powershell
Invoke-WebRequest -Uri "https://github.com/emergent-company/emergent.memory/releases/latest/download/memory-cli-windows-amd64.zip" -OutFile "memory-cli.zip"
Expand-Archive -Path memory-cli.zip -DestinationPath .
# Move memory.exe to a directory in your PATH
```

### Build from source

Requires Go 1.24+ and [`task`](https://taskfile.dev):

```bash
task cli:install        # builds and installs to ~/.memory/bin/memory
```

Or without `task`:

```bash
cd tools/cli
go build -o ~/.memory/bin/memory ./cmd/main.go
```

## Quick start

### API key (standalone / self-hosted)

```bash
export MEMORY_SERVER_URL=https://api.your-instance.com
export MEMORY_API_KEY=your-api-key

memory projects list
```

### OAuth (Zitadel)

```bash
export MEMORY_SERVER_URL=https://api.your-instance.com

memory auth login       # opens browser for device flow
memory projects list
```

## Configuration

**File:** `~/.memory/config.yaml`

```yaml
server_url: https://api.your-instance.com
org_id: org_abc123
project_id: proj_xyz789

cache:
  ttl: 5m
  enabled: true

ui:
  compact: false
  color: auto    # auto | always | never
  pager: true

query:
  default_limit: 50
  default_sort: updated_at:desc

completion:
  timeout: 2s
```

**Environment variables:**

| Variable | Description |
|---|---|
| `MEMORY_SERVER_URL` | Base URL of the Memory server |
| `MEMORY_API_KEY` | API key for standalone/self-hosted mode |
| `MEMORY_ORG_ID` | Default organization ID |
| `MEMORY_PROJECT_ID` | Default project ID |

**Precedence:** flags > env vars > config file > defaults

## Commands

```bash
# Authentication
memory auth login                  # OAuth device flow
memory auth logout
memory auth status

# Projects
memory projects list
memory projects create
memory projects get <id>

# Blueprints — apply a declarative config directory
memory blueprints ./my-config                         # from local folder
memory blueprints https://github.com/org/repo         # from GitHub
memory blueprints ./my-config --dry-run               # preview only
memory blueprints ./my-config --upgrade               # update existing resources

# Misc
memory config show
memory version
memory help
```

A blueprint directory layout:

```
my-config/
  packs/      # one YAML/JSON file per template pack
  agents/     # one YAML/JSON file per agent definition
```

## Development

```bash
cd tools/cli

go test ./...
go test -race ./...

golangci-lint run

# Cross-compile
GOOS=linux  GOARCH=amd64 go build -o memory-linux-amd64  ./cmd/main.go
GOOS=darwin GOARCH=arm64 go build -o memory-darwin-arm64 ./cmd/main.go
```

## Support

- Issues: https://github.com/emergent-company/emergent.memory/issues
- Docs: https://emergent-company.github.io/emergent.memory/
