## Purpose

Lets a user run a headless local CLI connector that registers their machine as an external MCP node on a Memory project over the MCP relay, serving a v1 set of local Apple (Notes/Reminders) tools, so those tools become visible and callable through Memory without opening inbound ports.

## ADDED Requirements

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

The connector SHALL provide an `init` command that captures the server URL, a project-scoped bearer token, and project context, validates connectivity and auth against the hub, and writes a local configuration file with restrictive permissions.

#### Scenario: init succeeds

- **WHEN** a user runs init with a reachable server and valid token
- **THEN** the configuration is written and the user is told the connector is ready to relay

#### Scenario: init rejects invalid token

- **WHEN** a user runs init with a server that rejects the token
- **THEN** init reports the auth failure and writes no configuration

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
