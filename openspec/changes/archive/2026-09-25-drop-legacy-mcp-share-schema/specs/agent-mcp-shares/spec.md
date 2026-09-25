## MODIFIED Requirements

### Requirement: core.agent_mcp_shares is superseded and backfilled

The single-credential `core.agent_mcp_shares` binding SHALL be superseded by the agent-owned endpoint and its many labeled keys. Existing active share rows MUST have been backfilled into one endpoint per distinct `(project_id, agent_id)` and one key per share row, copying the bound `token_id`, `created_by`, and `revoked_at`, with the key `label` taken from the share `name`, without modifying the bound tokens or their scopes so live keys keep working through the cutover. The legacy `core.agent_mcp_shares` table SHALL have been dropped once no reader remains, together with the unused `core.mcp_share_instances.allowed_agents` column. The tables backing the agent-owned endpoint (`core.agent_mcp_endpoints`, `core.agent_mcp_keys`, `core.agent_mcp_sessions`) and the project share instances (`core.mcp_share_instances`), including legacy rows, MUST remain.

#### Scenario: Active shares are backfilled

- **WHEN** the backfill migration runs over existing active shares
- **THEN** each distinct agent has one endpoint and each share becomes one labeled key bound to the same token

#### Scenario: Live keys keep working through cutover

- **WHEN** a client connects with a token that was minted for a backfilled share
- **THEN** the token authenticates the agent endpoint with no re-issuance

#### Scenario: The old table is dropped only after readers are gone

- **WHEN** the drop migration runs after the backfill has shipped and no reader remains
- **THEN** `core.agent_mcp_shares` and `core.mcp_share_instances.allowed_agents` no longer exist, and no code path reads or writes either
