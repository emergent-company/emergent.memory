## ADDED Requirements

### Requirement: Agent Sandbox API at /api/v1/agent/sandboxes

The system SHALL expose all agent sandbox (compute environment) management endpoints under the `/api/v1/agent/sandboxes` path prefix. The Go package SHALL be named `sandbox`, the fx module SHALL be named `sandbox`, and the primary struct SHALL be named `AgentSandbox`. The `ContainerType` constant for agent sandboxes SHALL be `"agent_sandbox"`.

#### Scenario: List sandboxes

- **WHEN** a GET request is sent to `/api/v1/agent/sandboxes`
- **THEN** the system SHALL return all `AgentSandbox` records from `kb.agent_sandboxes` visible to the caller

#### Scenario: List sandbox providers

- **WHEN** a GET request is sent to `/api/v1/agent/sandboxes/providers`
- **THEN** the system SHALL return available sandbox provider types (firecracker, e2b, gvisor)

#### Scenario: Get sandbox by ID

- **WHEN** a GET request is sent to `/api/v1/agent/sandboxes/:id`
- **THEN** the system SHALL return the `AgentSandbox` record or HTTP 404 if not found

#### Scenario: Create sandbox

- **WHEN** a POST request is sent to `/api/v1/agent/sandboxes`
- **THEN** the system SHALL provision a new compute environment, insert a row into `kb.agent_sandboxes` with `container_type = 'agent_sandbox'`, and return HTTP 201

#### Scenario: Create sandbox from snapshot

- **WHEN** a POST request is sent to `/api/v1/agent/sandboxes/from-snapshot` with a valid `snapshot_id`
- **THEN** the system SHALL restore a sandbox from the snapshot and return HTTP 201

#### Scenario: Stop sandbox

- **WHEN** a POST request is sent to `/api/v1/agent/sandboxes/:id/stop`
- **THEN** the system SHALL stop the compute environment and update `status` to `stopped`

#### Scenario: Resume sandbox

- **WHEN** a POST request is sent to `/api/v1/agent/sandboxes/:id/resume`
- **THEN** the system SHALL resume the stopped sandbox and update `status` to `ready`

#### Scenario: Delete sandbox

- **WHEN** a DELETE request is sent to `/api/v1/agent/sandboxes/:id`
- **THEN** the system SHALL destroy the compute environment and remove the record

#### Scenario: Execute bash in sandbox

- **WHEN** a POST request is sent to `/api/v1/agent/sandboxes/:id/bash` with a command payload
- **THEN** the system SHALL execute the command in the sandbox and return stdout/stderr

#### Scenario: Sandbox file operations

- **WHEN** POST requests are sent to `/api/v1/agent/sandboxes/:id/read`, `/write`, `/edit`, `/glob`, `/grep`
- **THEN** the system SHALL perform the respective file operation in the sandbox and return the result

#### Scenario: Snapshot sandbox

- **WHEN** a POST request is sent to `/api/v1/agent/sandboxes/:id/snapshot`
- **THEN** the system SHALL create a point-in-time snapshot of the sandbox state and return the snapshot ID

### Requirement: Sandbox images API at /api/admin/sandbox-images

The system SHALL expose sandbox image catalog endpoints under `/api/admin/sandbox-images`. The Go package SHALL be named `sandboximages` and the primary struct SHALL be named `SandboxImage`.

#### Scenario: List sandbox images

- **WHEN** a GET request is sent to `/api/admin/sandbox-images`
- **THEN** the system SHALL return all `SandboxImage` records from `kb.sandbox_images`

#### Scenario: Create sandbox image

- **WHEN** a POST request is sent to `/api/admin/sandbox-images`
- **THEN** the system SHALL create a new `SandboxImage` record and return HTTP 201

### Requirement: Sandbox enabled via ENABLE_AGENT_SANDBOXES env var

The system SHALL gate all sandbox endpoints behind the `ENABLE_AGENT_SANDBOXES` environment variable. When the variable is not set or set to `false`, all sandbox routes SHALL return HTTP 404.

#### Scenario: Sandbox routes active when enabled

- **WHEN** `ENABLE_AGENT_SANDBOXES=true` is set and the server starts
- **THEN** all `/api/v1/agent/sandboxes` and `/api/admin/sandbox-images` routes SHALL be registered and accessible

#### Scenario: Sandbox routes inactive when disabled

- **WHEN** `ENABLE_AGENT_SANDBOXES` is not set or is `false`
- **THEN** the sandbox and sandbox-images route groups SHALL NOT be registered
- **AND** requests to those paths SHALL receive HTTP 404

### Requirement: Database tables use kb.agent_sandboxes naming

The system SHALL store all sandbox records in `kb.agent_sandboxes` and all sandbox image records in `kb.sandbox_images`. The `container_type` discriminator value for agent sandboxes SHALL be `'agent_sandbox'`.

#### Scenario: New sandbox record has correct container_type

- **WHEN** a new `AgentSandbox` is created via the API
- **THEN** the inserted row in `kb.agent_sandboxes` SHALL have `container_type = 'agent_sandbox'`

#### Scenario: MCP server containers retain their container_type

- **WHEN** a container record was created for an MCP server (not an agent sandbox)
- **THEN** its `container_type` value SHALL remain `'mcp_server'` and SHALL NOT be changed by the sandbox rename migration
