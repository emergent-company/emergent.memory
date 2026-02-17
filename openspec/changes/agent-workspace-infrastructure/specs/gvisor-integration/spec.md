## ADDED Requirements

### Requirement: gVisor container creation

The system SHALL create Docker containers with gVisor (runsc) runtime for lightweight, cross-platform workspace isolation.

#### Scenario: Create gVisor container with default configuration

- **WHEN** the gVisor provider receives a workspace creation request
- **THEN** the system creates a Docker container with `--runtime=runsc`, default resource limits (2 CPU, 4GB RAM, 10GB disk), a named volume for workspace storage, and achieves startup in approximately 50ms

#### Scenario: gVisor runtime not installed

- **WHEN** the gVisor provider attempts to create a container but `runsc` runtime is not registered with Docker
- **THEN** the provider returns an error "gVisor runtime not available" and the orchestrator falls back to the next provider

#### Scenario: Fallback to standard Docker runtime

- **WHEN** gVisor is not available and no other providers are available
- **THEN** the system MAY create a standard Docker container (without gVisor) as a last-resort fallback, logging a security warning that isolation is reduced

### Requirement: gVisor command execution

The system SHALL execute commands in gVisor containers using Docker exec.

#### Scenario: Execute bash command via docker exec

- **WHEN** a bash tool request targets a gVisor workspace
- **THEN** the system uses `docker exec <container_id> <command>` to execute the command, captures stdout and stderr, and returns structured results

#### Scenario: File operations via docker exec

- **WHEN** a read, write, edit, glob, or grep tool request targets a gVisor workspace
- **THEN** the system uses `docker exec` with appropriate commands (`cat`, `tee`, file manipulation) to perform the operation, translating results to the standard tool response format

### Requirement: gVisor container persistence

The system SHALL use Docker named volumes for gVisor container persistence, supporting stateful workspaces.

#### Scenario: Named volume creation

- **WHEN** a gVisor workspace is created
- **THEN** the system creates a Docker named volume `agent-workspace-{id}` and mounts it at `/workspace` inside the container

#### Scenario: Volume persists across container stop/start

- **WHEN** a gVisor workspace container is stopped and later restarted
- **THEN** the system creates a new container with the same named volume mounted, and all filesystem state is preserved

#### Scenario: Volume cleanup on workspace destroy

- **WHEN** a gVisor workspace is destroyed
- **THEN** the system removes both the container and the named volume, freeing all disk space

### Requirement: gVisor resource limits

The system SHALL enforce resource limits on gVisor containers using Docker's built-in cgroup controls.

#### Scenario: CPU limit via Docker

- **WHEN** a gVisor workspace is created with `cpu = "2"`
- **THEN** the system sets `--cpus=2` on the Docker container

#### Scenario: Memory limit via Docker

- **WHEN** a gVisor workspace is created with `memory = "4G"`
- **THEN** the system sets `--memory=4g` on the Docker container

#### Scenario: Disk quota via Docker volume

- **WHEN** a gVisor workspace is created with `disk = "10G"`
- **THEN** the system configures the volume with a size limit where supported by the Docker storage driver, or monitors disk usage and warns when approaching the limit

### Requirement: gVisor cross-platform compatibility

The system SHALL ensure gVisor-based workspaces function on platforms where Firecracker is unavailable.

#### Scenario: macOS Docker Desktop

- **WHEN** the Emergent server runs on macOS via Docker Desktop
- **THEN** gVisor containers function correctly via Docker Desktop's Linux VM, providing workspace isolation without requiring KVM

#### Scenario: Windows Docker Desktop

- **WHEN** the Emergent server runs on Windows via Docker Desktop with WSL2
- **THEN** gVisor containers function correctly via WSL2's Linux kernel, providing workspace isolation without requiring KVM

#### Scenario: Linux without KVM

- **WHEN** the Emergent server runs on Linux but the host does not have KVM enabled (e.g., nested VM, certain cloud instances)
- **THEN** gVisor containers function correctly using ptrace mode (slower but functional), and the system logs that KVM-based Firecracker is unavailable

### Requirement: gVisor as preferred MCP server provider

The system SHALL prefer gVisor for MCP server containers due to lower resource overhead for long-running processes.

#### Scenario: Default MCP server creation uses gVisor

- **WHEN** an MCP server is registered without specifying a provider
- **THEN** the system uses gVisor by default (lighter than Firecracker for persistent containers)

#### Scenario: MCP server with gVisor networking

- **WHEN** a gVisor MCP server container is created
- **THEN** the system configures Docker networking to allow the container to communicate with the Emergent server process (for stdio bridge) while isolating it from other containers
