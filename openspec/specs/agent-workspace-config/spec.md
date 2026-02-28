# agent-workspace-config Specification

## Purpose
TBD - created by archiving change agent-workspace-infrastructure. Update Purpose after archive.
## Requirements
### Requirement: Agent type workspace configuration

The system SHALL allow each agent type to declaratively define workspace requirements, including repository source, allowed tools, resource limits, and setup behavior.

#### Scenario: Agent type with workspace enabled

- **WHEN** an agent type is configured with `workspace.enabled = true`, `repo_source.type = "task_context"`, `tools = ["bash","read","write","edit","grep","git"]`, and `resource_limits = {cpu: "2", memory: "4G"}`
- **THEN** the system stores this configuration and uses it to auto-provision workspaces for all sessions of this agent type

#### Scenario: Agent type with workspace disabled

- **WHEN** an agent type is configured with `workspace.enabled = false`
- **THEN** no workspace is provisioned for sessions of this agent type, and workspace tool endpoints return 404 for those sessions

#### Scenario: Agent type with fixed repository

- **WHEN** an agent type is configured with `repo_source.type = "fixed"` and `repo_source.url = "https://github.com/org/infra-repo"`
- **THEN** all workspaces for this agent type clone the specified repository regardless of task context

#### Scenario: Agent type with no repository

- **WHEN** an agent type is configured with `repo_source.type = "none"`
- **THEN** workspaces are created with an empty `/workspace` directory and no git clone is performed

#### Scenario: Tool restriction enforcement

- **WHEN** an agent type is configured with `tools = ["read","grep"]` and the agent attempts to use the `bash` tool
- **THEN** the system rejects the tool call with an error "Tool 'bash' is not enabled for this agent type" and returns HTTP 403

#### Scenario: Resource limit inheritance

- **WHEN** an agent type does not specify `resource_limits`
- **THEN** the system applies default resource limits from the server configuration (`WORKSPACE_DEFAULT_CPU`, `WORKSPACE_DEFAULT_MEMORY`, `WORKSPACE_DEFAULT_DISK`)

### Requirement: Auto-provisioning on session start

The system SHALL automatically provision a workspace when an agent session starts, based on the agent type's workspace configuration.

#### Scenario: Session start triggers workspace creation

- **WHEN** a new agent session starts for an agent type with `workspace.enabled = true`
- **THEN** the system automatically creates a workspace using the agent type's configuration, attaches it to the session, and makes it available before the agent receives its first task

#### Scenario: Workspace ready before agent activation

- **WHEN** auto-provisioning is in progress for a new session
- **THEN** the agent session status is `provisioning` until the workspace is ready (container created, repo cloned, setup commands complete), then transitions to `active`

#### Scenario: Auto-provisioning failure

- **WHEN** workspace auto-provisioning fails (provider unavailable, clone failure, setup command failure)
- **THEN** the system retries once with the fallback provider, and if still failing, starts the session without a workspace and logs the failure, allowing the agent to operate in degraded mode

#### Scenario: Warm pool assignment for auto-provisioned workspaces

- **WHEN** a session starts and a warm pool container is available matching the agent type's provider preference
- **THEN** the system assigns the warm pool container instead of creating a new one, reducing provisioning time to near-zero

### Requirement: Task context binding

The system SHALL extract repository and branch information from the task assigned to an agent session, using it to configure the workspace.

#### Scenario: PR-based task context

- **WHEN** a task is assigned with context `{repository_url: "https://github.com/org/repo", branch: "feature/auth", pull_request_number: 42, base_branch: "main"}` and the agent type has `repo_source.type = "task_context"`
- **THEN** the workspace clones the repository at the `feature/auth` branch, and the task context (PR number, base branch) is available to the agent via workspace metadata

#### Scenario: Task context overrides default branch

- **WHEN** the agent type specifies `repo_source.branch = "main"` as default, but the task context specifies `branch = "develop"`
- **THEN** the workspace uses the task context's `branch = "develop"`, with the agent type's branch serving only as fallback when task context has no branch

#### Scenario: Task without repository context

- **WHEN** a task is assigned without `repository_url` in its context, and the agent type has `repo_source.type = "task_context"`
- **THEN** the workspace is created with an empty `/workspace` directory (same as `repo_source.type = "none"` behavior)

#### Scenario: Fixed repo ignores task context

- **WHEN** a task has `repository_url` in its context but the agent type has `repo_source.type = "fixed"` with a different URL
- **THEN** the workspace clones the agent type's fixed repository, not the task context's repository

### Requirement: Setup commands execution

The system SHALL execute configured setup commands in the workspace after repository checkout, before marking the workspace as ready.

#### Scenario: Single setup command

- **WHEN** an agent type specifies `setup_commands = ["npm install --frozen-lockfile"]`
- **THEN** the system executes the command in `/workspace` after the git clone completes, and the workspace status transitions to `ready` only after the command succeeds

#### Scenario: Multiple setup commands in order

- **WHEN** an agent type specifies `setup_commands = ["npm install", "npm run build"]`
- **THEN** the system executes the commands sequentially in order, and if any command fails (non-zero exit), subsequent commands are skipped and the workspace is marked as `ready` with a warning about partial setup

#### Scenario: Setup command timeout

- **WHEN** a setup command runs longer than 5 minutes (default timeout)
- **THEN** the system kills the command, marks the workspace as `ready` with a warning, and logs the timeout

#### Scenario: No setup commands

- **WHEN** an agent type has an empty `setup_commands` list or the field is omitted
- **THEN** the workspace transitions to `ready` immediately after repository checkout (or container creation if no repo)

### Requirement: Workspace configuration management API

The system SHALL provide API endpoints for managing agent type workspace configurations.

#### Scenario: Get workspace config for agent type

- **WHEN** a `GET /api/v1/agent-types/:id/workspace-config` request is made
- **THEN** the system returns the workspace configuration for the specified agent type, including all fields (enabled, repo_source, tools, resource_limits, checkout_on_start, base_image, setup_commands)

#### Scenario: Update workspace config

- **WHEN** a `PUT /api/v1/agent-types/:id/workspace-config` request is made with updated configuration
- **THEN** the system validates the configuration (valid tools list, valid resource limits, valid repo_source type), persists the changes, and the new configuration applies to all future sessions (existing sessions are not affected)

#### Scenario: Invalid configuration rejected

- **WHEN** a workspace config update includes an invalid tool name (e.g., `tools = ["bash","ssh"]` where `ssh` is not a valid tool)
- **THEN** the system rejects the update with a 400 error listing the invalid tool names and valid options

#### Scenario: Default configuration for new agent types

- **WHEN** a new agent type is created without specifying workspace configuration
- **THEN** the system applies a default configuration: `workspace.enabled = false`, and the agent type operates without workspace capabilities until explicitly configured

