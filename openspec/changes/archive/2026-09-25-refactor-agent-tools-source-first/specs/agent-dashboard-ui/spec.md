## MODIFIED Requirements

### Requirement: Choose tools from available MCP tools

The Settings Tools panel SHALL let the user pick tools as checkboxes, SHALL group them by **source** first, SHALL NOT require tool names as free text, and SHALL preserve tools that no registered MCP server, connected relay node, or known capability group covers.

The top-level dimension SHALL be source: a collapsible **Built-in** section for memory's native tools, plus one collapsible sibling block per external MCP server and per connected relay node. Inside Built-in, capability groups SHALL each render with the existing group enable switch and tri-state policy select, and their member tools SHALL render as direct rows (no nested per-server sub-group). External MCP servers SHALL list their own tools directly with per-tool policy selects; relay nodes SHALL list theirs directly with the remote badge and no per-tool policy.

#### Scenario: Tools grouped by server

- **WHEN** the Tools panel loads and MCP servers with tools exist
- **THEN** the builtin server's tools render as direct rows inside their Built-in capability groups, every non-builtin server renders as a top-level sibling block of Built-in listing its own tools as checkboxes with per-tool policy selects, and each box is checked when the agent already has the tool

#### Scenario: Relay node tools listed by node

- **WHEN** the Tools panel loads and connected relay nodes with tools exist
- **THEN** each node renders as a top-level sibling block of Built-in labelled with that node and a remote badge, using their agent-facing `<instance>_<tool>` names, checked when the agent already has the tool, and with no per-tool policy select

#### Scenario: No relay nodes connected

- **WHEN** the Tools panel loads and no relay nodes are connected
- **THEN** no relay node block is shown and the panel otherwise behaves as before

#### Scenario: Preserve unlisted tools

- **WHEN** the agent has a tool that no registered MCP server, connected relay node, or capability group covers
- **THEN** that tool is shown in a fallback group, checked, so it is not dropped on save

#### Scenario: Built-in is the top-level native-tools section

- **WHEN** the Tools panel loads and the agent definition reports tool groups
- **THEN** a collapsible "Built-in" section renders at the top, holding each capability group with at least one native member as a nested collapsible group header carrying its enable switch and policy select

#### Scenario: Capability group members render as direct rows

- **WHEN** a capability group inside Built-in loads
- **THEN** each of its native member tools (from the builtin server, or a native tool no source offers) renders as a direct checkbox row, and no nested "builtin" server sub-group is shown

#### Scenario: Source owns a tool once

- **WHEN** the Tools panel renders
- **THEN** every tool renders exactly once across the whole picker, in its own source block or capability group

#### Scenario: Group with no native members is hidden

- **WHEN** a capability group's members are all offered by external servers or relay nodes
- **THEN** that group renders no header inside Built-in, and its members render in their own source blocks

#### Scenario: Server reports no groups

- **WHEN** the agent definition reports no tool groups
- **THEN** the panel falls back to the previous source-only grouping and remains usable
