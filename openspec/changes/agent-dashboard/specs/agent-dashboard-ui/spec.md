## Purpose

A web UI agent details screen — the agent dashboard — where a user can see one agent's summary, its configured tools, and its recent chats, and follow a link to a subpage that mirrors the iOS memory browser (a searchable list of that agent's memories with a detail view).

## ADDED Requirements

### Requirement: Navigate to an agent's details screen

The agents list SHALL link each agent to its own details screen.

#### Scenario: Open details from the agents list

- **WHEN** a user selects an agent in the agents list
- **THEN** the dashboard for that agent opens, scoped to that agent only

### Requirement: Show the agent summary

The dashboard SHALL display the agent's name, backend type, model, enabled state, and version.

#### Scenario: Dashboard shows summary

- **WHEN** the dashboard for an agent loads successfully
- **THEN** the agent's name, backend type, model, enabled state, and version are all shown

### Requirement: Show configured tools

The dashboard SHALL display the agent's configured tools: its MCP servers (by name) together with their allowed tool names, and its built-in function names.

#### Scenario: Agent with MCP servers and built-in functions

- **WHEN** the dashboard loads for an agent that has MCP tool references and built-in functions
- **THEN** the MCP servers are shown by name with their allowed tools, and the built-in functions are listed

#### Scenario: Agent with no tools

- **WHEN** the dashboard loads for an agent with no MCP references and no built-in functions
- **THEN** the dashboard shows a clear "no tools configured" state

### Requirement: Show the agent's recent chats

The dashboard SHALL list the agent's most recent sessions (not sessions belonging to other agents), most recent first, each linking to the full session timeline.

#### Scenario: Agent has sessions

- **WHEN** the dashboard loads for an agent that has recorded sessions
- **THEN** those sessions are listed most recent first, and selecting one opens its session timeline

#### Scenario: Agent has no sessions

- **WHEN** the dashboard loads for an agent with no recorded sessions
- **THEN** the dashboard shows a clear "no sessions yet" state

### Requirement: Gate the memories entry point on memory capability

The dashboard SHALL show a link to the memories subpage only when the agent is memory-capable (references the `memory` MCP server).

#### Scenario: Memory-capable agent

- **WHEN** the dashboard loads for a memory-capable agent
- **THEN** a link to the memories subpage is shown

#### Scenario: Non-memory agent

- **WHEN** the dashboard loads for an agent that is not memory-capable
- **THEN** no memories link is shown

### Requirement: List the agent's memories

The memories subpage SHALL list the agent's memories, each showing its content, category, and confidence.

#### Scenario: Memory-capable agent with memories

- **WHEN** the memories subpage loads for a memory-capable agent that has memories
- **THEN** the memories are listed, each showing content, category, and confidence

#### Scenario: No memories

- **WHEN** the memories subpage loads for an agent with no memories
- **THEN** a clear "no memories yet" state is shown

### Requirement: Search the agent's memories

The memories subpage SHALL let the user search the agent's memories and display the matching results.

#### Scenario: Search with matches

- **WHEN** the user enters a search query that matches stored memories
- **THEN** the matching memories are listed

#### Scenario: Search with no matches

- **WHEN** the user enters a search query with no matching memories
- **THEN** a clear "no matches" state is shown

### Requirement: Show a memory's full content

Selecting a memory SHALL show its full content.

#### Scenario: Open a memory detail

- **WHEN** the user selects a memory in the list
- **THEN** the memory's full content is displayed

### Requirement: Surface load failures without crashing

The dashboard and memories subpage SHALL show a clear error state when a backend fetch fails and SHALL NOT render a broken page.

#### Scenario: Backend unreachable

- **WHEN** a required backend request fails (for example, the control plane or admin server is down)
- **THEN** the affected section shows an error message while the rest of the page remains usable

#### Scenario: Memory service unavailable

- **WHEN** the memories subpage loads and the memory service is unreachable
- **THEN** an error state is shown instead of a broken or falsely empty list
