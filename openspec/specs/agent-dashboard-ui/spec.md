# agent-dashboard-ui Specification

## Purpose
A gateway web UI agent dashboard where a user can see one agent's summary, its configured tools, and its recent chats, edit the agent's settings in-page, browse its full session list, and follow a link to a subpage that browses the project's memory objects with search and detail views.

## Requirements

### Requirement: Navigate to an agent's dashboard

The agents list SHALL link each agent to its own dashboard.

#### Scenario: Open dashboard from the agents list

- **WHEN** a user selects an agent in the agents list
- **THEN** the dashboard for that agent opens, scoped to that agent only

### Requirement: Show the agent summary

The dashboard SHALL display the agent's name, model, flow type, visibility, tool count, and description.

#### Scenario: Dashboard shows summary

- **WHEN** the dashboard for an agent loads successfully
- **THEN** the agent's name, model, flow type, visibility, tool count, and description are shown

### Requirement: Show configured tools

The dashboard SHALL display the agent's configured tools: the allowed tool names and any banned tool names.

#### Scenario: Agent with tools

- **WHEN** the dashboard loads for an agent that has allowed tools
- **THEN** the allowed tool names are listed

#### Scenario: Agent with no tools

- **WHEN** the dashboard loads for an agent with no allowed tools
- **THEN** a clear "no tools configured" state is shown

### Requirement: Show the agent's recent chats

The dashboard SHALL list the agent's most recent conversations (not other agents'), most recent first, each linking to its chat.

#### Scenario: Agent has conversations

- **WHEN** the dashboard loads for an agent that has conversations
- **THEN** those conversations are listed most recent first, and selecting one opens its chat

#### Scenario: Agent has no conversations

- **WHEN** the dashboard loads for an agent with no conversations
- **THEN** a clear "no chats yet" state is shown

### Requirement: Navigate agent sections

The agent pages SHALL show a vertical sub-menu with Dashboard, Settings, Sandbox, and Sessions sections scoped to that agent. On any agent Settings subpage, the Settings entry SHALL expand into a group of six indented subpage links — General, Model, Tools, Skills, Delegation, and MCP sharing — with the active subpage highlighted.

#### Scenario: Sub-menu present

- **WHEN** an agent page loads
- **THEN** the sub-menu shows Dashboard, Settings, Sandbox, and Sessions links for that agent only

#### Scenario: Settings group lists the six subpages

- **WHEN** an agent Settings subpage loads
- **THEN** the Settings entry shows six indented children — General, Model, Tools, Skills, Delegation, and MCP sharing — and the current subpage is highlighted

#### Scenario: Subpages are distinct routes

- **WHEN** the owner opens any Settings subpage
- **THEN** each subpage renders only its own panel at `/agents/:id/settings[/<section>]`, and an unknown section returns 404

### Requirement: Edit an agent in-page

The Settings surface SHALL be split into one form per concern — General (name, system prompt, language), Model (model, temperature, max tokens), Tools (default approval + tool picker), Skills, and Delegation — each with its own Save action that persists ONLY that concern's fields. It SHALL NOT use a modal.

#### Scenario: Edit form shows current values

- **WHEN** a Settings subpage loads
- **THEN** the form shows the agent's current values for that concern's fields

#### Scenario: Save changes

- **WHEN** the owner submits a section's form with valid values
- **THEN** only that section's fields change, every other section's fields are preserved, and the page shows a success message

#### Scenario: Save rejected

- **WHEN** the name is empty or delegation is enabled with no targets
- **THEN** the page shows an error and does not persist the change

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

### Requirement: Open settings from the agents list

The agents list edit action SHALL link to the agent's Settings section.

#### Scenario: Edit link present

- **WHEN** the agents list loads
- **THEN** each agent's edit action links to that agent's Settings section

### Requirement: List an agent's sessions

The Sessions section SHALL list that agent's conversations, most recent first, each linking to its chat.

#### Scenario: Agent has sessions

- **WHEN** the Sessions section loads and the agent has conversations
- **THEN** those conversations are listed most recent first, and selecting one opens its chat

#### Scenario: Agent has no sessions

- **WHEN** the Sessions section loads and the agent has no conversations
- **THEN** a clear empty state is shown

### Requirement: Surface load failures without crashing

The dashboard and memories subpage SHALL show a clear error state when a backend fetch fails and SHALL NOT render a broken page.

#### Scenario: Backend unreachable

- **WHEN** a required backend request fails
- **THEN** the affected section shows an error message while the rest of the page remains usable

### Requirement: Link to objects created/updated by this agent

The dashboard SHALL provide a link to a subpage that browses the project's objects filtered by provenance — objects created by or updated by this agent.

#### Scenario: Objects link present

- **WHEN** the dashboard loads
- **THEN** a link to the agent's created/updated objects subpage is shown

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
