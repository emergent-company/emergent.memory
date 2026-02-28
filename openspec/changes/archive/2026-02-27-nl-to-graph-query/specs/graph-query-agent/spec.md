## ADDED Requirements

### Requirement: Default graph-query-agent definition

The system SHALL provide a default `graph-query-agent` agent definition that can be installed per-project, configured with graph/search MCP tools and a system prompt optimized for knowledge graph querying.

#### Scenario: Install default graph-query-agent for a project

- **WHEN** an admin calls `POST /api/admin/projects/:projectId/install-default-agents`
- **THEN** the system SHALL create an `AgentDefinition` record in `kb.agent_definitions` with name `graph-query-agent`
- **AND** the definition SHALL have `is_default` set to `true`
- **AND** the definition SHALL be scoped to the specified project (`project_id`)
- **AND** the definition SHALL have `flow_type` set to `single`
- **AND** the definition SHALL have `visibility` set to `project`

#### Scenario: Idempotent installation

- **WHEN** an admin calls the install endpoint for a project that already has a `graph-query-agent` definition
- **THEN** the system SHALL NOT create a duplicate definition
- **AND** the system SHALL return the existing definition

### Requirement: Graph-query-agent tool whitelist

The graph-query-agent definition SHALL include a curated set of MCP tools that cover search, entity querying, relationship traversal, and schema inspection.

#### Scenario: Tool whitelist contents

- **WHEN** a graph-query-agent definition is created
- **THEN** its `tools` array SHALL include: `hybrid_search`, `query_entities`, `search_entities`, `semantic_search`, `find_similar`, `get_entity_edges`, `traverse_graph`, `list_entity_types`, `schema_version`, `list_relationships`
- **AND** it SHALL NOT include coordination tools (`spawn_agents`, `list_available_agents`)
- **AND** it SHALL NOT include mutation tools (`create_entity`, `update_entity`, `delete_entity`, `create_relationship`)

#### Scenario: ToolPool resolves whitelisted tools

- **WHEN** the agent executor resolves tools for a graph-query-agent
- **THEN** the ToolPool SHALL return only the tools listed in the definition's `tools` array
- **AND** each resolved tool SHALL be a functioning ADK `FunctionTool` that delegates to `mcp.Service.ExecuteTool()`

### Requirement: Graph-query-agent model configuration

The graph-query-agent definition SHALL use a model configuration optimized for precise, factual graph querying rather than creative generation.

#### Scenario: Default model settings

- **WHEN** a graph-query-agent definition is created with default settings
- **THEN** the model config SHALL specify `gemini-2.0-flash` as the model name
- **AND** the temperature SHALL be `0.1`
- **AND** `max_steps` SHALL be `15`

### Requirement: Graph-query-agent system prompt

The graph-query-agent SHALL have a system prompt that instructs the LLM to use tools for data lookup, cite results, and avoid fabrication.

#### Scenario: System prompt behavior - use tools for data

- **WHEN** a user asks a factual question about the knowledge graph (e.g., "What entities are of type Decision?")
- **THEN** the agent SHALL invoke a graph tool (e.g., `query_entities`) to look up real data
- **AND** the agent SHALL NOT answer from its training data or fabricate entities

#### Scenario: System prompt behavior - cite results

- **WHEN** the agent retrieves entities or relationships via tools
- **THEN** the agent SHALL reference the specific entities and relationships found in its response
- **AND** the agent SHALL indicate the entity types and relationship types involved

#### Scenario: System prompt behavior - handle empty results

- **WHEN** a graph tool returns no results for a query
- **THEN** the agent SHALL clearly state that no matching data was found
- **AND** the agent SHALL NOT fabricate or hallucinate results

#### Scenario: Multi-step graph exploration

- **WHEN** a user asks a question requiring multiple lookups (e.g., "What are all the Decisions and how do they relate to each other?")
- **THEN** the agent SHALL chain multiple tool calls (e.g., `query_entities` then `get_entity_edges` or `traverse_graph`)
- **AND** the total tool calls SHALL NOT exceed `max_steps` (15)
