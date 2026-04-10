## ADDED Requirements

### Requirement: MCP tool `acp-list-agents`
The system SHALL register an `acp-list-agents` MCP tool in `MCPToolHandler` that lists externally-visible agents via ACP semantics. The tool SHALL call internal service methods directly (not HTTP) and return a JSON array of agent manifests matching the ACP discovery format. The tool SHALL accept no required parameters and an optional `include_status` boolean parameter.

#### Scenario: List external agents via MCP
- **WHEN** an MCP client calls `acp-list-agents` with no parameters
- **THEN** the tool returns a JSON array of agent manifests for all `visibility = 'external'` agents in the project, formatted as ACP manifests

#### Scenario: List agents with status metrics
- **WHEN** an MCP client calls `acp-list-agents` with `include_status: true`
- **THEN** each agent manifest in the response includes the `status` object with `avg_run_tokens`, `avg_run_time_seconds`, and `success_rate`

#### Scenario: No external agents returns empty array
- **WHEN** an MCP client calls `acp-list-agents` and no agents have `visibility = 'external'`
- **THEN** the tool returns `[]`

### Requirement: MCP tool `acp-trigger-run`
The system SHALL register an `acp-trigger-run` MCP tool in `MCPToolHandler` that creates and executes a run against an externally-visible agent. The tool SHALL accept `agent_name` (required, string), `message` (required, string — plain text input), `mode` (optional, default `sync`; values: `sync`, `async`), and `session_id` (optional, string). The tool SHALL call internal service methods directly, not HTTP endpoints.

#### Scenario: Trigger sync run via MCP
- **WHEN** an MCP client calls `acp-trigger-run` with `agent_name: "my-agent"` and `message: "Summarize the project"`
- **THEN** the tool blocks until the run completes and returns the run object with `status: "completed"` and `output` messages in ACP format

#### Scenario: Trigger async run via MCP
- **WHEN** an MCP client calls `acp-trigger-run` with `agent_name: "my-agent"`, `message: "Process"`, and `mode: "async"`
- **THEN** the tool returns immediately with the run object containing `id` and `status: "submitted"`

#### Scenario: Trigger run with non-external agent returns error
- **WHEN** an MCP client calls `acp-trigger-run` with `agent_name: "internal-agent"` where the agent has `visibility = 'project'`
- **THEN** the tool returns an error: "Agent 'internal-agent' is not externally visible via ACP"

#### Scenario: Trigger run with non-existent agent returns error
- **WHEN** an MCP client calls `acp-trigger-run` with `agent_name: "nonexistent"`
- **THEN** the tool returns an error: "Agent 'nonexistent' not found"

#### Scenario: Trigger run that pauses returns input-required
- **WHEN** a sync run triggered via MCP pauses because the agent asks a question
- **THEN** the tool returns the run object with `status: "input-required"` and `await_request` containing the question details

#### Scenario: Trigger run with session_id
- **WHEN** an MCP client calls `acp-trigger-run` with `session_id: "<uuid>"`
- **THEN** the created run is linked to the specified ACP session

#### Scenario: Stream mode is not supported via MCP
- **WHEN** an MCP client calls `acp-trigger-run` with `mode: "stream"`
- **THEN** the tool returns an error: "Stream mode is not supported via MCP tools. Use sync or async."

### Requirement: MCP tool `acp-get-run-status`
The system SHALL register an `acp-get-run-status` MCP tool in `MCPToolHandler` that retrieves the current state of a run. The tool SHALL accept `agent_name` (required, string) and `run_id` (required, string). The tool SHALL return the run object with ACP status mapping applied.

#### Scenario: Get status of a completed run
- **WHEN** an MCP client calls `acp-get-run-status` with `agent_name: "my-agent"` and `run_id: "<runId>"`
- **THEN** the tool returns the run object with `status: "completed"` and `output` messages

#### Scenario: Get status of a running run
- **WHEN** an MCP client calls `acp-get-run-status` for a run with Memory status `running`
- **THEN** the tool returns the run object with `status: "working"` (ACP mapped)

#### Scenario: Get status of a paused run includes await_request
- **WHEN** an MCP client calls `acp-get-run-status` for a paused run with a pending question
- **THEN** the tool returns the run object with `status: "input-required"` and `await_request`

#### Scenario: Get status of non-existent run returns error
- **WHEN** an MCP client calls `acp-get-run-status` with a `run_id` that does not exist
- **THEN** the tool returns an error: "Run not found"

#### Scenario: Get status with mismatched agent name returns error
- **WHEN** an MCP client calls `acp-get-run-status` with `agent_name: "wrong-agent"` for a run that belongs to a different agent
- **THEN** the tool returns an error: "Run not found" (agent name mismatch treated as not found)

### Requirement: MCP tool definitions follow existing pattern
All three ACP MCP tools SHALL be registered in `MCPToolHandler.GetAgentToolDefinitions()` using the same `mcp.ToolDefinition` structure as existing tools. Each tool definition SHALL include `name`, `description`, and `inputSchema` with JSON Schema property definitions.

#### Scenario: Tool definitions include ACP tools
- **WHEN** `GetAgentToolDefinitions()` is called
- **THEN** the returned slice includes definitions for `acp-list-agents`, `acp-trigger-run`, and `acp-get-run-status`

#### Scenario: Tool definitions have proper input schemas
- **WHEN** the `acp-trigger-run` tool definition is inspected
- **THEN** its `inputSchema` has `agent_name` (required, string), `message` (required, string), `mode` (optional, enum: sync/async), and `session_id` (optional, string)

### Requirement: Internal service calls, not HTTP
ACP MCP tools SHALL call internal service methods directly (e.g., `h.repo.FindExternalAgents()`, `h.executor.ExecuteWithRun()`) rather than making HTTP requests to `/acp/v1/` endpoints. This avoids network overhead, auth token management, and circular dependencies.

#### Scenario: MCP tool does not make HTTP calls
- **WHEN** `acp-trigger-run` is invoked via MCP
- **THEN** it calls internal Go methods on the service/executor directly, not `http.Get("/acp/v1/...")`

#### Scenario: MCP tool uses same executor as HTTP endpoints
- **WHEN** `acp-trigger-run` creates a run
- **THEN** it uses the same `AgentExecutor.ExecuteWithRun()` method that the ACP HTTP handler uses, ensuring identical behavior
