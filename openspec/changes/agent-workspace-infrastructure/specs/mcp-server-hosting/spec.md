## ADDED Requirements

### Requirement: Register MCP server for hosting

The system SHALL accept MCP server registration configurations that define how to run a persistent, stdio-based MCP server in an isolated container.

#### Scenario: Register stdio-based MCP server

- **WHEN** an MCP server registration request is received with `name = "langfuse-mcp"`, `image = "emergent/mcp-langfuse:latest"`, `stdio_bridge = true`, and `restart_policy = "always"`
- **THEN** the system creates a persistent container from the image, establishes a stdio bridge (stdin/stdout connection), stores the configuration in `kb.agent_workspaces` with `container_type = 'mcp_server'`, and sets status to `ready`

#### Scenario: Register HTTP-based MCP server

- **WHEN** an MCP server registration request is received with `stdio_bridge = false` and a port mapping
- **THEN** the system creates a persistent container with the specified port exposed, does NOT establish a stdio bridge, and the MCP server is accessible via HTTP directly

#### Scenario: Register with environment variables

- **WHEN** an MCP server registration includes `environment = {"API_KEY": "...", "DB_URL": "..."}`
- **THEN** the system passes these environment variables to the container at startup, and they are available to the MCP server process

### Requirement: stdio-to-HTTP bridge

The system SHALL bridge stdio-based MCP server communication (stdin/stdout) to an HTTP API that agents can call.

#### Scenario: Call MCP method via HTTP bridge

- **WHEN** a call request is sent to `/api/v1/mcp/servers/:id/call` with `method = "tools/call"` and `params = {"name": "get-traces"}`
- **THEN** the system formats the request as JSON-RPC, writes it to the container's stdin, reads the response from stdout, parses the JSON-RPC response, and returns the result as HTTP JSON

#### Scenario: MCP server returns error via stdio

- **WHEN** the MCP server writes a JSON-RPC error response to stdout
- **THEN** the system parses the error, returns it as a structured HTTP response with appropriate status code, preserving the MCP error code and message

#### Scenario: MCP server response timeout

- **WHEN** a call is made to an MCP server and no stdout response is received within 30 seconds (configurable)
- **THEN** the system returns a timeout error to the caller, does NOT restart the MCP server (it may still be processing), and logs the timeout event

#### Scenario: Concurrent MCP calls are serialized

- **WHEN** multiple agents send concurrent call requests to the same stdio-based MCP server
- **THEN** the system serializes the calls (queue them), sending one request at a time to stdin and waiting for the response before sending the next, to prevent interleaved stdio

### Requirement: MCP server lifecycle management

The system SHALL manage the lifecycle of MCP server containers as persistent daemon processes.

#### Scenario: Auto-start on Emergent boot

- **WHEN** the Emergent server starts
- **THEN** the system queries all registered MCP servers from the database and starts each one, establishing stdio bridges as configured, in parallel

#### Scenario: Restart after crash

- **WHEN** an MCP server container's main process exits unexpectedly (non-zero exit code)
- **THEN** the system restarts the container within 5 seconds, re-establishes the stdio bridge, and logs the crash event with exit code and any stderr output

#### Scenario: Crash loop backoff

- **WHEN** an MCP server crashes more than 3 times within 60 seconds
- **THEN** the system enters backoff mode with delays of 5s, 15s, 45s, 2m, 5m between restart attempts, and emits a health warning

#### Scenario: Graceful shutdown

- **WHEN** the Emergent server receives a shutdown signal (SIGTERM)
- **THEN** the system sends SIGTERM to all MCP server containers, waits up to 30 seconds for graceful shutdown, then sends SIGKILL to any remaining containers

#### Scenario: Manual restart

- **WHEN** a restart request is sent to `/api/v1/mcp/servers/:id/restart`
- **THEN** the system sends SIGTERM to the current process, waits up to 10 seconds, starts a new container from the same image and configuration, re-establishes the stdio bridge, and resets crash counters

### Requirement: MCP server health monitoring

The system SHALL monitor the health of hosted MCP servers and report their status.

#### Scenario: Health status for running server

- **WHEN** a status request is sent to `/api/v1/mcp/servers/:id`
- **THEN** the system returns the container status (running/stopped/restarting), uptime, restart count, last crash timestamp (if any), and resource usage (CPU/memory)

#### Scenario: List all MCP servers

- **WHEN** a list request is sent to `/api/v1/mcp/servers`
- **THEN** the system returns all registered MCP servers with their current status, image, and configuration summary

#### Scenario: Detect unresponsive server

- **WHEN** an MCP server container is running but the process inside is unresponsive (no stdout output for heartbeat checks)
- **THEN** the system marks the server status as `unresponsive` and logs a warning, but does NOT automatically restart (the MCP may be doing long-running work)

### Requirement: MCP server resource isolation

The system SHALL isolate MCP server containers with appropriate resource limits, preferring lightweight providers.

#### Scenario: Default resource limits for MCP servers

- **WHEN** an MCP server is registered without explicit resource limits
- **THEN** the system applies default MCP limits: 0.5 CPU, 512MB memory, 1GB disk (lighter than workspace defaults)

#### Scenario: Custom resource limits

- **WHEN** an MCP server registration includes `resource_limits = {"cpu": "1", "memory": "1G"}`
- **THEN** the system enforces the specified limits on the container

#### Scenario: Provider preference for MCP servers

- **WHEN** an MCP server is registered without specifying a provider
- **THEN** the system prefers gVisor (lighter resource overhead for persistent long-running containers) over Firecracker or E2B

### Requirement: MCP server storage persistence

The system SHALL provide persistent storage for MCP servers that need to maintain state across restarts.

#### Scenario: Persistent volume mount

- **WHEN** an MCP server registration includes `volumes = ["/data"]`
- **THEN** the system creates a named persistent volume for the specified path, mounts it into the container, and the volume survives container restarts

#### Scenario: Data survives server restart

- **WHEN** an MCP server writes data to its persistent volume, then crashes and restarts
- **THEN** the restarted container has access to all previously written data in the mounted volume
