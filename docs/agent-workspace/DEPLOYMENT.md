# Agent Workspace Deployment Guide

This guide covers deploying the agent workspace infrastructure for self-hosted Emergent installations.

## Quick Start

1. Set `ENABLE_AGENT_WORKSPACES=true` in your environment
2. Ensure Docker is running on the host
3. Restart the Emergent server

The system defaults to the **gVisor provider** which works on any Linux host with Docker. No additional setup is required for basic functionality.

## Provider Setup

### gVisor (Default — Recommended)

gVisor provides lightweight container isolation using the `runsc` OCI runtime. It works on any Linux host and falls back to the standard Docker runtime automatically.

**Setup:** See [GVISOR_SETUP.md](./GVISOR_SETUP.md) for detailed installation instructions.

**When to use:** Default for all deployments. Best balance of security, performance, and compatibility.

### Firecracker (KVM Required)

Firecracker provides hardware-level VM isolation using microVMs. Requires bare-metal Linux with KVM support.

**Requirements:**

- Linux host with `/dev/kvm` access
- Bare metal or nested virtualization enabled
- At least 4GB RAM beyond base system requirements

**Setup:**

1. Uncomment the `firecracker-manager` service in `docker-compose.dev.yml`
2. Set `WORKSPACE_DEFAULT_PROVIDER=firecracker`
3. Ensure `/dev/kvm` is accessible

**When to use:** High-security deployments where hardware isolation is required. Multi-tenant environments where container escape is a concern.

### E2B (Managed)

E2B provides managed sandboxes via their cloud API. No local infrastructure required.

**Requirements:**

- E2B account and API key
- Internet access from the Emergent server

**Setup:**

1. Set `E2B_API_KEY=your-api-key`
2. Set `WORKSPACE_DEFAULT_PROVIDER=e2b`

**When to use:** When you don't want to manage sandbox infrastructure. Quick evaluation and testing.

## Environment Variables

### Core Configuration

| Variable                         | Default  | Description                                      |
| -------------------------------- | -------- | ------------------------------------------------ |
| `ENABLE_AGENT_WORKSPACES`        | `false`  | Master switch for all workspace functionality    |
| `WORKSPACE_DEFAULT_PROVIDER`     | `gvisor` | Default provider: `gvisor`, `firecracker`, `e2b` |
| `WORKSPACE_MAX_CONCURRENT`       | `10`     | Maximum simultaneous active workspaces           |
| `WORKSPACE_DEFAULT_TTL_DAYS`     | `30`     | Days before ephemeral workspaces are cleaned up  |
| `WORKSPACE_CLEANUP_INTERVAL_MIN` | `60`     | Minutes between cleanup scans                    |
| `WORKSPACE_ALERT_THRESHOLD_PCT`  | `80`     | Resource usage warning threshold (%)             |

### Resource Defaults

| Variable                   | Default | Description                          |
| -------------------------- | ------- | ------------------------------------ |
| `WORKSPACE_DEFAULT_CPU`    | `2`     | CPU cores per workspace              |
| `WORKSPACE_DEFAULT_MEMORY` | `4G`    | Memory limit per workspace           |
| `WORKSPACE_DEFAULT_DISK`   | `10G`   | Disk limit per workspace             |
| `WORKSPACE_WARM_POOL_SIZE` | `0`     | Pre-booted containers (0 = disabled) |

### Network Isolation

| Variable                 | Default   | Description                             |
| ------------------------ | --------- | --------------------------------------- |
| `WORKSPACE_NETWORK_NAME` | _(empty)_ | Docker network for workspace containers |

### Provider-Specific

| Variable                    | Default   | Description                                      |
| --------------------------- | --------- | ------------------------------------------------ |
| `E2B_API_KEY`               | _(empty)_ | E2B API key for managed provider                 |
| `GITHUB_APP_ENCRYPTION_KEY` | _(empty)_ | AES-256 key for GitHub App credential encryption |

## Network Architecture

```text
                        ┌─────────────────────────────────────┐
                        │         Docker Host                  │
                        │                                      │
  ┌──────────────┐      │   ┌──────────────┐                  │
  │  Emergent    │◄────►│   │  Emergent    │                  │
  │  Admin UI    │      │   │  Server      │                  │
  └──────────────┘      │   └──────┬───────┘                  │
                        │          │                           │
                        │   ┌──────┴───────┐                  │
                        │   │ Default Net  │  (db, etc.)      │
                        │   └──────────────┘                  │
                        │          │                           │
                        │   ┌──────┴───────┐                  │
                        │   │ workspace_net│  (isolated)      │
                        │   │  ICC=false   │                  │
                        │   ├──────────────┤                  │
                        │   │ ┌──┐ ┌──┐    │                  │
                        │   │ │W1│ │W2│ .. │  Workspace       │
                        │   │ └──┘ └──┘    │  Containers      │
                        │   └──────────────┘                  │
                        └─────────────────────────────────────┘
```

- **workspace_net** has `enable_icc=false`: workspace containers cannot communicate with each other
- The Emergent server bridges requests between the default network and workspace network
- Workspaces have outbound internet access (for `git clone`, package installation)

## Resource Planning

### Per-Workspace Defaults

| Resource | Default | Notes                                        |
| -------- | ------- | -------------------------------------------- |
| CPU      | 2 cores | Can be overridden per workspace              |
| Memory   | 4 GB    | Hard limit, OOM kill if exceeded             |
| Disk     | 10 GB   | Via Docker volume (no hard quota by default) |

### Host Requirements

| Concurrent Workspaces | Recommended Host   |
| --------------------- | ------------------ |
| 1-5                   | 8 CPU, 32 GB RAM   |
| 5-10                  | 16 CPU, 64 GB RAM  |
| 10-20                 | 32 CPU, 128 GB RAM |

### Warm Pool Sizing

The warm pool pre-boots containers for faster workspace creation (~50ms vs ~2-5s):

| Setting                      | Effect                           |
| ---------------------------- | -------------------------------- |
| `WORKSPACE_WARM_POOL_SIZE=0` | Disabled (default)               |
| `WORKSPACE_WARM_POOL_SIZE=2` | Good for low-traffic             |
| `WORKSPACE_WARM_POOL_SIZE=5` | Production with moderate traffic |

Warm pool containers consume resources even when idle. Size according to your expected concurrent usage.

## Health Monitoring

The system performs automatic health checks:

- **Provider health:** Checked every 30 seconds. Unhealthy providers are removed from the selection pool
- **Cleanup job:** Runs hourly (configurable) to destroy expired workspaces
- **Resource alerts:** Warning logged when aggregate usage exceeds the threshold

### Health API

```bash
# Check provider status
curl -H "Authorization: Bearer $TOKEN" \
  https://api.emergent-company.ai/api/v1/agent/workspaces/providers
```

## Troubleshooting

### Workspace creation fails

1. Check Docker daemon is running: `docker info`
2. Check provider health via the API
3. Check concurrent workspace limit: reduce `WORKSPACE_MAX_CONCURRENT` or destroy unused workspaces
4. Check server logs for provider-specific errors

### Workspace container not starting

1. Check Docker has sufficient resources: `docker system df`
2. Check if the base image can be pulled: `docker pull ubuntu:22.04`
3. If using gVisor, check runtime availability: `docker run --runtime=runsc --rm hello-world`

### MCP servers not auto-starting

1. Verify `ENABLE_AGENT_WORKSPACES=true`
2. Check server startup logs for "failed to auto-start MCP servers"
3. Verify MCP server images are accessible
