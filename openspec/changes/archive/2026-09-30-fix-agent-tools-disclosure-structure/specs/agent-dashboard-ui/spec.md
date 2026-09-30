## MODIFIED Requirements

### Requirement: Choose tools from available MCP tools

The Settings Tools panel SHALL let the user pick tools as checkboxes, SHALL group them by **source** first, SHALL NOT require tool names as free text, and SHALL preserve tools that no registered MCP server, connected relay node, or known capability group covers.

The top-level dimension SHALL be source: a collapsible **Built-in** section for memory's native tools, plus one collapsible sibling block per external MCP server and per connected relay node. Inside Built-in, capability groups SHALL each render with the existing group enable switch on their `<summary>` trigger and their tri-state policy select in the group **body** (never inside the interactive `<summary>`), and their member tools SHALL render as direct rows (no nested per-server sub-group). External MCP servers SHALL list their own tools directly with per-tool policy selects; relay nodes SHALL list theirs directly with the remote badge and no per-tool policy.

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
- **THEN** a collapsible "Built-in" section renders at the top, holding each capability group with at least one native member as a nested collapsible group whose `<summary>` carries its enable switch and whose body carries its approval-policy select

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

### Requirement: Tools picker inheritance updates live without collapsing the group

On the Settings Tools panel the displayed policy inheritance SHALL stay in step
with the current selections before a save: changing the agent default policy or a
capability group's policy SHALL recompute every policy select's
`Inherit (<value>)` label client-side, using the same resolution order the server
uses (explicit tool policy → owning group policy → agent default). A tool select
outside any capability group SHALL reflect the agent default. Changing a group
policy SHALL NOT fold or unfold the group because the group policy select SHALL
render in the group body, not inside the interactive `<summary>`: a click on it
SHALL NOT run the `<summary>` activation behaviour and SHALL NOT be
`preventDefault`ed.

#### Scenario: Changing the default relabels every inheriting select

- **WHEN** the owner changes the default approval policy and no group policy is set
- **THEN** every group select and every per-tool select shows `Inherit (<new default>)`

#### Scenario: Changing a group policy relabels that group's tools

- **WHEN** the owner changes a capability group's policy to `Deny`
- **THEN** each tool select in that group shows `Inherit (Deny)` while the group select continues to show `Inherit (<agent default>)`, and tool selects outside the group are unchanged

#### Scenario: Group policy select does not fold the group

- **WHEN** the owner picks an option in a capability group's policy select, which lives in the group body
- **THEN** the group stays open/closed as it was — the disclosure does not toggle, and the select's click is not default-prevented
