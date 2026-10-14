## ADDED Requirements

### Requirement: Device-targeted servers render as device rows

The gateway SHALL render a device-targeted MCP server (`target = 'device'`)
distinctly from an outbound server on the MCP Servers list: the row SHALL show
a `device` badge and, when the server is bound to a specific connector, its
target instance id. The row SHALL NOT offer the host-only outbound affordances
(sync, inspect, or OAuth connect), because the shared host never dials a
device-targeted server. Instead the expanded row SHALL explain that the server
runs on a connector device, and the "no tools cached" hint SHALL say tools are
discovered on the device rather than directing the user to run a host sync.

#### Scenario: Device row shows its target

- **WHEN** the MCP Servers list renders a device-targeted server
- **THEN** the row shows a `device` badge rather than rendering as an ordinary outbound server

#### Scenario: Bound device shows its instance id

- **WHEN** the MCP Servers list renders a device-targeted server bound to a specific connector instance
- **THEN** the row shows that target instance id alongside the `device` badge

#### Scenario: Broadcast device shows no instance id

- **WHEN** the MCP Servers list renders a device-targeted server with no bound instance (broadcast)
- **THEN** the row shows the `device` badge and no instance id

#### Scenario: Device row drops outbound affordances

- **WHEN** the MCP Servers list renders a device-targeted server
- **THEN** the row offers no sync, inspect, or connect actions, while an outbound server's row still does

#### Scenario: Device row explains where it runs

- **WHEN** the user expands a device-targeted server's row
- **THEN** the row explains that the server runs on a connector device, and the empty tools hint does not direct the user to run a host sync
