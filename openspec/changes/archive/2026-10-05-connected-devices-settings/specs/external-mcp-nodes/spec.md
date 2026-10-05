## MODIFIED Requirements

### Requirement: List connected external MCP nodes

The web UI SHALL list the external MCP nodes currently connected to the project via the backend MCP relay in the Connected devices page at `/settings/devices`, under an MCP nodes section, showing each node's instance id, version, tool count, and connection time, most recently connected first. The legacy `/settings/mcp-nodes` route SHALL redirect (303) to `/settings/devices`, preserving the `?node=` query parameter, and the node selection and tools panel SHALL render in that same MCP nodes section.

#### Scenario: Nodes connected

- **WHEN** the Connected devices page loads and the backend reports connected relay sessions
- **THEN** each session is listed under the MCP nodes section with its instance id, version, tool count, and connection time

#### Scenario: No nodes connected

- **WHEN** the Connected devices page loads and no relay sessions exist
- **THEN** a clear "no external nodes connected" empty state is shown in the MCP nodes section

#### Scenario: Relay API unreachable

- **WHEN** the backend relay API request fails
- **THEN** the MCP nodes section shows a clear error state and does not render a broken page

#### Scenario: Legacy route redirects

- **WHEN** the user opens `/settings/mcp-nodes`
- **THEN** the browser is redirected (303) to `/settings/devices`, and a `?node=<instance id>` query parameter is preserved so the node is selected on arrival
