# mcp-connector Specification

## Purpose

Lets a user run a headless local CLI connector that registers their machine as an external MCP node on a Memory project over the MCP relay, serving a v1 set of local Apple (Notes/Reminders) tools, so those tools become visible and callable through Memory without opening inbound ports.

## Requirements

### Requirement: Connect and register with the project relay hub

The connector SHALL open an outbound WebSocket to the project's MCP relay hub endpoint and register itself with a stable instance id, version, and its current tool list, using the project-scoped bearer token from its configuration.

#### Scenario: Successful registration

- **WHEN** the connector starts with valid configuration and the hub is reachable
- **THEN** it connects and registers, and the node appears in the Memory project's connected relay sessions with its tool count

#### Scenario: Registration replaces a stale session

- **WHEN** a node with the same instance id reconnects
- **THEN** the new registration replaces the previous session without error

#### Scenario: Hub unreachable at start

- **WHEN** the hub cannot be reached when the connector starts
- **THEN** the connector retries with backoff and does not exit on its own

### Requirement: Serve relayed tool calls

The connector SHALL respond to relayed MCP `tools/call` requests for the tools it registered by dispatching to the matching local handler and returning a result or an error response to the hub.

#### Scenario: Known tool call succeeds

- **WHEN** the hub forwards a call for a registered tool with valid arguments
- **THEN** the connector runs the local handler and returns the result to the hub

#### Scenario: Unknown tool call

- **WHEN** the hub forwards a call for a tool the connector did not register
- **THEN** the connector returns an error response naming the unknown tool

#### Scenario: Handler failure

- **WHEN** the local handler for a registered tool fails (e.g. the target app is not running or a script errors)
- **THEN** the connector returns an error response describing the failure

### Requirement: Keep the connection alive

The connector SHALL keep the relay connection alive with keepalive pings, SHALL detect a dead connection, and SHALL reconnect with capped exponential backoff, re-registering after every reconnect.

#### Scenario: Idle connection stays alive

- **WHEN** the connection is idle for longer than the hub's idle timeout
- **THEN** keepalive traffic keeps the session from being reaped

#### Scenario: Connection drops

- **WHEN** the hub or network drops the connection
- **THEN** the connector reconnects with backoff and re-registers without operator action

#### Scenario: Clean shutdown

- **WHEN** the connector receives SIGTERM or SIGINT
- **THEN** it closes the relay connection cleanly and exits

### Requirement: Configure a project from the CLI

The connector SHALL provide an `init` command that captures the server URL, a project-scoped bearer token, and project context, validates connectivity and auth against the hub, and writes a local configuration file with restrictive permissions. The connector SHALL also honor an optional `disabled_tools` list in its configuration: such tools are NOT registered with the hub, are NOT reported by `status`, and are rejected when called.

#### Scenario: init succeeds

- **WHEN** a user runs init with a reachable server and valid token
- **THEN** the configuration is written and the user is told the connector is ready to relay

#### Scenario: init rejects invalid token

- **WHEN** a user runs init with a server that rejects the token
- **THEN** init reports the auth failure and writes no configuration

#### Scenario: Disabled tools are not registered

- **WHEN** the configuration lists a tool under `disabled_tools`
- **THEN** that tool is absent from the registered tool list and from `status`, and calls to it return an error naming the tool as disabled

### Requirement: Report status

The connector SHALL provide a `status` command that reports whether the relay connection is established and which tools are currently registered.

#### Scenario: status while connected

- **WHEN** a user runs status while the connector is connected
- **THEN** the output shows the connection state and the registered tool list

### Requirement: Serve Apple Notes and Reminders tools on macOS

On macOS the connector SHALL register local MCP tools for Apple Notes and Apple Reminders, implemented via the system's AppleScript automation (no third-party dependencies), covering at least searching and creating in Notes and listing and adding in Reminders.

#### Scenario: Apple tools registered on macOS

- **WHEN** the connector starts on macOS
- **THEN** its registered tool list includes Notes and Reminders tools with names, descriptions, and input schemas

#### Scenario: Apple tools unavailable on other platforms

- **WHEN** the connector starts on a non-macOS platform
- **THEN** it runs without the Apple tools and its status output explains the platform limitation

### Requirement: Host local MCP servers and share a selected tool subset

The connector SHALL host arbitrary local MCP servers configured under `mcp_servers` and share a user-selected subset of their tools through the same relay/tool mechanism as the built-in tools. Each server entry SHALL specify a unique `name`, a `transport` of `stdio`, `http`, or `sse`, and the transport-specific connection fields (`command`/`args`/`env` for stdio; `url`/`headers` for http/sse). An omitted `enabled` defaults to true, and each server SHALL support a per-server `disabled_tools` deny list of its own (un-namespaced) tool names. The connector SHALL connect outbound only — hosted servers SHALL NOT require any inbound port on the connector host — and server credentials (commands, URLs, headers, env) SHALL remain local and never be sent to Memory.

#### Scenario: Configured server's tools are registered and relayed

- **WHEN** the connector starts with an enabled `mcp_servers` entry and the server is reachable
- **THEN** the connector connects, runs the MCP initialize handshake, lists the server's tools, and registers each selected tool in the local registry under the namespaced name `<serverName>_<toolName>`, which Memory exposes to agents as `<instanceID>_<serverName>_<toolName>`; relayed calls are forwarded to that server and its result returned through the relay

#### Scenario: Per-tool selection on the connector

- **WHEN** a hosted server exposes tools A and B and its config lists B in `disabled_tools`
- **THEN** only A is registered and shared; B is absent from the relayed tool list and calls to it fail as an unknown tool

#### Scenario: One bad server does not break the others

- **WHEN** one configured server fails to connect, start, or initialize
- **THEN** the failure is logged and that server is skipped, while the other hosted servers and the built-in tools still register and the connector keeps running

#### Scenario: Invalid or duplicate hosted-server config is rejected

- **WHEN** `mcp_servers` contains an unknown transport, a missing transport-required field, or two servers with the same name
- **THEN** configuration validation fails with a clear error naming the offending server and the connector does not start

#### Scenario: Hosted tool name collides with an existing tool

- **WHEN** a hosted tool's namespaced name is already registered (for example it collides with a built-in tool or another hosted server)
- **THEN** startup fails with a clear error naming the server and the conflicting tool

#### Scenario: Secrets and connection details stay local

- **WHEN** a hosted server is registered through the relay
- **THEN** Memory receives only the tool names, descriptions, and input schemas — never the server's command, arguments, environment, URL, or headers

### Requirement: Local management API for hosted MCP servers

The connector SHALL optionally expose a loopback-only HTTP management API so a companion UI can manage hosted MCP servers without hand-editing the config file. The API SHALL be disabled by default, enabled by `relay --api-port <port>`, and bound to `127.0.0.1` only. It SHALL provide: list/create servers (`GET`/`POST /api/mcp-servers`), read one (`GET /api/mcp-servers/{name}`), replace config (`PUT /api/mcp-servers/{name}/config`), enable/disable (`PUT /api/mcp-servers/{name}/enabled`), delete (`DELETE /api/mcp-servers/{name}`), list a server's discovered tools (`GET /api/mcp-servers/{name}/tools`), and connector status (`GET /api/status`). Every mutation SHALL be validated, persisted to the connector config file, and applied live (the relayed tool list updates) without restarting the process.

#### Scenario: CRUD persists and hot-reloads

- **WHEN** a client creates, updates, enables/disables, or deletes a hosted server through the API
- **THEN** the change is validated, written to the connector config file, and applied to the running process so the hub's registered tool list reflects it without a restart

#### Scenario: Invalid config is rejected without side effects

- **WHEN** a create/update request is invalid (unknown transport, missing required field, duplicate name)
- **THEN** the API returns a 4xx error naming the problem and the stored configuration is unchanged

#### Scenario: Unknown server returns not found

- **WHEN** a request targets a server name that is not configured
- **THEN** the API returns 404

#### Scenario: Status reflects per-server state

- **WHEN** a client reads `GET /api/mcp-servers` or `GET /api/status`
- **THEN** each configured server is reported with its transport, enabled state, connection state/error, and tool count, alongside the connector's relay connection state

#### Scenario: API is loopback-only

- **WHEN** the management API is enabled
- **THEN** it listens on `127.0.0.1` only and no inbound request from another host can reach it

