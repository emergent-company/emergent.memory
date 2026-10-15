## Purpose

Let an operator connect, inspect, and disconnect a remote MCP server's OAuth
authorization from the `memory` CLI, mirroring the web UI's Connect/status/
Disconnect surface, so a headless or scripted setup can authorize an OAuth
protected server without driving the admin API by hand.

## ADDED Requirements

### Requirement: OAuth commands live under the mcp-servers group

The CLI SHALL expose an `oauth` subgroup of the existing
`memory agents mcp-servers` command with `connect`, `status`, and `disconnect`
subcommands. The subgroup SHALL target one MCP server by id, accepted as a
positional argument or resolved through the interactive server picker when
omitted.

#### Scenario: Command tree is registered

- **WHEN** an operator runs `memory agents mcp-servers oauth --help`
- **THEN** the output lists `connect`, `status`, and `disconnect`

#### Scenario: Server resolved by id or picker

- **WHEN** an operator omits the server id and stdin is a terminal
- **THEN** the command resolves the server through the interactive picker, matching the other mcp-servers subcommands

### Requirement: Connect starts the browser flow and polls for completion

The `connect` subcommand SHALL call the server's authorize endpoint to obtain an
authorization URL, print it and open the browser, then poll the status endpoint
until the server reports a connected credential for the operator, or a timeout
elapses. On success it SHALL report connected; on timeout or error it SHALL
report a failure with the status message and a non-zero exit, and SHALL never
print token material.

#### Scenario: Connect opens the browser and waits

- **WHEN** an operator runs `connect` for a server that requires authorization
- **THEN** the authorization URL is printed, the browser is opened, and the command polls until the server reports connected

#### Scenario: Connect times out

- **WHEN** the operator does not complete authorization within the poll window
- **THEN** the command exits non-zero with a timeout message

### Requirement: Status reports identity without tokens

The `status` subcommand SHALL print whether the server is connected for the
operator, whether the credential is shared or the operator's own, the
authorization server, granted scopes, and expiry, and SHALL never print an access
token, refresh token, or client secret.

#### Scenario: Status printed

- **WHEN** an operator runs `status` for a connected server
- **THEN** the output reports the connection state, identity (shared/personal), authorization server, scopes, and expiry

#### Scenario: No token leaked

- **WHEN** `status` prints
- **THEN** no access token, refresh token, or client secret value appears

### Requirement: Disconnect removes the operator's credential

The `disconnect` subcommand SHALL delete the operator's own stored OAuth
credential for the server (falling back to the shared credential when the
operator holds none), and SHALL confirm the server is disconnected.

#### Scenario: Disconnect removes the credential

- **WHEN** an operator runs `disconnect` for a connected server
- **THEN** the operator's credential is deleted and the command confirms the server is disconnected
