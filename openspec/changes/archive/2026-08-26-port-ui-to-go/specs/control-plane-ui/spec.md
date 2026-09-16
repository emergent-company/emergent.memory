## Purpose

A web UI for configuring Alfred voice agents and their MCP servers through the control-plane API.

## ADDED Requirements

### Requirement: List agents
The UI SHALL display all configured agents with their name, enabled state, backend type, and version.

#### Scenario: View agent list
- **WHEN** the user opens the agents page
- **THEN** each agent is shown with its name, enabled/disabled state, backend type, and version

### Requirement: Create agent
The UI SHALL allow creating an agent with a name, system prompt, and a backend (openai_compat, realtime, or a2a).

#### Scenario: Create agent with realtime backend
- **WHEN** the user enters a name and system prompt and selects a realtime backend with provider, model, voice, and language
- **THEN** the agent is created via `POST /api/agents` and appears in the agent list

#### Scenario: Duplicate name rejected
- **WHEN** the user creates an agent whose name already exists
- **THEN** the UI displays the conflict error returned by the API (HTTP 409)

### Requirement: Edit agent
The UI SHALL allow editing an existing agent's fields (system prompt, backend, tools, sub-agents) and persist via full replacement.

#### Scenario: Edit system prompt
- **WHEN** the user edits an agent's system prompt and saves
- **THEN** the agent's system prompt is updated and its version is incremented

### Requirement: Delete agent
The UI SHALL allow deleting an agent.

#### Scenario: Delete agent
- **WHEN** the user deletes an agent and confirms
- **THEN** the agent is removed from the agent list

### Requirement: Activate and deactivate agent
The UI SHALL allow toggling an agent between enabled and disabled without deleting it.

#### Scenario: Deactivate agent
- **WHEN** the user deactivates an agent
- **THEN** the agent remains listed but is marked disabled

### Requirement: View agent status
The UI SHALL display an agent's status, including enabled state, version, and sub-agent references.

#### Scenario: Show agent status
- **WHEN** the user opens an agent's status view
- **THEN** the enabled state, version, and sub-agent references are shown

### Requirement: Manage MCP servers
The UI SHALL allow listing, creating, editing, and deleting MCP servers with name, URL, transport, and tool allowlist.

#### Scenario: Create MCP server
- **WHEN** the user creates an MCP server with a name, URL, and transport
- **THEN** the server is created via `POST /api/mcp-servers` and appears in the MCP server list

### Requirement: API authentication
The UI SHALL send the `X-API-Key` header on every control-plane API request and surface authentication failures.

#### Scenario: Invalid API key
- **WHEN** the control-plane API returns HTTP 401
- **THEN** the UI displays an authentication error and does not present a success state

### Requirement: Backend type form
The UI SHALL let the user select a backend type and present only the fields relevant to that type.

#### Scenario: Switch backend type
- **WHEN** the user selects a different backend type
- **THEN** the form shows the fields for that type (for example `base_url` for openai_compat, `voice` for realtime, `agent_card_url` for a2a)
