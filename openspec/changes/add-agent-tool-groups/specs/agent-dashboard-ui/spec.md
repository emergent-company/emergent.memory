## MODIFIED Requirements

### Requirement: Choose tools from available MCP tools

The Settings Tools panel SHALL let the user pick tools as checkboxes, SHALL organize them by capability group with the existing MCP-server and relay-node listings nested as sub-groups, SHALL NOT require tool names as free text, and SHALL preserve tools that no registered MCP server, connected relay node, or known capability group covers.

#### Scenario: Tools grouped by capability

- **WHEN** the Tools panel loads and the agent definition reports tool groups
- **THEN** each group with at least one member tool — whether in the project's catalog or already referenced by the agent (allowed or banned) — is listed as a collapsible group header, and each group's `tools` is its full membership

#### Scenario: Tools grouped by server

- **WHEN** the Tools panel loads and MCP servers with tools exist
- **THEN** each server's tools are listed as checkboxes, checked when the agent already has the tool, nested under the capability group that owns them

#### Scenario: Relay node tools listed by node

- **WHEN** the Tools panel loads and connected relay nodes with tools exist
- **THEN** each node's tools are listed in a group labelled with that node, using their agent-facing `<instance>_<tool>` names, checked when the agent already has the tool

#### Scenario: No relay nodes connected

- **WHEN** the Tools panel loads and no relay nodes are connected
- **THEN** no relay node group is shown and the panel otherwise behaves as before

#### Scenario: Preserve unlisted tools

- **WHEN** the agent has a tool that no registered MCP server, connected relay node, or capability group covers
- **THEN** that tool is shown in a fallback group, checked, so it is not dropped on save

#### Scenario: Server reports no groups

- **WHEN** the agent definition reports no tool groups
- **THEN** the panel falls back to the previous source-only grouping and remains usable

#### Scenario: Group with no members is hidden

- **WHEN** a tool group has no member tool in the project's available catalog and no tool in the agent's allowed or banned tools
- **THEN** that group's header is not rendered

#### Scenario: Disabled group still renders

- **WHEN** a tool group's every member is banned or absent from the agent's allowed tools
- **THEN** that group's header still renders with its enable control off, so it can be re-enabled

## ADDED Requirements

### Requirement: Group-level enable and approval controls

Each rendered tool group header SHALL carry an enable control that turns the whole group on or off and a policy control offering `Inherit`, `Allow`, `Ask`, and `Deny`. Turning a group off SHALL remove its members from the agent's tools and ban them; turning it on SHALL restore them. The policy control SHALL write the group policy without writing a per-tool entry for each member.

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
