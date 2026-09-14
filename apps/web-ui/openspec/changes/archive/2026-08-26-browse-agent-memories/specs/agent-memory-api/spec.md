## Purpose

Provides a read-only HTTP API for retrieving an agent's stored memories by proxying the Emergent Memory service, scoped so only memory-enabled agents surface results.

## ADDED Requirements

### Requirement: Search an agent's memories

The API SHALL allow a client to search an agent's stored memories (notes, preferences, and facts) by query text and return the matching memories with their content.

#### Scenario: Search returns matches

- **WHEN** a client searches for a query against an agent that has stored memories
- **THEN** the API returns the matching memories with their content

#### Scenario: Search returns no matches

- **WHEN** a client searches and no stored memory matches the query
- **THEN** the API returns an empty result list, not an error

### Requirement: Report memory capability per agent

The API SHALL report, for each agent, whether the agent has memory configured.

#### Scenario: Memory-enabled agent

- **WHEN** a client queries agent info for an agent with a memory MCP server
- **THEN** the API reports the agent as memory-capable

#### Scenario: Agent without memory

- **WHEN** a client queries agent info for an agent without a memory MCP server
- **THEN** the API reports the agent as not memory-capable

### Requirement: Empty result for memory-less agents

The API SHALL return an empty result, not an error, when a client requests memories for an agent that has no memory configured.

#### Scenario: Search a non-memory agent

- **WHEN** a client searches memories for an agent without memory
- **THEN** the API returns an empty result list

### Requirement: Read-only access

The API SHALL provide read-only access to memories and MUST NOT create, modify, or delete memories.

#### Scenario: Mutation rejected

- **WHEN** a client attempts to write a memory through the API
- **THEN** the API rejects the request

### Requirement: Authenticated access

The API SHALL require the same shared API-key trust boundary as the existing token endpoint and MUST NOT expose memories to unauthenticated callers.

#### Scenario: Unauthenticated request

- **WHEN** a client calls the API without a valid API key
- **THEN** the API returns an unauthorized response and no memory content
