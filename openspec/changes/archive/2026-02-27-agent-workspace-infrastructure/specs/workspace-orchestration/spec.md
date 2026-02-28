## ADDED Requirements

### Requirement: Provider interface abstraction

The system SHALL define a common provider interface that all sandbox providers (Firecracker, E2B, gVisor) MUST implement, enabling the orchestrator to treat them interchangeably.

#### Scenario: All providers implement the same interface

- **WHEN** a new provider is added to the system
- **THEN** it MUST implement the `WorkspaceProvider` interface with methods: `Create`, `Destroy`, `Stop`, `Resume`, `Exec`, `ReadFile`, `WriteFile`, `Health`, and `Capabilities`

#### Scenario: Provider capabilities reporting

- **WHEN** the orchestrator queries a provider's capabilities
- **THEN** the provider reports: supported features (persistence, snapshots, warm pool), platform requirements (KVM, Docker), startup latency estimate, and current resource availability

### Requirement: Automatic provider selection

The system SHALL automatically select the best provider when `provider = "auto"` is specified (or no provider is specified).

#### Scenario: Self-hosted deployment with KVM available

- **WHEN** `provider = "auto"`, `deployment_mode = "self-hosted"`, and Firecracker is healthy (KVM available)
- **THEN** the orchestrator selects Firecracker (best isolation + persistence for self-hosted)

#### Scenario: Self-hosted deployment without KVM

- **WHEN** `provider = "auto"`, `deployment_mode = "self-hosted"`, and Firecracker reports KVM unavailable
- **THEN** the orchestrator selects gVisor (cross-platform fallback)

#### Scenario: Managed deployment with E2B configured

- **WHEN** `provider = "auto"`, `deployment_mode = "managed"`, and E2B API key is configured
- **THEN** the orchestrator selects E2B (managed infrastructure, no local resource overhead)

#### Scenario: Managed deployment without E2B

- **WHEN** `provider = "auto"`, `deployment_mode = "managed"`, and no E2B API key is configured
- **THEN** the orchestrator falls back to the self-hosted selection logic (Firecracker > gVisor)

#### Scenario: No provider specified defaults to auto

- **WHEN** a workspace creation request omits the `provider` field entirely
- **THEN** the system behaves as if `provider = "auto"` was specified

### Requirement: Provider fallback chain

The system SHALL implement a fallback chain when the preferred provider fails to create a workspace.

#### Scenario: Firecracker fails, falls back to gVisor

- **WHEN** the orchestrator selects Firecracker but creation fails (KVM error, resource exhaustion)
- **THEN** the system automatically retries with gVisor, logs the fallback event, and records the actual provider used in the workspace metadata

#### Scenario: E2B fails, falls back to Firecracker then gVisor

- **WHEN** the orchestrator selects E2B but creation fails (API error, quota exceeded)
- **THEN** the system retries with Firecracker (if KVM available), then gVisor, logging each fallback attempt

#### Scenario: Explicit provider request does NOT fallback

- **WHEN** a workspace creation request explicitly specifies `provider = "firecracker"` (not "auto") and Firecracker fails
- **THEN** the system returns an error without attempting fallback, since the caller explicitly chose a provider

#### Scenario: All providers fail

- **WHEN** the orchestrator exhausts the fallback chain and no provider can create a workspace
- **THEN** the system returns a comprehensive error listing each provider and its failure reason

### Requirement: Provider health monitoring

The system SHALL continuously monitor the health of all configured providers and factor health into selection decisions.

#### Scenario: Healthy provider is preferred

- **WHEN** the orchestrator selects a provider for a new workspace
- **THEN** it checks the provider's health status and only selects providers reporting healthy status

#### Scenario: Provider health check frequency

- **WHEN** the Emergent server is running
- **THEN** the system checks each provider's health every 30 seconds and caches the result

#### Scenario: Provider becomes unhealthy

- **WHEN** a provider health check fails (e.g., Firecracker manager unreachable)
- **THEN** the system marks the provider as unhealthy, removes it from the auto-selection pool, and logs a warning; it is re-added when a subsequent health check passes

#### Scenario: Health status API

- **WHEN** a status request is made to `/api/v1/agent/workspaces/providers`
- **THEN** the system returns the health status of all configured providers including: name, status (healthy/unhealthy), last check timestamp, active workspaces count, and available resources

### Requirement: Provider-specific container type routing

The system SHALL prefer different providers for different container types (agent workspace vs MCP server).

#### Scenario: Agent workspace defaults to Firecracker

- **WHEN** a workspace is created with `container_type = "agent_workspace"` and `provider = "auto"`
- **THEN** the orchestrator prefers Firecracker for best isolation and performance (short-lived, compute-heavy)

#### Scenario: MCP server defaults to gVisor

- **WHEN** a workspace is created with `container_type = "mcp_server"` and `provider = "auto"`
- **THEN** the orchestrator prefers gVisor for lower resource overhead (long-lived, lightweight processes)

### Requirement: Warm pool provider coordination

The system SHALL coordinate warm pool management with the provider selection logic.

#### Scenario: Warm pool uses default provider

- **WHEN** the warm pool is initialized on server start
- **THEN** the system creates warm containers using the default provider for the current deployment mode and platform

#### Scenario: Warm pool container matches request

- **WHEN** a workspace request arrives and the warm pool has pre-created containers
- **THEN** the system assigns a warm container only if the container's provider matches what the orchestrator would select for this request (no provider mismatch)

#### Scenario: Warm pool miss triggers cold start with correct provider

- **WHEN** a workspace request arrives but no suitable warm container is available (pool empty or provider mismatch)
- **THEN** the orchestrator creates a new container using the selected provider (cold start) and logs the warm pool miss reason
