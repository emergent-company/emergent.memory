## Purpose

A gateway web UI agent dashboard where a user can see one agent's summary, its configured tools, and its recent chats, and follow a link to a subpage that browses the project's memory objects with search and detail views.

## ADDED Requirements

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
