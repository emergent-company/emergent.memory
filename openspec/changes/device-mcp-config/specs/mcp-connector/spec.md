## MODIFIED Requirements

### Requirement: Host local MCP servers and share a selected tool subset

The connector SHALL host arbitrary local MCP servers configured under `mcp_servers` and share a user-selected subset of their tools through the same relay/tool mechanism as the built-in tools. Each server entry SHALL specify a unique `name`, a `transport` of `stdio`, `http`, or `sse`, and the transport-specific connection fields (`command`/`args`/`env` for stdio; `url`/`headers` for http/sse). An omitted `enabled` defaults to true, and each server SHALL support a per-server `disabled_tools` deny list of its own (un-namespaced) tool names. The connector SHALL connect outbound only — hosted servers SHALL NOT require any inbound port on the connector host. Server entries MAY be locally configured (from the connector config file or its loopback management API) or Memory-managed (pushed by Memory over the relay). For locally-configured servers, credentials (commands, URLs, headers, env) SHALL remain local and never be sent to Memory. For Memory-managed servers, credentials MAY be received from Memory over the authenticated relay and SHALL be stored locally; the connector SHALL never return credential values to Memory.

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

- **WHEN** a locally-configured hosted server is registered through the relay
- **THEN** Memory receives only the tool names, descriptions, and input schemas — never that server's command, arguments, environment, URL, or headers

## ADDED Requirements

### Requirement: Apply Memory-managed MCP configuration pushed over the relay

The connector SHALL accept a configuration frame from the relay carrying Memory-managed MCP server
entries and apply it without a process restart, and SHALL do so only when its operator has opted in
to server-managed configuration (an explicit setting that defaults to off and is advertised to the
hub at registration). On receiving an accepted frame the connector SHALL validate the entries, apply
them through the same live reload path used by the management API, and only after a successful
reload persist the Memory-managed set, then re-register with the hub so the updated tool list is
visible immediately. The apply SHALL run off the relay read loop and be isolated from panics, and
SHALL reject a frame whose version is not newer than the last applied version. The Memory-managed
set SHALL be authoritative on a name collision with a locally-configured entry, while
locally-configured entries with no Memory-managed counterpart SHALL be preserved. A frame that fails
validation or reload SHALL leave both the running configuration and the persisted set untouched and
SHALL be acknowledged with an error. Credential values received in a frame SHALL be stored locally
and SHALL NOT be echoed to Memory in any acknowledgement, registration, or status payload, and the
received frame and its credential values SHALL NOT be logged.

#### Scenario: Pushed config applies live

- **WHEN** an opted-in connector receives a valid, newer configuration frame with an enabled server and the server is reachable
- **THEN** the connector validates it, connects the server, persists it after a successful reload, and re-registers so the hub sees the new tool list without restarting

#### Scenario: Opted-out connector ignores pushes

- **WHEN** a connector has not enabled server-managed configuration
- **THEN** it does not apply a configuration frame and advertises no acceptance at registration

#### Scenario: Server-managed wins on name collision

- **WHEN** a pushed Memory-managed entry has the same name as a locally-configured entry
- **THEN** the Memory-managed entry is applied and takes precedence for the running process

#### Scenario: Local-only entries are preserved

- **WHEN** a pushed configuration omits a locally-configured server name
- **THEN** that locally-configured server remains hosted and registered

#### Scenario: Invalid pushed config is rejected without side effects

- **WHEN** a configuration frame contains an unknown transport, a missing required field, duplicate names, an illegal header or env key, a credential value containing a newline, or a configuration that fails to start (for example a tool-name collision)
- **THEN** neither the running configuration nor the persisted Memory-managed set changes, and the connector acknowledges the frame with an error naming the problem

#### Scenario: Stale frame rejected

- **WHEN** a configuration frame is not newer than the last applied version
- **THEN** the connector leaves its configuration unchanged and acknowledges the frame as stale

#### Scenario: Oversized frame is an error, not a disconnect

- **WHEN** a configuration frame exceeds the connector's read limit
- **THEN** the connector acknowledges it as an error rather than dropping the relay connection

#### Scenario: Apply does not block the relay or crash the connector

- **WHEN** a configuration apply is slow or panics
- **THEN** the relay read loop keeps servicing keepalives and the connector keeps running

#### Scenario: Received credentials are not echoed or logged

- **WHEN** the connector receives credential values in a configuration frame
- **THEN** it stores them locally, logs neither the frame nor the values, and returns only status such as success and tool count to Memory
