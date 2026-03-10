## MODIFIED Requirements

### Requirement: Sandbox state persists across agent sessions

The system SHALL maintain sandbox filesystem state across agent session boundaries, enabling multiple agents to work on the same codebase sequentially. Records SHALL be stored in `kb.agent_sandboxes`.

#### Scenario: Agent A modifies files, Agent B sees changes

- **WHEN** Agent A creates and modifies files in sandbox S, then disconnects, and Agent B later attaches to sandbox S
- **THEN** Agent B sees all file changes made by Agent A, including new files, modified content, and deleted files

#### Scenario: Installed dependencies persist

- **WHEN** an agent runs `npm install` in a sandbox, completing the installation, and the sandbox is stopped and later resumed
- **THEN** the `node_modules` directory and all installed dependencies are present in the resumed sandbox

### Requirement: Sandbox attachment by multiple agents

The system SHALL allow different agent sessions to attach to the same sandbox sequentially (not concurrently).

#### Scenario: Sequential sandbox attachment

- **WHEN** Agent A detaches from sandbox S (session ends) and Agent B requests to attach to sandbox S
- **THEN** Agent B receives access to sandbox S with the exact filesystem state left by Agent A

#### Scenario: Reject concurrent attachment

- **WHEN** Agent A is actively attached to sandbox S and Agent B requests to attach to sandbox S
- **THEN** the system rejects Agent B's request with an error indicating the sandbox is currently in use, and identifies the active session

### Requirement: Sandbox state snapshot

The system SHALL support creating point-in-time snapshots of sandbox state for backup and branching.

#### Scenario: Create snapshot of running sandbox

- **WHEN** a snapshot request is received for sandbox S with status `ready`
- **THEN** the system creates a snapshot of the filesystem state, assigns it a unique snapshot ID, stores it in the provider's snapshot storage, and returns the snapshot ID

#### Scenario: Restore sandbox from snapshot

- **WHEN** a restore request is received with a valid snapshot ID
- **THEN** the system creates a new sandbox in `kb.agent_sandboxes` with the filesystem state from the snapshot, assigns a new sandbox ID, and sets status to `ready`

#### Scenario: Snapshot of Firecracker sandbox

- **WHEN** a snapshot request is made for a Firecracker-backed sandbox
- **THEN** the system creates a copy-on-write clone of the block device backing the sandbox and stores the reference

#### Scenario: Snapshot of gVisor sandbox

- **WHEN** a snapshot request is made for a gVisor-backed sandbox
- **THEN** the system creates a Docker volume snapshot (via `docker volume create` from existing) and stores the reference

### Requirement: Running process state across stop/resume

The system SHALL document the limitations of process state across sandbox stop/resume cycles.

#### Scenario: Background processes do not survive stop/resume

- **WHEN** a sandbox with running background processes (e.g., dev server) is stopped and later resumed
- **THEN** the filesystem state is preserved but running processes are NOT restored; the agent MUST restart any required processes

#### Scenario: Environment variables persist in filesystem

- **WHEN** an agent writes environment variables to `.env` or `.bashrc` files and the sandbox is stopped and resumed
- **THEN** the environment variable files are preserved on disk but NOT automatically loaded into new shell sessions unless the tool explicitly sources them

### Requirement: MCP server state persistence

The system SHALL maintain MCP server container state across Emergent server restarts. MCP server containers remain in `kb.agent_sandboxes` with `container_type = 'mcp_server'` and are NOT affected by the sandbox rename.

#### Scenario: MCP server database state survives restart

- **WHEN** an MCP server with persistent storage (database files in mounted volume) is stopped and restarted
- **THEN** the database files and all stored data are present in the restarted container
- **AND** the MCP server's `container_type` column value SHALL remain `'mcp_server'`
