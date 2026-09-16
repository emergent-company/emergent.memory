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

### Requirement: Link to the memories browser

The dashboard SHALL provide a link to the memories subpage.

#### Scenario: Memories link present

- **WHEN** the dashboard loads
- **THEN** a link to the memories subpage is shown

### Requirement: Navigate agent sections

The agent pages SHALL show a vertical sub-menu with Dashboard, Settings, and Sessions sections scoped to that agent.

#### Scenario: Sub-menu present

- **WHEN** an agent page loads
- **THEN** the sub-menu shows Dashboard, Settings, and Sessions links for that agent only

### Requirement: Edit an agent in-page

The Settings section SHALL show a form organized into panels (section header + panel), one per concern: General (name, system prompt), Model (model, temperature, max tokens), Tools, Skills, and Delegation. It SHALL NOT use a modal.

#### Scenario: Edit form shows current values

- **WHEN** the Settings section loads
- **THEN** the form shows the agent's current values for every editable field

#### Scenario: Save changes

- **WHEN** the user submits the form with valid values
- **THEN** the changes persist and the page shows a success message

#### Scenario: Save rejected

- **WHEN** the name is empty or delegation is enabled with no targets
- **THEN** the page shows an error and does not persist the change

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

### Requirement: List memory objects

The memories subpage SHALL list memory objects, each showing its content, category, and confidence.

#### Scenario: Memories present

- **WHEN** the memories subpage loads and memory objects exist
- **THEN** the objects are listed, each showing content, category, and confidence

#### Scenario: No memories

- **WHEN** the memories subpage loads and no memory objects exist
- **THEN** a clear "no memories yet" state is shown

### Requirement: Search memory objects

The memories subpage SHALL let the user search memory objects and display the matching results.

#### Scenario: Search with matches

- **WHEN** the user enters a query that matches stored objects
- **THEN** the matching objects are listed

#### Scenario: Search with no matches

- **WHEN** the user enters a query with no matching objects
- **THEN** a clear "no matches" state is shown

### Requirement: Show a memory's full content

Selecting a memory SHALL show its full content.

#### Scenario: Open a memory detail

- **WHEN** the user selects a memory in the list
- **THEN** the memory's full content is displayed

### Requirement: Surface load failures without crashing

The dashboard and memories subpage SHALL show a clear error state when a backend fetch fails and SHALL NOT render a broken page.

#### Scenario: Backend unreachable

- **WHEN** a required backend request fails
- **THEN** the affected section shows an error message while the rest of the page remains usable

