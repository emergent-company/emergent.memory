# Provider Selection Guide

The agent workspace system supports three providers, each with different trade-offs. The system automatically selects the best available provider, but you can override the default.

## Provider Comparison

| Feature          | gVisor              | Firecracker         | E2B                 |
| ---------------- | ------------------- | ------------------- | ------------------- |
| **Isolation**    | Application kernel  | Hardware (microVM)  | Hardware (cloud VM) |
| **Startup time** | ~50ms               | ~125ms              | ~150ms              |
| **KVM required** | No                  | Yes                 | No                  |
| **Self-hosted**  | Yes                 | Yes                 | No (managed)        |
| **Persistence**  | Docker volumes      | Block devices       | Ephemeral only      |
| **Snapshots**    | Volume copy         | Block device clone  | Not supported       |
| **Warm pool**    | Yes                 | Yes                 | No                  |
| **Platform**     | Linux (Docker)      | Linux (bare metal)  | Any (API)           |
| **Cost**         | Infrastructure only | Infrastructure only | Per-minute billing  |
| **License**      | Apache 2.0          | Apache 2.0          | Apache 2.0          |

## Automatic Selection Logic

When `WORKSPACE_DEFAULT_PROVIDER` is set to `auto` or left empty, the system selects providers using this priority:

1. **Firecracker** — if KVM is available and the provider is healthy
2. **gVisor** — cross-platform fallback, always available on Linux with Docker
3. **E2B** — if configured (`E2B_API_KEY` set) and other providers are unhealthy

### Container Type Routing

The system also considers the container type:

- **Agent workspaces** prefer Firecracker (stronger isolation for arbitrary code execution)
- **MCP servers** prefer gVisor (lower overhead for long-running daemon processes)

### Fallback Behavior

If the selected provider fails during workspace creation:

1. The system tries the next provider in priority order
2. If the request explicitly specified a provider (e.g., `"provider": "firecracker"`), no fallback is attempted
3. Failed providers are marked unhealthy and excluded from selection for 30 seconds

## When to Use Each Provider

### gVisor (Default)

**Best for:** Most deployments, development, testing, MCP servers

- Works on any Linux host with Docker
- No special hardware requirements
- Falls back to standard Docker runtime if `runsc` is not installed
- Lowest resource overhead
- Supports persistence and snapshots via Docker volumes

### Firecracker

**Best for:** Production multi-tenant, high-security, untrusted code execution

- Hardware-level VM isolation (each workspace is a microVM)
- Complete kernel separation between workspaces
- Requires bare-metal Linux with KVM (`/dev/kvm`)
- Does not work in most cloud VMs (unless nested virtualization is enabled)
- Higher resource overhead per workspace

### E2B

**Best for:** Quick evaluation, no-infrastructure-needed, cloud-native deployments

- No local infrastructure to manage
- Managed by E2B (cloud service)
- Per-minute billing
- No persistence (workspaces are ephemeral)
- Requires internet access and E2B API key
- Limited to E2B's available templates and resource configurations

## Configuration Examples

### Development (minimal setup)

```env
ENABLE_AGENT_WORKSPACES=true
WORKSPACE_DEFAULT_PROVIDER=gvisor
WORKSPACE_MAX_CONCURRENT=5
WORKSPACE_WARM_POOL_SIZE=0
```

### Production (self-hosted with gVisor)

```env
ENABLE_AGENT_WORKSPACES=true
WORKSPACE_DEFAULT_PROVIDER=gvisor
WORKSPACE_MAX_CONCURRENT=20
WORKSPACE_WARM_POOL_SIZE=3
WORKSPACE_DEFAULT_CPU=2
WORKSPACE_DEFAULT_MEMORY=4G
WORKSPACE_NETWORK_NAME=workspace_net
```

### Production (self-hosted with Firecracker)

```env
ENABLE_AGENT_WORKSPACES=true
WORKSPACE_DEFAULT_PROVIDER=firecracker
WORKSPACE_MAX_CONCURRENT=10
WORKSPACE_WARM_POOL_SIZE=2
WORKSPACE_DEFAULT_CPU=2
WORKSPACE_DEFAULT_MEMORY=4G
```

### Cloud (E2B managed)

```env
ENABLE_AGENT_WORKSPACES=true
WORKSPACE_DEFAULT_PROVIDER=e2b
E2B_API_KEY=e2b_xxx
WORKSPACE_MAX_CONCURRENT=20
```
