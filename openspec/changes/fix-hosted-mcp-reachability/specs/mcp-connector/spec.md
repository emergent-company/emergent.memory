## MODIFIED Requirements

### Requirement: Local management API for hosted MCP servers

The connector SHALL optionally expose a loopback-only HTTP management API so a companion UI can manage hosted MCP servers without hand-editing the config file. The API SHALL be disabled by default, enabled by `relay --api-port <port>`, and bound to `127.0.0.1` only. It SHALL provide: list/create servers (`GET`/`POST /api/mcp-servers`), read one (`GET /api/mcp-servers/{name}`), replace config (`PUT /api/mcp-servers/{name}/config`), enable/disable (`PUT /api/mcp-servers/{name}/enabled`), delete (`DELETE /api/mcp-servers/{name}`), list a server's discovered tools (`GET /api/mcp-servers/{name}/tools`), and connector status (`GET /api/status`). Every mutation SHALL be validated, persisted to the connector config file, and applied live (the relayed tool list updates) without restarting the process.

When `--api-port` is enabled the connector SHALL bind the loopback listener before it begins serving, and SHALL only report the API as listening once that bind has succeeded. A transient bind failure SHALL be retried for a bounded window, so a management port still held by a predecessor process during a restart is ridden out rather than lost. If the listener still cannot bind after the retry window, the relay SHALL exit non-zero rather than continue running with a management API that is not reachable.

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

#### Scenario: API is bound before it is served

- **WHEN** the management API is enabled
- **THEN** the relay binds the loopback listener before it begins serving and does not report the API as listening until the bind has succeeded

#### Scenario: Transient bind failure is retried

- **WHEN** the loopback management port is briefly held by a terminating predecessor process at startup
- **THEN** the connector retries the bind for a bounded window and begins serving once the port is released, rather than reporting the API as listening while it is unreachable

#### Scenario: Permanent bind failure fails fast

- **WHEN** the loopback management port cannot be bound after the retry window (e.g. held by an unrelated process)
- **THEN** the relay exits non-zero and reports the failure, rather than continuing to run with a dead management API
