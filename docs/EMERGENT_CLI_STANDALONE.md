# Emergent CLI - Standalone Mode Guide

This guide explains how to use the Emergent CLI with a standalone server (API key authentication) versus a full server with OAuth.

## Two Authentication Modes

The Emergent CLI supports two authentication modes:

### 1. **Standalone Mode** (API Key Authentication)

Use this mode when connecting to a standalone Emergent server that uses simple API key authentication. This is ideal for:

- Local development and testing
- Docker-based standalone deployments
- CI/CD pipelines
- Headless/non-interactive environments

**Configuration:**

```bash
export EMERGENT_SERVER_URL=http://localhost:9090
export EMERGENT_API_KEY=your-api-key-here
```

**Example:**

```bash
# Set environment variables
export EMERGENT_SERVER_URL=http://localhost:9090
export EMERGENT_API_KEY=a1e9e1ec8a81886ab68bbdf4a32a1deb36d3e523c639e0ad841d96263dc8b0a7

# Use CLI commands
emergent-cli projects list
```

### 2. **Full Mode** (OAuth Authentication)

Use this mode when connecting to a full Emergent server with Zitadel OAuth integration. This is the default production setup.

**Configuration:**

```bash
# Set server URL
emergent-cli config set-server https://api.emergent.example.com

# Authenticate via OAuth device flow
emergent-cli login

# Use CLI commands
emergent-cli projects list
```

## How Authentication Works

The CLI automatically detects which authentication mode to use:

```
┌─────────────────────────────────────┐
│   emergent-cli command executed     │
└─────────────────┬───────────────────┘
                  │
                  v
         ┌────────────────────┐
         │ Is EMERGENT_API_KEY│
         │   configured?      │
         └────────┬───────────┘
                  │
         ┌────────┴────────┐
         │                 │
        YES               NO
         │                 │
         v                 v
    ┌────────┐      ┌──────────┐
    │ Use    │      │ Use OAuth│
    │X-API-Key│     │ Bearer   │
    │ header │      │  token   │
    └────────┘      └──────────┘
```

## Environment Variables

| Variable              | Description                 | Required | Example                                                            |
| --------------------- | --------------------------- | -------- | ------------------------------------------------------------------ |
| `EMERGENT_SERVER_URL` | Server API base URL         | Yes      | `http://localhost:9090`                                            |
| `EMERGENT_API_KEY`    | API key for standalone mode | No\*     | `a1e9e1ec8a81886ab68bbdf4a32a1deb36d3e523c639e0ad841d96263dc8b0a7` |
| `EMERGENT_EMAIL`      | User email (optional)       | No       | `user@example.com`                                                 |
| `EMERGENT_ORG_ID`     | Default organization ID     | No       | `uuid-here`                                                        |
| `EMERGENT_PROJECT_ID` | Default project ID          | No       | `uuid-here`                                                        |
| `EMERGENT_DEBUG`      | Enable debug logging        | No       | `true`                                                             |

\* Required for standalone mode, not needed for OAuth mode

## Configuration File vs Environment Variables

The CLI supports both configuration files and environment variables. Environment variables take precedence over config file values.

**Config file location:** `$HOME/.emergent/config.yaml`

**Example config file:**

```yaml
server_url: http://localhost:9090
api_key: a1e9e1ec8a81886ab68bbdf4a32a1deb36d3e523c639e0ad841d96263dc8b0a7
org_id: ''
project_id: ''
debug: false
```

**Priority order:**

1. Environment variables (`EMERGENT_*`)
2. Config file (`~/.emergent/config.yaml`)
3. Default values

## Standalone Docker Setup Example

### 1. Start standalone Docker instance

```bash
cd /path/to/emergent-standalone-test
docker-compose up -d
```

### 2. Get your API key

The API key is in the `.env` file:

```bash
grep STANDALONE_API_KEY .env
```

### 3. Use the CLI

```bash
export EMERGENT_SERVER_URL=http://localhost:9090
export EMERGENT_API_KEY=$(grep STANDALONE_API_KEY .env | cut -d'=' -f2)

emergent-cli projects list
```

## Commands

Currently available commands:

- `emergent-cli projects list` - List all projects
- `emergent-cli login` - Authenticate via OAuth (full mode only)
- `emergent-cli status` - Show authentication status (OAuth only)
- `emergent-cli config set-server <url>` - Set server URL in config file

## Troubleshooting

### "not authenticated" error in standalone mode

**Problem:**

```
Error: failed to make request: not authenticated. Run 'emergent-cli login' first
```

**Solution:**
Make sure `EMERGENT_API_KEY` is set:

```bash
echo $EMERGENT_API_KEY  # Should output your API key
```

If empty, set it:

```bash
export EMERGENT_API_KEY=your-api-key-here
```

### "request failed with status 401"

**Problem:** API key is set but authentication fails.

**Possible causes:**

1. Wrong API key - verify the key in your `.env` file
2. Server is using OAuth mode - check `MODE=standalone` in server `.env`
3. Server is not running - check `docker ps` or `curl http://localhost:9090/health`

### Config file overrides environment variables

**Problem:** Environment variables seem to be ignored.

**Solution:**
Either:

- Remove or rename `~/.emergent/config.yaml`
- Or edit the config file to include the API key

## Security Best Practices

1. **Never commit API keys to git**

   - Add `.env` files to `.gitignore`
   - Use environment variables in CI/CD

2. **Use different API keys per environment**

   - Development: Short-lived, rotated frequently
   - Production: Long-lived, tightly controlled

3. **Rotate API keys periodically**

   - Standalone mode doesn't auto-rotate keys
   - Manual rotation recommended every 90 days

4. **Use OAuth for production user access**
   - Standalone mode is for automation only
   - End users should use OAuth (full mode)

## CI/CD Integration Example

### GitHub Actions

```yaml
name: Test with Emergent CLI
on: [push]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Download Emergent CLI
        run: |
          # Download CLI binary
          curl -L https://github.com/emergent-company/emergent/releases/latest/download/emergent-cli-linux-amd64 -o emergent-cli
          chmod +x emergent-cli

      - name: List projects
        env:
          EMERGENT_SERVER_URL: ${{ secrets.EMERGENT_SERVER_URL }}
          EMERGENT_API_KEY: ${{ secrets.EMERGENT_API_KEY }}
        run: |
          ./emergent-cli projects list
```

### GitLab CI

```yaml
test:
  image: alpine:latest
  script:
    - apk add --no-cache curl
    - curl -L https://github.com/emergent-company/emergent/releases/latest/download/emergent-cli-linux-amd64 -o emergent-cli
    - chmod +x emergent-cli
    - ./emergent-cli projects list
  variables:
    EMERGENT_SERVER_URL: $EMERGENT_SERVER_URL
    EMERGENT_API_KEY: $EMERGENT_API_KEY
```

## Implementation Details

### HTTP Client Authentication

The CLI uses a custom HTTP client wrapper (`internal/client/client.go`) that:

1. Checks if `APIKey` is set in config
2. If yes: Adds `X-API-Key` header to requests
3. If no: Loads OAuth credentials and adds `Authorization: Bearer` header
4. Automatically refreshes expired OAuth tokens

### Code Structure

```
tools/emergent-cli/
├── cmd/
│   └── main.go                 # Entry point
├── internal/
│   ├── client/
│   │   └── client.go           # HTTP client with auth
│   ├── config/
│   │   └── config.go           # Config loading (env + file)
│   ├── auth/
│   │   ├── device_flow.go      # OAuth device flow
│   │   └── credentials.go      # OAuth token storage
│   └── cmd/
│       ├── root.go             # Root command
│       ├── auth.go             # login, status commands
│       ├── config.go           # config commands
│       └── projects.go         # projects commands
```

## Next Steps

Future commands to be implemented:

- `emergent-cli documents list` - List documents
- `emergent-cli documents upload <file>` - Upload document
- `emergent-cli documents delete <id>` - Delete document
- `emergent-cli orgs list` - List organizations
- `emergent-cli users list` - List users

All commands will automatically work with both authentication modes.
