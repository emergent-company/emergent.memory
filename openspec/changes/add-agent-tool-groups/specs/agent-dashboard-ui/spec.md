## MODIFIED Requirements

### Requirement: Choose tools from available MCP tools

The Settings Tools panel SHALL let the user pick tools as checkboxes, SHALL group them by **source** first, SHALL NOT require tool names as free text, and SHALL preserve tools that no registered MCP server, connected relay node, or known capability group covers.

The top-level dimension SHALL be source: a collapsible **Built-in** section for memory's native tools, plus one collapsible sibling block per external MCP server and per connected relay node. Inside Built-in, capability groups SHALL each render with the existing group enable switch and tri-state policy select, and their member tools SHALL render as direct rows (no nested per-server sub-group). External MCP servers SHALL list their own tools directly with per-tool policy selects; relay nodes SHALL list theirs directly with the remote badge and no per-tool policy.

#### Scenario: Built-in is the top-level native-tools section

- **WHEN** the Tools panel loads and the agent definition reports tool groups
- **THEN** a collapsible "Built-in" section renders at the top, holding each capability group with at least one native member as a nested collapsible group header carrying its enable switch and policy select

#### Scenario: Capability group members render as direct rows

- **WHEN** a capability group inside Built-in loads
- **THEN** each of its native member tools (from the builtin server, or a native tool no source offers) renders as a direct checkbox row, and no nested "builtin" server sub-group is shown

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

#### Scenario: Server reports no groups

- **WHEN** the agent definition reports no tool groups
- **THEN** the panel falls back to the previous source-only grouping and remains usable

#### Scenario: Group with no native members is hidden

- **WHEN** a capability group's members are all offered by external servers or relay nodes
- **THEN** that group renders no header inside Built-in, and its members render in their own source blocks

#### Scenario: Disabled group still renders

- **WHEN** a tool group's every member is banned or absent from the agent's allowed tools
- **THEN** that group's header still renders with its enable control off, so it can be re-enabled

## ADDED Requirements

### Requirement: Group-level enable and approval controls

Each rendered tool group header SHALL carry an enable control that turns the whole group on or off. A policy control offering `Inherit`, `Allow`, `Ask`, and `Deny` SHALL be rendered on every capability group except the fallback group, which carries the enable control only (it is display-only and never a policy source). External MCP-server and relay-node source blocks SHALL offer enable/disable only, with no group policy control. Turning a group off SHALL remove its members from the agent's tools and ban them; turning it on SHALL restore them. The policy control SHALL write the group policy without writing a per-tool entry for each member.

#### Scenario: Group enable control reflects state

- **WHEN** a group has at least one member tool enabled on the agent
- **THEN** the group's enable control renders as on, otherwise as off

#### Scenario: Turning a group off persists membership and bans

- **WHEN** the user turns a group off and saves
- **THEN** the saved agent has the group's member tools removed from its tools and recorded as banned tools

#### Scenario: Turning a group on restores membership

- **WHEN** the user turns a disabled group on and saves
- **THEN** the saved agent has the group's member tools in its tools and absent from its banned tools

#### Scenario: Group policy persists as a group entry

- **WHEN** the user sets a group's policy to `Ask` and saves
- **THEN** the saved agent carries a group-level policy entry for that group and no new per-tool entries for its members

#### Scenario: Inherit clears the group policy

- **WHEN** the user sets a group's policy to `Inherit` and saves
- **THEN** the saved agent carries no group-level policy entry for that group

#### Scenario: Policy control reflects the stored policy

- **WHEN** a group has a stored policy of `Deny`
- **THEN** the group's policy control renders `Deny`

### Requirement: Per-tool override remains visible under grouping

A tool row inside a group SHALL retain its own policy control, and a tool with no explicit policy entry SHALL indicate the group policy it inherits.

#### Scenario: Inherited policy is indicated

- **WHEN** a tool row has no explicit policy entry and its group has a stored policy
- **THEN** the row indicates that it inherits that group's policy

#### Scenario: Explicit tool policy is shown as the row value

- **WHEN** a tool row has an explicit policy entry
- **THEN** the row shows that entry's value rather than the inherited one

#### Scenario: Delegation-managed tools are excluded

- **WHEN** the Tools panel renders groups and group enable or disable is applied
- **THEN** the delegation-managed tools are neither shown in a group nor modified by group enable or disable
