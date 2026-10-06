# device-mcp-config Specification

## ADDED Requirements

### Requirement: Target MCP servers at connected devices

Memory SHALL allow an MCP server registry entry to be targeted at a connected relay device
instead of being connected outbound by the server. A device-targeted entry SHALL carry a target of
`device` and an optional `target_instance_id`; an absent instance id SHALL mean every connected
device of the project that is eligible under the authorization and opt-in rules. Device-targeted
entries SHALL reuse the existing server transport fields and encrypted secret storage, and SHALL be
created, read, updated, and deleted through the same project-scoped API and agent tools as
server-targeted entries. A `builtin` server type SHALL NOT be permitted with a device target.

#### Scenario: Create a device-targeted server

- **WHEN** an authorized caller creates an MCP server with target `device`
- **THEN** the entry is stored with that target and the server does not attempt to connect to it

#### Scenario: List device-targeted servers

- **WHEN** a client or the assistant lists MCP servers filtered to a connected instance
- **THEN** entries targeted at that instance and entries targeted at all devices are returned, and server-targeted entries are excluded

#### Scenario: Server-targeted behaviour is unchanged

- **WHEN** an MCP server is created without a device target
- **THEN** it defaults to target `server` and is connected and tool-synced exactly as before

#### Scenario: Invalid target rejected

- **WHEN** a create or update supplies a target other than `server` or `device`, or targets a `builtin` server at a device
- **THEN** the request fails with a clear validation error and nothing is stored

### Requirement: Device-targeted servers are never connected outbound by the server

Memory SHALL exclude device-targeted entries from every server-side outbound path: the agent tool
pool query, the server-slug tool resolver, tool discovery/sync, server inspection, and direct tool
invocation. A device-targeted entry SHALL never be connected to, have its tools imported, or be
called by the server; its tools are served only by the device through the relay.

#### Scenario: Device row excluded from the tool pool

- **WHEN** the agent tool pool is built for a project containing a device-targeted server
- **THEN** that server's tools are absent from the pool and no outbound connection is attempted

#### Scenario: Device row cannot be inspected or called server-side

- **WHEN** a caller attempts to inspect or directly invoke a device-targeted server by id
- **THEN** the request fails clearly rather than opening an outbound connection

### Requirement: Device opt-in and owner authorization for configuration push

A device SHALL receive server-managed configuration only when it has explicitly opted in, and only
from an authorized caller. The relay register frame SHALL carry an `accepts_managed_config` flag
that defaults to false and is true only when the device operator enabled it, and the registering
authenticated subject SHALL be recorded as the device's owner. A device-targeted create, update, or
delete SHALL be rejected unless the caller is the target device's owner or a project admin; a
broadcast (no instance id) SHALL be permitted only to a project admin and SHALL be delivered to
every opted-in device of the project. There SHALL be no project-member-reachable HTTP endpoint that
pushes configuration; pushes originate in-process.

#### Scenario: Non-opted-in device is not pushed to

- **WHEN** a device registers without `accepts_managed_config`
- **THEN** the server stores the entry but sends it no configuration frame

#### Scenario: Non-owner member is rejected

- **WHEN** a project member who is not the device owner (and not an admin) creates a device-targeted server for that instance
- **THEN** the request is rejected with an authorization error and nothing is stored or pushed

#### Scenario: Admin may broadcast

- **WHEN** a project admin creates a device-targeted server with no instance id
- **THEN** it is pushed to all opted-in devices of the project

#### Scenario: Non-admin broadcast rejected

- **WHEN** a project member who is not an admin creates a device-targeted server with no instance id
- **THEN** the request is rejected with an authorization error and nothing is stored or pushed

#### Scenario: No member-reachable push route

- **WHEN** the relay HTTP routes are enumerated
- **THEN** no route allows a project member to push configuration to a device

### Requirement: Server pushes device configuration over the relay

The relay SHALL support a server-to-device configuration frame, distinct from the tool-call request
frame, carrying the resolved list of MCP server configurations and a monotonic version. The server
SHALL send the frame to a matching session after a registry change and, for a device that elected to
receive it, after the device's registration acknowledgement (never before it). Every push SHALL be
non-blocking so a slow or offline device cannot stall registry writes or the registration
response. A device SHALL acknowledge a configuration frame with a result or an error, and SHALL
reject a frame whose version is not newer than the last applied version.

#### Scenario: Config pushed on change

- **WHEN** a device-targeted MCP server is created, updated, or deleted while its target opted-in instance is connected
- **THEN** the server sends a configuration frame to that session and the device acknowledges it

#### Scenario: Hydration follows the register acknowledgement

- **WHEN** an opted-in device registers or re-registers with the relay
- **THEN** the server writes the `registered` acknowledgement first, then sends the device's current resolved configuration

#### Scenario: Broadcast to all eligible devices

- **WHEN** an admin creates a device-targeted entry with no instance id
- **THEN** the configuration frame is sent to every opted-in connected device of the project

#### Scenario: Unreachable target is not an error

- **WHEN** a change targets a device that is not currently connected or has not opted in
- **THEN** the change is stored successfully and no push is made, with reconciliation attempted on the next eligible register

#### Scenario: A slow device does not block the caller

- **WHEN** a device is connected but slow to acknowledge
- **THEN** the registry write and registration response complete without waiting for the acknowledgement

#### Scenario: Stale frame rejected

- **WHEN** a device receives a configuration frame whose version is not newer than the last applied version
- **THEN** it does not change its configuration and acknowledges the frame as stale

### Requirement: Device configuration secrets are encrypted, not logged, and not echoed

For device-targeted servers, secret env values and HTTP headers SHALL be stored AES-GCM encrypted at
rest server-side and SHALL be returned by the API only as key names, never as values. They SHALL be
decrypted only inside the server's MCP registry service when building a push; the resolved frame and
the decrypted values SHALL NOT be logged at any layer. They SHALL be transmitted to the device only
over the authenticated relay connection for the device's own project, and the device SHALL never
send credential values back to Memory in any acknowledgement, registration, or status payload.

#### Scenario: Secret values are not readable back

- **WHEN** a client reads a device-targeted server that has secret env or header values
- **THEN** only the secret key names are returned, never the values

#### Scenario: Secrets delivered to the owning device only

- **WHEN** a device-targeted server with credentials is pushed to an opted-in device
- **THEN** the values are delivered over that project's authenticated relay connection and are stored locally on the device

#### Scenario: Secrets never appear in logs, acks, or status

- **WHEN** a configuration frame is pushed and acknowledged
- **THEN** no layer logs the frame or decrypted values, and the acknowledgement, registration, and status payloads contain no credential values
