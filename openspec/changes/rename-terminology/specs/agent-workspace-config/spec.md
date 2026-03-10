## MODIFIED Requirements

### Requirement: Agent type sandbox configuration

The system SHALL allow each agent type to declaratively define sandbox requirements, including repository source, allowed tools, resource limits, and setup behavior. The configuration struct SHALL be named `AgentSandboxConfig` and the JSONB column on `kb.agent_definitions` SHALL be named `sandbox_config`.

#### Scenario: Agent type with sandbox enabled

- **WHEN** an agent type is configured with `sandbox.enabled = true`, `repo_source.type = "task_context"`, `tools = ["bash","read","write","edit","grep","git"]`, and `resource_limits = {cpu: "2", memory: "4G"}`
- **THEN** the system stores this configuration in `sandbox_config` and uses it to auto-provision sandboxes for all sessions of this agent type

#### Scenario: Agent type with sandbox disabled

- **WHEN** an agent type is configured with `sandbox.enabled = false`
- **THEN** no sandbox is provisioned for sessions of this agent type, and sandbox tool endpoints return 404 for those sessions

#### Scenario: Agent type with fixed repository

- **WHEN** an agent type is configured with `repo_source.type = "fixed"` and `repo_source.url = "https://github.com/org/infra-repo"`
- **THEN** all sandboxes for this agent type clone the specified repository regardless of task context

#### Scenario: Agent type with no repository

- **WHEN** an agent type is configured with `repo_source.type = "none"`
- **THEN** sandboxes are created with an empty `/workspace` directory and no git clone is performed

#### Scenario: Tool restriction enforcement

- **WHEN** an agent type is configured with `tools = ["read","grep"]` and the agent attempts to use the `bash` tool
- **THEN** the system rejects the tool call with an error "Tool 'bash' is not enabled for this agent type" and returns HTTP 403

#### Scenario: Resource limit inheritance

- **WHEN** an agent type does not specify `resource_limits`
- **THEN** the system applies default resource limits from the server configuration (`SANDBOX_DEFAULT_CPU`, `SANDBOX_DEFAULT_MEMORY`, `SANDBOX_DEFAULT_DISK`)

### Requirement: Auto-provisioning on session start

The system SHALL automatically provision a sandbox when an agent session starts, based on the agent type's sandbox configuration.

#### Scenario: Session start triggers sandbox creation

- **WHEN** a new agent session starts for an agent type with `sandbox.enabled = true`
- **THEN** the system automatically creates a sandbox using the agent type's configuration, attaches it to the session, and makes it available before the agent receives its first task

#### Scenario: Sandbox ready before agent activation

- **WHEN** auto-provisioning is in progress for a new session
- **THEN** the agent session status is `provisioning` until the sandbox is ready (container created, repo cloned, setup commands complete), then transitions to `active`

#### Scenario: Auto-provisioning failure

- **WHEN** sandbox auto-provisioning fails (provider unavailable, clone failure, setup command failure)
- **THEN** the system retries once with the fallback provider, and if still failing, starts the session without a sandbox and logs the failure, allowing the agent to operate in degraded mode

#### Scenario: Warm pool assignment for auto-provisioned sandboxes

- **WHEN** a session starts and a warm pool container is available matching the agent type's provider preference
- **THEN** the system assigns the warm pool container instead of creating a new one, reducing provisioning time to near-zero

### Requirement: Task context binding

The system SHALL extract repository and branch information from the task assigned to an agent session, using it to configure the sandbox.

#### Scenario: PR-based task context

- **WHEN** a task is assigned with context `{repository_url: "https://github.com/org/repo", branch: "feature/auth", pull_request_number: 42, base_branch: "main"}` and the agent type has `repo_source.type = "task_context"`
- **THEN** the sandbox clones the repository at the `feature/auth` branch, and the task context (PR number, base branch) is available to the agent via sandbox metadata

#### Scenario: Task context overrides default branch

- **WHEN** the agent type specifies `repo_source.branch = "main"` as default, but the task context specifies `branch = "develop"`
- **THEN** the sandbox uses the task context's `branch = "develop"`, with the agent type's branch serving only as fallback when task context has no branch
