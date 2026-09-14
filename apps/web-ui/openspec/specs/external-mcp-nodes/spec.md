# external-mcp-nodes Specification

## Purpose

Lets a Memory user see external MCP relay nodes connected to their project and inspect the tools each node serves, so tools running on remote machines (e.g. Apple Notes/Reminders on a Mac) become visible and manageable in the Memory web UI.

## Requirements

### Requirement: List connected external MCP nodes

The web UI SHALL list the external MCP nodes currently connected to the project via the backend MCP relay, showing each node's instance id, version, tool count, and connection time, most recently connected first.

#### Scenario: Nodes connected

- **WHEN** the external MCP nodes page loads and the backend reports connected relay sessions
- **THEN** each session is listed with its instance id, version, tool count, and connection time

#### Scenario: No nodes connected

- **WHEN** the external MCP nodes page loads and no relay sessions exist
- **THEN** a clear "no external nodes connected" empty state is shown

#### Scenario: Relay API unreachable

- **WHEN** the backend relay API request fails
- **THEN** the page shows a clear error state and does not render a broken page

### Requirement: Inspect a node's tools

The web UI SHALL show the MCP tools served by a connected node, each identified by its agent-facing name (node instance id prefixed, e.g. `<instance>_<tool>`), so the user can see exactly which tool names an agent whitelist would reference.

#### Scenario: Node with tools

- **WHEN** the user selects a connected node
- **THEN** the node's tools are listed with their agent-facing prefixed names

#### Scenario: Node with no tools

- **WHEN** the user selects a connected node that serves no tools
- **THEN** a clear "no tools" state is shown

#### Scenario: Node disconnected between list and inspection

- **WHEN** the user selects a node that has disconnected since the list loaded
- **THEN** a clear "node not found or disconnected" state is shown

### Requirement: Identify relay-sourced tools

Relay-sourced tools SHALL be visually distinguishable from registry MCP server tools wherever tool lists are shown, so the user understands a tool originates from a remote machine.

#### Scenario: Tool lists distinguish sources

- **WHEN** a page shows tools from both a registry MCP server and a connected relay node
- **THEN** the relay node's tools are presented under a node label separate from the registry server's group
