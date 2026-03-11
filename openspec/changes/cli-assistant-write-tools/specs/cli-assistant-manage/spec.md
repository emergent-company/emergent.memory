## ADDED Requirements

### Requirement: create_project MCP tool
The MCP service SHALL expose a `create_project` tool that creates a new project under the authenticated user's organization.

The tool SHALL accept:
- `name` (required string): the project name
- `org_id` (optional string): the organization UUID; if omitted, resolved from the authenticated user's context

The tool SHALL return the created project's `id`, `name`, and `orgId`.

The tool SHALL return a descriptive error if the org cannot be resolved or the project name is blank.

#### Scenario: Agent creates project with name only
- **WHEN** the agent calls `create_project` with `{"name": "my-project"}` and the user's org ID is available in context
- **THEN** a new project is created and the response contains the project `id`

#### Scenario: Agent creates project with explicit org_id
- **WHEN** the agent calls `create_project` with `{"name": "my-project", "org_id": "<uuid>"}`
- **THEN** a new project is created under the specified org

#### Scenario: create_project fails with no org
- **WHEN** the agent calls `create_project` but no org ID is resolvable from context or arguments
- **THEN** the tool returns an error explaining that `org_id` is required

### Requirement: cli-assistant-agent write tool whitelist
The `cli-assistant-agent` definition SHALL include write tools covering all major resource types so it can manage the Memory instance on the user's behalf.

The tool list SHALL include at minimum:
- Graph write: `create_entity`, `update_entity`, `delete_entity`, `create_relationship`, `update_relationship`, `delete_relationship`
- Agent definitions: `create_agent_definition`, `update_agent_definition`, `delete_agent_definition`
- Runtime agents: `create_agent`, `update_agent`, `delete_agent`, `trigger_agent`
- Schema management: `create_schema`, `delete_schema`, `assign_schema`, `update_template_assignment`
- MCP registry: `create_mcp_server`, `update_mcp_server`, `delete_mcp_server`, `install_mcp_from_registry`, `sync_mcp_server_tools`
- Project management: `create_project`

#### Scenario: Agent creates an agent definition when asked
- **WHEN** the user asks the agent to create an agent definition with a name, system prompt, and model
- **THEN** the agent calls `create_agent_definition` with the specified parameters
- **THEN** the response confirms the agent definition was created and includes its ID

#### Scenario: Agent creates a project when asked
- **WHEN** the user asks the agent to create a new project
- **THEN** the agent calls `create_project` with the requested name
- **THEN** the response confirms the project was created and includes its ID

### Requirement: cli-assistant-agent action-aware system prompt
The `cli-assistant-agent` system prompt SHALL instruct the agent that it can take actions — not just answer questions — and SHALL include guardrails for destructive operations.

The system prompt SHALL instruct the agent to:
- Describe what action it is about to take before executing write operations
- Prefer `update_*` over delete-then-recreate for modifications
- Warn the user and request confirmation before calling any `delete_*` tool

#### Scenario: Agent describes action before executing
- **WHEN** the user asks the agent to create a resource
- **THEN** the agent briefly describes what it will do before calling the write tool

#### Scenario: Agent warns before delete
- **WHEN** the user asks the agent to delete a resource
- **THEN** the agent explicitly states the deletion is irreversible before proceeding
