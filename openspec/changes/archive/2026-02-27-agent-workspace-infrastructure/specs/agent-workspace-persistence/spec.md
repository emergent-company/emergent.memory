## ADDED Requirements

### Requirement: Workspace state persists across agent sessions

The system SHALL maintain workspace filesystem state across agent session boundaries, enabling multiple agents to work on the same codebase sequentially.

#### Scenario: Agent A modifies files, Agent B sees changes

- **WHEN** Agent A creates and modifies files in workspace W, then disconnects, and Agent B later attaches to workspace W
- **THEN** Agent B sees all file changes made by Agent A, including new files, modified content, and deleted files

#### Scenario: Installed dependencies persist

- **WHEN** an agent runs `npm install` in a workspace, completing the installation, and the workspace is stopped and later resumed
- **THEN** the `node_modules` directory and all installed dependencies are present in the resumed workspace

### Requirement: Workspace attachment by multiple agents

The system SHALL allow different agent sessions to attach to the same workspace sequentially (not concurrently).

#### Scenario: Sequential workspace attachment

- **WHEN** Agent A detaches from workspace W (session ends) and Agent B requests to attach to workspace W
- **THEN** Agent B receives access to workspace W with the exact filesystem state left by Agent A

#### Scenario: Reject concurrent attachment

- **WHEN** Agent A is actively attached to workspace W and Agent B requests to attach to workspace W
- **THEN** the system rejects Agent B's request with an error indicating the workspace is currently in use, and identifies the active session

### Requirement: Workspace state snapshot

The system SHALL support creating point-in-time snapshots of workspace state for backup and branching.

#### Scenario: Create snapshot of running workspace

- **WHEN** a snapshot request is received for workspace W with status `ready`
- **THEN** the system creates a snapshot of the filesystem state, assigns it a unique snapshot ID, stores it in the provider's snapshot storage, and returns the snapshot ID

#### Scenario: Restore workspace from snapshot

- **WHEN** a restore request is received with a valid snapshot ID
- **THEN** the system creates a new workspace with the filesystem state from the snapshot, assigns a new workspace ID, and sets status to `ready`

#### Scenario: Snapshot of Firecracker workspace

- **WHEN** a snapshot request is made for a Firecracker-backed workspace
- **THEN** the system creates a copy-on-write clone of the block device backing the workspace and stores the reference

#### Scenario: Snapshot of gVisor workspace

- **WHEN** a snapshot request is made for a gVisor-backed workspace
- **THEN** the system creates a Docker volume snapshot (via `docker volume create` from existing) and stores the reference

### Requirement: Running process state across stop/resume

The system SHALL document the limitations of process state across workspace stop/resume cycles.

#### Scenario: Background processes do not survive stop/resume

- **WHEN** a workspace with running background processes (e.g., dev server) is stopped and later resumed
- **THEN** the filesystem state is preserved but running processes are NOT restored; the agent MUST restart any required processes

#### Scenario: Environment variables persist in filesystem

- **WHEN** an agent writes environment variables to `.env` or `.bashrc` files and the workspace is stopped and resumed
- **THEN** the environment variable files are preserved on disk but NOT automatically loaded into new shell sessions unless the tool explicitly sources them

### Requirement: MCP server state persistence

The system SHALL maintain MCP server container state across Emergent server restarts.

#### Scenario: MCP server database state survives restart

- **WHEN** an MCP server with persistent storage (database files in mounted volume) is stopped and restarted
- **THEN** the database files and all stored data are present in the restarted container

#### Scenario: MCP server configuration persists

- **WHEN** the Emergent server restarts (planned or crash)
- **THEN** all registered MCP server configurations in `kb.agent_workspaces` are preserved and the system restarts all persistent MCP servers automatically

### Requirement: Workspace metadata tracking

The system SHALL track workspace usage metadata for monitoring and debugging.

#### Scenario: Track last used timestamp

- **WHEN** any tool operation is performed on a workspace
- **THEN** the system updates the `last_used_at` field in the database to the current timestamp

#### Scenario: Track agent session history

- **WHEN** a new agent attaches to an existing workspace
- **THEN** the system updates `agent_session_id` to the new session and logs the attachment event with the previous session ID for audit trail
