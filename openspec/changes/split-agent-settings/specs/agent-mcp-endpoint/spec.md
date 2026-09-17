## ADDED Requirements

### Requirement: MCP sharing surface is a dedicated settings subpage

The agent's MCP endpoint create/revoke UI SHALL live on its own subpage of the agent Settings group — the "MCP sharing" subpage at `/agents/:id/settings/mcp` — and its create/revoke flows SHALL PRG-redirect back to that subpage with a `#mcp` anchor so the section is visible on return.

#### Scenario: Endpoint managed from its subpage

- **WHEN** a project admin opens the agent's MCP sharing subpage
- **THEN** the endpoint renders there, and creating or revoking the endpoint redirects back to `/agents/:id/settings/mcp#mcp`
