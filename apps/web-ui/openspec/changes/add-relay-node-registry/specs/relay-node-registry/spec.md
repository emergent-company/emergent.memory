## Purpose

Keeps a per-project record of external MCP relay nodes observed by the gateway,
so the Memory UI can show nodes that are currently offline (with a last-seen
time) and let a user remove stale entries.

## ADDED Requirements

### Requirement: Persist observed relay nodes

The gateway SHALL record each relay node observed for the project in a
per-project store, capturing instance id, version, tool count, a tool snapshot,
first-seen and last-seen timestamps, and SHALL update the record on every
observation.

#### Scenario: Live node is recorded

- **WHEN** the MCP nodes page loads and the backend reports a connected session
- **THEN** the node's record is created (first) or refreshed (last-seen, version, tool count, tools)

#### Scenario: Record survives disconnection

- **WHEN** a recorded node is no longer present in the backend sessions
- **THEN** its record remains and is marked offline

### Requirement: Show offline state and last-seen

The MCP nodes page SHALL distinguish live from offline nodes and SHALL show the
last-seen time for offline nodes.

#### Scenario: Offline node listed

- **WHEN** a recorded node is offline
- **THEN** the page shows it with an offline marker and its last-seen time

#### Scenario: Live node listed

- **WHEN** a node is currently connected
- **THEN** the page shows it as live with its connection details

### Requirement: Serve tools for offline nodes

The page SHALL show a node's tools from the cached snapshot when the node is
offline, and from the live backend when it is connected.

#### Scenario: Tools for an offline node

- **WHEN** the user selects an offline node
- **THEN** the tool list comes from the stored snapshot (or a clear "no tools recorded" state)

### Requirement: Remove a stored node

The page SHALL let the user remove a stored node from the registry.

#### Scenario: Remove an offline node

- **WHEN** the user removes an offline node
- **THEN** its record is deleted and it no longer appears on the page

#### Scenario: Live node removal reappears

- **WHEN** the user removes a node that is still connected
- **THEN** the record is deleted, and the node reappears on the next page load because the live session is re-observed
