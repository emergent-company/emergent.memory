## MODIFIED Requirements

### Requirement: Choose tools from available MCP tools

The Settings Tools panel SHALL let the user pick tools as checkboxes grouped by MCP server, SHALL also list tools served by connected external MCP relay nodes in their own group(s) labelled by node, SHALL NOT require tool names as free text, and SHALL preserve tools that no registered MCP server or connected relay node offers.

#### Scenario: Tools grouped by server

- **WHEN** the Tools panel loads and MCP servers with tools exist
- **THEN** each server's tools are listed as checkboxes, checked when the agent already has the tool

#### Scenario: Relay node tools listed by node

- **WHEN** the Tools panel loads and connected relay nodes with tools exist
- **THEN** each node's tools are listed in a group labelled with that node, using their agent-facing `<instance>_<tool>` names, checked when the agent already has the tool

#### Scenario: No relay nodes connected

- **WHEN** the Tools panel loads and no relay nodes are connected
- **THEN** no relay node group is shown and the panel otherwise behaves as before

#### Scenario: Preserve unlisted tools

- **WHEN** the agent has a tool that no registered MCP server or connected relay node offers
- **THEN** that tool is shown in an "Other" group, checked, so it is not dropped on save
