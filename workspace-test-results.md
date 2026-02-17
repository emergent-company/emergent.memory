# Agent Workspace Infrastructure Testing - v0.16.1

## Date: 2026-02-17

## Summary

Successfully validated the workspace infrastructure API on mcj-emergent server running v0.16.1. The workspace module is now enabled and functional, though provider configuration is needed for full functionality.

## Test Results

### ✅ Completed Successfully

1. **Released v0.16.1**

   - Fixed graph object unique constraint handling
   - Published Docker images and CLI binaries
   - Docker image: `ghcr.io/emergent-company/emergent-server-with-cli:0.16.1`

2. **Enabled Workspace Module**

   - Added `ENABLE_AGENT_WORKSPACES: 'true'` to docker-compose.yml on mcj-emergent
   - Changed default value from `false` to `true` in config.go for next release
   - Workspace routes now registered and accessible

3. **Workspace API Authentication**

   - Project API tokens (`emt_*`) do NOT have `admin:read`/`admin:write` scopes
   - Workspace endpoints require `X-API-Key` header with standalone API key in standalone mode
   - Successfully tested `/api/v1/agent/workspaces/providers` endpoint

4. **Created Test Project**
   - Project: `workspace-test` (2cf28517-ac2a-46f6-99ff-a61218c4aede)
   - Token: `emt_04d6af3661230ee689ca66d35e505019a4a3b8cf954173623951c9c4ce9d4c25`
   - Note: Token has `data:read, data:write` scopes only (not suitable for workspace API)

### ⚠️ Provider Configuration Needed

**Current Provider Status:**

- **gVisor**: Registered but unhealthy

  - Error: "Docker daemon unreachable: Cannot connect to the Docker daemon at unix:///var/run/docker.sock"
  - Cause: Server runs inside Docker without access to host Docker socket
  - **Fix**: Mount `/var/run/docker.sock:/var/run/docker.sock` in docker-compose.yml

- **Firecracker**: Failed to initialize

  - Error: "KVM not available: stat /dev/kvm: no such file or directory"
  - Cause: Needs KVM access
  - **Fix**: Add `--device=/dev/kvm` to docker-compose.yml (if host supports KVM)

- **E2B**: Not registered
  - Cause: No E2B_API_KEY configured
  - **Fix**: Add `E2B_API_KEY: 'your-key'` to environment variables

## API Endpoints Tested

### List Providers

```bash
curl -s http://localhost:3002/api/v1/agent/workspaces/providers \
  -H 'X-API-Key: 4334d35abf06be28271d7c5cd5a1cf02a6e9b4c2753db816e48419330e023060'
```

**Response:**

```json
[
  {
    "name": "gVisor (Docker)",
    "type": "gvisor",
    "healthy": false,
    "message": "Docker daemon unreachable: Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?",
    "capabilities": {
      "name": "gVisor (Docker)",
      "supports_persistence": true,
      "supports_snapshots": true,
      "supports_warm_pool": true,
      "requires_kvm": false,
      "estimated_startup_ms": 50,
      "provider_type": "gvisor"
    },
    "active_count": 0
  }
]
```

## Architecture Insights

### Workspace Module Components (v0.16.1)

- **Orchestrator**: Provider selection and fallback logic
- **Providers**: Firecracker (preferred), gVisor (fallback), E2B (managed)
- **Cleanup Job**: Runs every 1 hour, max 10 concurrent workspaces
- **Warm Pool**: Disabled by default (WORKSPACE_WARM_POOL_SIZE=0)
- **MCP Hosting**: Initialized for serving MCP servers from workspaces

### Authentication Requirements

- **Read operations** (`GET /workspaces`, `/providers`, `/workspaces/:id`): Requires `admin:read` scope
- **Write operations** (`POST /workspaces`, `DELETE`, etc.): Requires `admin:write` scope
- **Tool operations** (`POST /workspaces/:id/bash`, `/read`, `/write`, etc.): Requires `admin:write` + audit logging
- **Standalone mode**: Use `X-API-Key` header with STANDALONE_API_KEY value

### Configuration Changes for Next Release

- ✅ `ENABLE_AGENT_WORKSPACES` default changed from `false` → `true` in `/root/emergent/apps/server-go/internal/config/config.go:278`

## Next Steps for Full Functionality

### Option 1: Enable gVisor Provider (Recommended for Testing)

```yaml
# In /root/.emergent/docker/docker-compose.yml
services:
  server:
    # ... existing config ...
    volumes:
      - emergent_cli_config:/root/.emergent
      - /var/run/docker.sock:/var/run/docker.sock # Add this line
```

Then restart: `cd /root/.emergent/docker && docker-compose up -d server`

### Option 2: Enable E2B Provider (Managed Cloud)

```yaml
# In /root/.emergent/docker/docker-compose.yml
services:
  server:
    environment:
      # ... existing vars ...
      E2B_API_KEY: 'your-e2b-api-key-here'
```

Get an API key from: https://e2b.dev

### Option 3: Enable Firecracker (Production)

Requires KVM support on host:

```yaml
services:
  server:
    devices:
      - /dev/kvm:/dev/kvm
```

## Test Script for Future Use

Once a provider is configured, use this test script:

```bash
#!/bin/bash
# Full workspace workflow test
API_BASE="http://localhost:3002"
API_KEY="4334d35abf06be28271d7c5cd5a1cf02a6e9b4c2753db816e48419330e023060"

echo "📋 Listing providers..."
curl -s "$API_BASE/api/v1/agent/workspaces/providers" -H "X-API-Key: $API_KEY"

echo -e "\n\n🚀 Creating workspace..."
WORKSPACE=$(curl -s "$API_BASE/api/v1/agent/workspaces" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "container_type": "agent_workspace",
    "provider": "auto",
    "repository_url": "https://github.com/emergent-company/emergent.git",
    "branch": "main",
    "deployment_mode": "self-hosted",
    "resource_limits": {
      "memory_mb": 2048,
      "cpu_count": 2,
      "disk_mb": 4096
    }
  }')

WORKSPACE_ID=$(echo "$WORKSPACE" | jq -r '.id')
echo "Created workspace: $WORKSPACE_ID"

echo -e "\n📁 Running bash command..."
curl -s "$API_BASE/api/v1/agent/workspaces/$WORKSPACE_ID/bash" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"command": "ls -la", "workdir": "/workspace", "timeout_ms": 10000}'

echo -e "\n🧹 Cleaning up..."
curl -s -X DELETE "$API_BASE/api/v1/agent/workspaces/$WORKSPACE_ID" \
  -H "X-API-Key: $API_KEY"

echo -e "\n✨ Test completed!"
```

## Files Modified

1. `/root/emergent/apps/server-go/internal/config/config.go:278`

   - Changed `envDefault:"false"` to `envDefault:"true"` for ENABLE_AGENT_WORKSPACES

2. `/root/.emergent/docker/docker-compose.yml` (on mcj-emergent server)
   - Added `ENABLE_AGENT_WORKSPACES: 'true'` to server environment

## Recommendations

1. **For development/testing**: Mount Docker socket to enable gVisor provider
2. **For production**: Use Firecracker provider with KVM support
3. **For managed environments**: Use E2B provider (no infrastructure needed)
4. **Security**: Consider network isolation via WORKSPACE_NETWORK_NAME
5. **Resource limits**: Adjust WORKSPACE_MAX_CONCURRENT based on host capacity

## References

- Release: https://github.com/emergent-company/emergent/releases/tag/v0.16.1
- Workspace implementation: PR #40
- Server URL: http://localhost:3002 (mcj-emergent)
- Standalone API Key: `4334d35abf06be28271d7c5cd5a1cf02a6e9b4c2753db816e48419330e023060`
