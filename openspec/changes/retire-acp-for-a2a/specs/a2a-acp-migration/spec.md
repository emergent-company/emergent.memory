## ADDED Requirements

### Requirement: MCP is the sole Model-Context-Protocol surface
The MCP tool catalog and the per-agent MCP endpoint SHALL remain the real Model Context Protocol surface for the system. No ACP-named tool or artifact SHALL remain in the MCP catalog, and the per-agent endpoint (`/api/mcp/agents/:agentId`) SHALL keep functioning unchanged.

#### Scenario: No acp-named MCP tool remains
- **WHEN** the MCP tool catalog is listed after this change
- **THEN** no tool name begins with `acp-`

#### Scenario: Per-agent MCP endpoint unchanged
- **WHEN** a client calls the per-agent MCP endpoint after this change
- **THEN** the fixed tool catalog (`call_agent`, `start_session`, `continue_session`, `get_session`, `list_sessions`) is unchanged and still responds

## MODIFIED Requirements

### Requirement: MCP tool disposition
The `acp-*` MCP tools (`acp-list-agents`, `acp-trigger-run`, `acp-get-run-status`, `acp-get-run-events`) SHALL be retired as duplicates of the existing agent listing and triggering tools (`agent-list`, `trigger_agent`, `agent-run-status`).

#### Scenario: Duplicative ACP tools are retired or renamed
- **WHEN** the MCP tool catalog is listed after this change
- **THEN** no tool named `acp-*` remains

#### Scenario: No capability regression
- **WHEN** an agent previously used an `acp-*` MCP tool to list or trigger agents
- **THEN** an equivalent supported `agent-*` tool remains available

### Requirement: ACP implementation removal
The ACP server implementation SHALL be deleted: the `/acp/v1/` and `/agent-chat/v1/` routes, their handlers and DTOs, the `pkg/sdk/acp` client, and the `memory acp` CLI command group. ACP-named identifiers SHALL be migrated to A2A or protocol-neutral naming, with no A2A behavioural change.

#### Scenario: ACP handlers are removed
- **WHEN** this change is applied
- **THEN** `acp_handler.go`, `acp_dto.go`, `acp_routes.go`, `pkg/sdk/acp`, and `apps/cli/internal/cmd/acp.go` no longer exist

#### Scenario: ACP routes return no handler
- **WHEN** a client calls `/acp/v1/` or `/agent-chat/v1/` after this change
- **THEN** the routes are no longer registered and return 404

#### Scenario: A2A surface is unaffected by removal
- **WHEN** the ACP implementation is deleted
- **THEN** all A2A discovery, message-flow, and HITL behaviours continue to pass their tests

#### Scenario: Reused tables remain valid
- **WHEN** the `kb.acp_sessions` and `kb.acp_run_events` tables are renamed or retained
- **THEN** task `contextId` continuity and task history reconstruction continue to function

#### Scenario: ACP-named identifiers migrated
- **WHEN** the naming migration lands
- **THEN** `AgentDefinition` no longer exposes an `ACPConfig` field, and the agent-slug helper no longer carries the `acp` name
