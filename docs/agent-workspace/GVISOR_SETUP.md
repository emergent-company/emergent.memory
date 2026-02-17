# gVisor Runtime Setup for Agent Workspaces

gVisor provides sandbox-level isolation for agent workspace containers using the `runsc` OCI runtime. When gVisor is not available, the system falls back to the standard Docker runtime with a security warning.

## Prerequisites

- Docker Engine 20.10+ (Docker Desktop or Docker CE)
- Linux host (gVisor `runsc` requires Linux; macOS/Windows use Docker Desktop which does not support custom runtimes)

## Installing gVisor (runsc)

### Ubuntu/Debian

```bash
# Add the gVisor APT repository
sudo apt-get update && sudo apt-get install -y apt-transport-https ca-certificates curl gnupg

curl -fsSL https://gvisor.dev/archive.key | sudo gpg --dearmor -o /usr/share/keyrings/gvisor-archive-keyring.gpg

echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/gvisor-archive-keyring.gpg] https://storage.googleapis.com/gvisor/releases release main" | sudo tee /etc/apt/sources.list.d/gvisor.list > /dev/null

sudo apt-get update && sudo apt-get install -y runsc
```

### Manual install (any Linux)

```bash
ARCH=$(uname -m)
URL="https://storage.googleapis.com/gvisor/releases/release/latest/${ARCH}"
wget "${URL}/runsc" "${URL}/runsc.sha512"
sha512sum -c runsc.sha512
chmod a+rx runsc
sudo mv runsc /usr/local/bin/
```

## Configuring Docker to Use gVisor

Add `runsc` as an available runtime in `/etc/docker/daemon.json`:

```json
{
  "runtimes": {
    "runsc": {
      "path": "/usr/local/bin/runsc",
      "runtimeArgs": ["--platform=systrap"]
    }
  }
}
```

Then restart Docker:

```bash
sudo systemctl restart docker
```

### Verify Installation

```bash
# Check that runsc is available as a Docker runtime
docker info | grep -i runtime

# Run a test container with gVisor
docker run --runtime=runsc --rm hello-world
```

## gVisor Runtime Options

The `--platform=systrap` argument (recommended) uses ptrace-based interception. Other options:

| Platform  | Description                            | Performance |
| --------- | -------------------------------------- | ----------- |
| `systrap` | ptrace-based syscall interception      | Good        |
| `kvm`     | Hardware virtualization (requires KVM) | Best        |

For KVM platform (if `/dev/kvm` is available):

```json
{
  "runtimes": {
    "runsc": {
      "path": "/usr/local/bin/runsc",
      "runtimeArgs": ["--platform=kvm"]
    }
  }
}
```

## How Emergent Uses gVisor

When `ENABLE_AGENT_WORKSPACES=true`:

1. On startup, the workspace provider checks if the `runsc` runtime is registered with Docker
2. If available, all agent workspace containers are created with `--runtime=runsc`
3. If not available, containers use the standard Docker runtime (with a warning in logs)

### Environment Variables

| Variable                     | Default   | Description                                       |
| ---------------------------- | --------- | ------------------------------------------------- |
| `ENABLE_AGENT_WORKSPACES`    | `false`   | Enable workspace endpoints and lifecycle          |
| `WORKSPACE_DEFAULT_PROVIDER` | `gvisor`  | Default sandbox provider                          |
| `WORKSPACE_DEFAULT_CPU`      | `2`       | Default CPU limit per workspace                   |
| `WORKSPACE_DEFAULT_MEMORY`   | `4G`      | Default memory limit per workspace                |
| `WORKSPACE_DEFAULT_DISK`     | `10G`     | Default disk limit per workspace                  |
| `WORKSPACE_MAX_CONCURRENT`   | `10`      | Maximum concurrent active workspaces              |
| `WORKSPACE_WARM_POOL_SIZE`   | `0`       | Pre-booted container pool size (0 = disabled)     |
| `WORKSPACE_DEFAULT_TTL_DAYS` | `30`      | Default TTL for ephemeral workspaces              |
| `WORKSPACE_NETWORK_NAME`     | _(empty)_ | Docker network for container isolation            |
| `E2B_API_KEY`                | _(empty)_ | API key for E2B managed sandbox provider          |
| `GITHUB_APP_ENCRYPTION_KEY`  | _(empty)_ | AES-256 encryption key for GitHub App credentials |

### Network Isolation

When `WORKSPACE_NETWORK_NAME` is set (e.g., `workspace_net`), workspace containers are attached to a dedicated Docker network. The `docker-compose.dev.yml` defines a `workspace_net` bridge network with inter-container communication disabled (`enable_icc=false`), preventing workspace containers from reaching infrastructure services directly.

## Troubleshooting

### "gVisor runtime (runsc) not found" warning

This means Docker does not have `runsc` registered. Check:

```bash
# Verify runsc is installed
which runsc

# Verify daemon.json is correct
cat /etc/docker/daemon.json

# Check Docker knows about the runtime
docker info --format '{{.Runtimes}}'
```

### Container creation fails with gVisor

Some operations are not supported under gVisor's syscall filter. If a workspace image requires unsupported syscalls:

1. Check the gVisor compatibility docs: https://gvisor.dev/docs/user_guide/compatibility/
2. Consider using `--platform=kvm` for broader compatibility
3. As a last resort, set `WORKSPACE_DEFAULT_PROVIDER=gvisor` with standard runtime fallback (automatic)

### Docker Desktop (macOS/Windows)

Docker Desktop does not support custom OCI runtimes. gVisor is only available on Linux hosts with Docker Engine. On macOS/Windows, the system automatically falls back to the standard Docker runtime.
