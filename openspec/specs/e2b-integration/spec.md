# e2b-integration Specification

## Purpose
TBD - created by archiving change agent-workspace-infrastructure. Update Purpose after archive.
## Requirements
### Requirement: E2B sandbox creation

The system SHALL create E2B Firecracker-based sandboxes via the E2B SDK for managed deployments.

#### Scenario: Create sandbox with default E2B template

- **WHEN** the E2B provider receives a workspace creation request
- **THEN** the system creates an E2B sandbox using the default base template, achieving startup in approximately 150ms, and returns a workspace reference

#### Scenario: Create sandbox with custom Emergent template

- **WHEN** the E2B provider receives a request and a custom Emergent template ID is configured
- **THEN** the system creates the sandbox using the Emergent-specific template (pre-installed tools, git, build utilities)

#### Scenario: E2B API key not configured

- **WHEN** the E2B provider receives a request but no E2B API key is configured in Emergent server environment
- **THEN** the provider returns an error "E2B API key not configured" and the orchestrator falls back to the next provider

#### Scenario: E2B service unavailable

- **WHEN** the E2B provider attempts to create a sandbox but the E2B managed service is unreachable
- **THEN** the provider returns a connection error within 10 seconds and the orchestrator falls back to the next provider

### Requirement: E2B command execution

The system SHALL execute commands in E2B sandboxes using the E2B SDK's process and filesystem APIs.

#### Scenario: Execute bash command via E2B SDK

- **WHEN** a bash tool request targets an E2B workspace
- **THEN** the system uses `sandbox.Process.Start()` to execute the command, captures stdout and stderr via the SDK's streaming interface, and returns structured results

#### Scenario: File operations via E2B filesystem API

- **WHEN** a read, write, or edit tool request targets an E2B workspace
- **THEN** the system uses `sandbox.Filesystem.Read()`, `sandbox.Filesystem.Write()` respectively, translating the tool interface to E2B SDK calls

#### Scenario: Glob and grep via E2B process execution

- **WHEN** a glob or grep tool request targets an E2B workspace
- **THEN** the system executes `find` or `grep` commands via `sandbox.Process.Start()` inside the sandbox and parses the output into the structured tool response format

### Requirement: E2B sandbox lifecycle

The system SHALL manage E2B sandbox lifecycle through the SDK, respecting the ephemeral nature of E2B sandboxes.

#### Scenario: Sandbox timeout management

- **WHEN** an E2B sandbox is created
- **THEN** the system configures the sandbox timeout to match the workspace TTL (default 30 days or max allowed by E2B plan), and extends the timeout on each tool operation

#### Scenario: Sandbox destruction

- **WHEN** a destroy request is received for an E2B workspace
- **THEN** the system calls `sandbox.Close()` via the SDK, which destroys the Firecracker microVM and releases all resources on E2B's infrastructure

#### Scenario: E2B sandbox persistence limitations

- **WHEN** an E2B sandbox is stopped (timed out or explicitly stopped)
- **THEN** all filesystem state is lost (ephemeral by design), and this limitation is documented in the workspace metadata returned to the agent

### Requirement: E2B deployment mode

The system SHALL support E2B in managed mode with optional future self-hosted support.

#### Scenario: Managed mode (default)

- **WHEN** E2B is configured with `deployment_mode = "managed"` and a valid API key
- **THEN** sandboxes run on E2B's cloud infrastructure, the Emergent server communicates via E2B's API, and no local Firecracker/KVM is required

#### Scenario: E2B resource limits

- **WHEN** a workspace is created with resource limits via E2B
- **THEN** the system maps the resource limits to E2B's sandbox configuration options (CPU, memory) as supported by the E2B plan

#### Scenario: E2B quota tracking

- **WHEN** E2B sandbox operations are performed
- **THEN** the system tracks sandbox creation count and compute minutes for cost monitoring and logs warnings when approaching plan limits

