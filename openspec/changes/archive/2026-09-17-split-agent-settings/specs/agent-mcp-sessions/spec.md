## ADDED Requirements

### Requirement: Sessions surface lives on the MCP sharing subpage

The external client sessions list and its status filter SHALL render on the agent's MCP sharing subpage at `/agents/:id/settings/mcp`, reachable via the agent Settings group nav.

#### Scenario: Sessions listed on the MCP sharing subpage

- **WHEN** a project admin opens the agent's MCP sharing subpage
- **THEN** the endpoint's external sessions and their status filter render there
