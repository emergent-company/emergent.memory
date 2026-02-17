## ADDED Requirements

### Requirement: Create agent workspace

The system SHALL create an isolated workspace container for an agent session upon request. The workspace MUST be assigned a unique identifier and tracked in the `kb.agent_workspaces` database table with `container_type = 'agent_workspace'`.

#### Scenario: Successful workspace creation with default settings

- **WHEN** an agent requests a new workspace without specifying provider or resource limits
- **THEN** the system creates a workspace using the default provider selection (orchestrator decides) with default resource limits (2 CPU, 4GB RAM, 10GB disk), returns a workspace ID, and sets status to `ready`

#### Scenario: Workspace creation with explicit provider

- **WHEN** an agent requests a workspace with `provider = "firecracker"`
- **THEN** the system creates the workspace using Firecracker microVM exclusively, returning an error if Firecracker is unavailable (no fallback when explicitly requested)

#### Scenario: Workspace creation with warm pool hit

- **WHEN** an agent requests a workspace and the warm pool has a pre-booted container available
- **THEN** the system assigns the warm container to the agent within 150ms, sets status to `ready`, and replenishes the warm pool asynchronously

#### Scenario: Workspace creation with warm pool miss

- **WHEN** an agent requests a workspace and the warm pool is empty
- **THEN** the system creates a new container from scratch (cold start), returns the workspace within the provider's startup time, and logs a warm pool miss metric

#### Scenario: Workspace creation exceeds concurrent limit

- **WHEN** an agent requests a workspace but the maximum concurrent workspace limit (configurable, default 10) has been reached
- **THEN** the system rejects the request with a clear error indicating the limit has been reached and suggests waiting or destroying unused workspaces

### Requirement: Create MCP server container

The system SHALL create a persistent, isolated container for hosting an MCP server upon registration. The container MUST be tracked in `kb.agent_workspaces` with `container_type = 'mcp_server'` and `lifecycle = 'persistent'`.

#### Scenario: Successful MCP server registration

- **WHEN** an administrator registers an MCP server with an image name and stdio bridge configuration
- **THEN** the system creates a persistent container from the specified image, establishes a stdio bridge if configured, sets status to `ready`, and sets `expires_at` to NULL (no TTL)

#### Scenario: MCP server auto-start on Emergent boot

- **WHEN** the Emergent server starts up
- **THEN** the system queries `kb.agent_workspaces` for all entries with `container_type = 'mcp_server'`, `lifecycle = 'persistent'`, and starts each container, establishing stdio bridges as configured

### Requirement: Destroy workspace

The system SHALL destroy a workspace or MCP server container and release all associated resources when explicitly requested.

#### Scenario: Successful workspace destruction

- **WHEN** a destroy request is received for a workspace with status `ready` or `stopped`
- **THEN** the system terminates the container via the provider, releases storage volumes (unless snapshot requested), removes the database record, and returns confirmation

#### Scenario: Destroy running workspace with active operations

- **WHEN** a destroy request is received for a workspace that has an in-progress tool operation (bash command running)
- **THEN** the system sends SIGTERM to running processes, waits up to 10 seconds for graceful shutdown, then force-destroys the container

#### Scenario: Destroy non-existent workspace

- **WHEN** a destroy request is received for a workspace ID that does not exist
- **THEN** the system returns a 404 error with a clear message

### Requirement: Stop and resume workspace

The system SHALL support stopping a workspace without destroying it, preserving its state for later resumption.

#### Scenario: Stop a running workspace

- **WHEN** a stop request is received for a workspace with status `ready`
- **THEN** the system pauses the container (preserving filesystem state), sets status to `stopped`, and updates `last_used_at`

#### Scenario: Resume a stopped workspace

- **WHEN** a resume request is received for a workspace with status `stopped`
- **THEN** the system resumes the paused container, sets status to `ready`, and the workspace filesystem reflects the state at the time of stopping

### Requirement: Workspace TTL and auto-cleanup

The system SHALL automatically clean up expired workspaces based on TTL (time-to-live) to prevent resource leaks.

#### Scenario: Workspace expires after TTL

- **WHEN** a workspace's `expires_at` timestamp has passed and no operations are in progress
- **THEN** the system destroys the workspace, releases resources, and logs the cleanup action

#### Scenario: MCP servers are exempt from TTL

- **WHEN** the cleanup job runs
- **THEN** it skips all entries with `container_type = 'mcp_server'` and `lifecycle = 'persistent'` (NULL `expires_at`)

#### Scenario: Active workspace TTL extension

- **WHEN** an agent performs any tool operation on a workspace
- **THEN** the system updates `last_used_at` and extends `expires_at` by the configured TTL duration (default 30 days)

### Requirement: Workspace resource limits

The system SHALL enforce resource limits on every workspace and MCP server container to prevent resource exhaustion.

#### Scenario: CPU limit enforcement

- **WHEN** a workspace process attempts to use more CPU than allocated
- **THEN** the provider throttles the process (cgroups for Firecracker/gVisor, E2B limits) without killing it

#### Scenario: Memory limit enforcement

- **WHEN** a workspace process exceeds the allocated memory limit
- **THEN** the provider's OOM killer terminates the offending process and the system logs the event, but the workspace container itself continues running

#### Scenario: Disk quota enforcement

- **WHEN** a workspace's filesystem usage exceeds the allocated disk limit
- **THEN** subsequent write operations fail with ENOSPC errors while read operations continue functioning

### Requirement: Warm pool management

The system SHALL maintain a configurable pool of pre-booted workspace containers to minimize cold start latency.

#### Scenario: Warm pool initialization on server start

- **WHEN** the Emergent server starts and warm pool is configured (size > 0)
- **THEN** the system pre-creates the specified number of containers using the default provider, ready for immediate assignment

#### Scenario: Warm pool replenishment after assignment

- **WHEN** a warm container is assigned to an agent workspace
- **THEN** the system asynchronously creates a replacement container to maintain the configured pool size

#### Scenario: Warm pool size adjustment

- **WHEN** an administrator changes the warm pool size via configuration
- **THEN** the system adjusts the pool by creating or destroying containers to match the new size within 60 seconds

### Requirement: MCP server restart on crash

The system SHALL automatically restart MCP server containers that crash or exit unexpectedly.

#### Scenario: MCP server process exits with error

- **WHEN** an MCP server container's main process exits with a non-zero exit code
- **THEN** the system restarts the container within 5 seconds, re-establishes the stdio bridge, and logs the crash event

#### Scenario: MCP server crash loop detection

- **WHEN** an MCP server crashes more than 3 times within 60 seconds
- **THEN** the system applies exponential backoff (5s, 15s, 45s) before the next restart attempt and emits a health warning alert

#### Scenario: MCP server graceful shutdown on Emergent stop

- **WHEN** the Emergent server receives a shutdown signal
- **THEN** the system sends SIGTERM to all MCP server containers, waits up to 30 seconds for graceful shutdown, then force-kills remaining containers
