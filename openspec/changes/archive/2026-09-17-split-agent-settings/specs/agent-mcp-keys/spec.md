## MODIFIED Requirements

### Requirement: An agent owns exactly one endpoint

The system SHALL persist a per-agent MCP endpoint that belongs to exactly one project and one agent. At most one active endpoint SHALL exist per agent at a time, and a revoked endpoint SHALL not count against that limit. The endpoint SHALL be created and revoked from the agent's own configuration surface — the "MCP sharing" subpage of the agent Settings group at `/agents/:id/settings/mcp` — not only from a separate MCP-sharing page. Deleting the agent MUST delete its endpoint and everything the endpoint owns.

#### Scenario: One active endpoint per agent

- **WHEN** an endpoint already exists for an agent and a second active endpoint is created for the same agent
- **THEN** the system rejects the second create and the first endpoint remains active

#### Scenario: A revoked endpoint is replaced

- **WHEN** an agent's endpoint has been revoked and a new endpoint is created for that agent
- **THEN** the new endpoint is created and becomes the agent's active endpoint

#### Scenario: Deleting the agent removes the endpoint

- **WHEN** the agent row is deleted (agents are hard-deleted)
- **THEN** its endpoint and its keys and sessions are deleted as well, and no orphaned binding remains

#### Scenario: Endpoint is managed from the agent

- **WHEN** a project admin views an agent's MCP sharing settings subpage
- **THEN** the agent's MCP endpoint and its keys are created, listed, revoked, and rotated from that subpage
