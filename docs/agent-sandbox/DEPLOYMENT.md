# Agent Sandbox Deployment Guide

This guide covers deploying the agent sandbox infrastructure for self-hosted Emergent installations.

## Quick Start

1. Set `ENABLE_AGENT_SANDBOXES=true` in your environment
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

| Variable                         | Default  | Description                                     |
| -------------------------------- | -------- | ----------------------------------------------- |
| `ENABLE_AGENT_SANDBOXES`         | `false`  | Master switch for all sandbox functionality     |
| `WORKSPACE_DEFAULT_PROVIDER`     | `gvisor` | Default provider: `gvisor`, `firecracker`, `e2b` |
| `WORKSPACE_MAX_CONCURRENT`       | `10`     | Maximum simultaneous active sandboxes           |
| `WORKSPACE_DEFAULT_TTL_DAYS`     | `30`     | Days before ephemeral sandboxes are cleaned up  |
| `WORKSPACE_CLEANUP_INTERVAL_MIN` | `60`     | Minutes between cleanup scans                   |
| `WORKSPACE_ALERT_THRESHOLD_PCT`  | `80`     | Resource usage warning threshold (%)            |
| `WORKSPACE_PERSISTENT_IDLE_TTL_DAYS` | `0`  | Days of inactivity after which a hosted MCP server is reclaimed (`0` = disabled) |

### Resource Defaults

| Variable                   | Default | Description                         |
| -------------------------- | ------- | ----------------------------------- |
| `WORKSPACE_DEFAULT_CPU`    | `2`     | CPU cores per sandbox               |
| `WORKSPACE_DEFAULT_MEMORY` | `4G`    | Memory limit per sandbox            |
| `WORKSPACE_DEFAULT_DISK`   | `10G`   | Disk limit per sandbox              |
| `WORKSPACE_WARM_POOL_SIZE` | `0`     | Pre-booted containers (0 = disabled) |

### Network Isolation

| Variable                 | Default   | Description                            |
| ------------------------ | --------- | -------------------------------------- |
| `WORKSPACE_NETWORK_NAME` | _(empty)_ | Docker network for sandbox containers  |

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
                        │   │ │S1│ │S2│ .. │  Sandbox         │
                        │   │ └──┘ └──┘    │  Containers      │
                        │   └──────────────┘                  │
                        └─────────────────────────────────────┘
```

- **workspace_net** has `enable_icc=false`: sandbox containers cannot communicate with each other
- The Emergent server bridges requests between the default network and sandbox network
- Sandboxes have outbound internet access (for `git clone`, package installation)

## Resource Planning

### Per-Sandbox Defaults

| Resource | Default | Notes                                        |
| -------- | ------- | -------------------------------------------- |
| CPU      | 2 cores | Can be overridden per sandbox                |
| Memory   | 4 GB    | Hard limit, OOM kill if exceeded             |
| Disk     | 10 GB   | Via Docker volume (no hard quota by default) |

### Host Requirements

| Concurrent Sandboxes | Recommended Host   |
| -------------------- | ------------------ |
| 1-5                  | 8 CPU, 32 GB RAM   |
| 5-10                 | 16 CPU, 64 GB RAM  |
| 10-20                | 32 CPU, 128 GB RAM |

### Warm Pool Sizing

The warm pool pre-boots containers for faster sandbox creation (~50ms vs ~2-5s):

| Setting                      | Effect                           |
| ---------------------------- | -------------------------------- |
| `WORKSPACE_WARM_POOL_SIZE=0` | Disabled (default)               |
| `WORKSPACE_WARM_POOL_SIZE=2` | Good for low-traffic             |
| `WORKSPACE_WARM_POOL_SIZE=5` | Production with moderate traffic |

Warm pool containers consume resources even when idle. Size according to your expected concurrent usage.

## Health Monitoring

The system performs automatic health checks:

- **Provider health:** Re-checked every 30 seconds for the process lifetime. Every known provider is always listed with an availability reason; unavailable providers are never selected
- **Cleanup job:** Runs hourly (configurable) to destroy expired sandboxes
- **Resource alerts:** Warning logged when aggregate usage exceeds the threshold

### Health API

```bash
# Check provider status
curl -H "Authorization: Bearer $TOKEN" \
  https://api.emergent-company.ai/api/v1/agent/sandboxes/providers
```

## Troubleshooting

### Sandbox creation fails

1. Check Docker daemon is running: `docker info`
2. Check provider health via the API
3. Check concurrent sandbox limit: reduce `WORKSPACE_MAX_CONCURRENT` or destroy unused sandboxes
4. Check server logs for provider-specific errors

### Sandbox container not starting

1. Check Docker has sufficient resources: `docker system df`
2. Check if the base image can be pulled: `docker pull ubuntu:22.04`
3. If using gVisor, check runtime availability: `docker run --runtime=runsc --rm hello-world`

### MCP servers not auto-starting

1. Verify `ENABLE_AGENT_SANDBOXES=true`
2. Check server startup logs for "failed to auto-start MCP servers"
3. Verify MCP server images are accessible

## Teardown Guarantee

Every agent run that provisions a sandbox tears it down exactly once, on every
exit path: normal completion, error return, context cancellation, and panic.
Teardown is bound to the run's lifetime by the executor and does not depend on
the caller invoking any cleanup function. A second teardown for the same run is
a no-op.

On startup, any sandbox row whose owning run is no longer active (the process
crashed or was restarted mid-run) is transitioned out of its non-stopped state
and logged, so its container and volume become eligible for reclamation rather
than lingering until the 30-day ephemeral TTL. Runs still queued or executing
are left untouched, and recovery is idempotent across repeated restarts.

## Persistent MCP Idle Reclamation (opt-in)

Persistent hosted MCP servers are not governed by the ephemeral TTL. By default
they live until an operator explicitly deletes them (`DELETE /api/v1/mcp/hosted/:id`).

Set `WORKSPACE_PERSISTENT_IDLE_TTL_DAYS` to a positive number of days to have
the cleanup cycle destroy hosted MCP servers whose `last_used_at` is older than
that window. Servers in `creating`/`stopping` states are never reclaimed, a
failed container destroy leaves the row in place for retry, and the row is
deleted only after a successful destroy. `0` (the default) keeps the current
persistent semantics — nothing is reclaimed by idleness.

`GET /api/v1/mcp/hosted` exposes `last_used_at`, so you can identify idle
servers before enabling the policy. See [OPERATIONS.md](./OPERATIONS.md).
