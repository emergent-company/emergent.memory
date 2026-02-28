## ADDED Requirements

### Requirement: MCP Server Configuration Storage

The system SHALL store external MCP server configurations per project in the `kb.project_mcp_servers` table.

#### Scenario: MCP server table schema

- **WHEN** the database migration runs
- **THEN** `kb.project_mcp_servers` SHALL exist with columns: id, project_id, product_id, name, description, transport (enum: "stdio", "sse"), config (JSONB containing command/args/env or url/headers), enabled, created_at, updated_at

#### Scenario: Product manifest import

- **WHEN** a product manifest includes an `mcp.servers` block
- **THEN** each server SHALL be stored as a row in `kb.project_mcp_servers`
- **AND** `product_id` SHALL reference the product that defined the server
- **AND** `project_id` SHALL reference the project the product is installed on

### Requirement: MCP Client for stdio Transport

The system SHALL support connecting to external MCP servers via stdio transport (spawning a child process).

#### Scenario: stdio server connection

- **WHEN** the system connects to an MCP server with `transport: "stdio"`
- **THEN** the system SHALL spawn a child process using the configured `command` and `args`
- **AND** communicate via stdin/stdout using the MCP protocol
- **AND** pass configured environment variables to the child process

#### Scenario: Environment variable interpolation

- **WHEN** an MCP server config contains environment variable references (e.g., `"${SEARCH_API_KEY}"`)
- **THEN** the system SHALL interpolate the values from the project's secret store
- **AND** if a referenced variable is not found, the server connection SHALL fail with a descriptive error

#### Scenario: stdio process lifecycle

- **WHEN** a stdio MCP server connection is no longer needed (project unloaded, server reconfigured)
- **THEN** the child process SHALL be terminated gracefully (SIGTERM, then SIGKILL after timeout)

### Requirement: MCP Client for SSE Transport

The system SHALL support connecting to external MCP servers via Server-Sent Events (SSE) transport.

#### Scenario: SSE server connection

- **WHEN** the system connects to an MCP server with `transport: "sse"`
- **THEN** the system SHALL establish an HTTP SSE connection to the configured URL
- **AND** include any configured headers (e.g., Authorization)

#### Scenario: SSE reconnection

- **WHEN** an SSE connection is dropped
- **THEN** the system SHALL attempt to reconnect with exponential backoff
- **AND** tool calls during disconnection SHALL return errors (not block indefinitely)

### Requirement: Tool Discovery from External MCP Servers

The system SHALL discover available tools from connected MCP servers using the MCP `tools/list` method.

#### Scenario: Tool discovery on connection

- **WHEN** a connection to an external MCP server is established
- **THEN** the system SHALL call `tools/list` to discover available tools
- **AND** each discovered tool SHALL be wrapped as an ADK tool function
- **AND** the tool name SHALL be preserved as-is from the MCP server

#### Scenario: Tool call forwarding

- **WHEN** an agent invokes a tool that originates from an external MCP server
- **THEN** the system SHALL forward the tool call to the appropriate MCP server
- **AND** return the MCP server's response to the agent
- **AND** the tool call SHALL be recorded in `kb.agent_run_tool_calls` like built-in tools

### Requirement: Lazy Connection Initialization

The system SHALL initialize MCP server connections lazily (on first use), not at server startup.

#### Scenario: Lazy connection on first tool call

- **WHEN** an agent's resolved tools include a tool from an external MCP server that has not yet been connected
- **THEN** the system SHALL initiate the connection at pipeline build time
- **AND** discover tools via `tools/list`
- **AND** subsequent agent executions in the same project SHALL reuse the existing connection

#### Scenario: Connection pooling per project

- **WHEN** multiple agents in the same project use tools from the same external MCP server
- **THEN** they SHALL share the same MCP client connection
- **AND** connections SHALL be tracked per project, not per agent

#### Scenario: Circuit breaker for failing servers

- **WHEN** an external MCP server fails to connect after 3 consecutive attempts
- **THEN** the system SHALL mark the server as temporarily unavailable
- **AND** tool calls to that server SHALL return immediate errors for a cooldown period
- **AND** the circuit SHALL be retried after the cooldown (default 60 seconds)
