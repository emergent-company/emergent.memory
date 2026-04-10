## Why

External systems and third-party agents need a standard protocol to discover and invoke Memory agents without embedding Memory-specific project IDs, custom auth flows, or internal API conventions. ACP (Agent Communication Protocol) v0.2.0 provides a vendor-neutral interop layer — enabling any ACP-compatible client to discover externally-visible agents, trigger runs with sync/async/streaming modes, manage human-in-the-loop interactions, and group runs into sessions.

## What Changes

- New HTTP route group `/acp/v1/` with 10 endpoints: ping, agent discovery, run lifecycle (create/get/cancel/resume/events), and sessions
- New `kb.acp_sessions` table for thin session grouping of runs
- New `kb.acp_run_events` table for persisted SSE event history
- New `acp_session_id` column on `kb.agent_runs`
- `cancelling` intermediate run status added (ACP two-step cancel: `cancelling` → `cancelled`)
- New `memory acp` CLI command group: `ping`, `agents list`, `agents get`, `runs create`, `runs get`, `runs cancel`, `runs resume`, `sessions create`, `sessions get`
- New MCP tools: `acp-list-agents`, `acp-trigger-run`, `acp-get-run-status` for agent-to-agent ACP invocation
- Agent manifest enriched: `tags`, `domains`, `recommended_models`, `documentation`, `framework`, `links`, `dependencies`, live `status` stats (`avg_run_tokens`, `avg_run_time_seconds`, `success_rate`)
- Only `AgentDefinition` records with `visibility = 'external'` are exposed via ACP endpoints

## Capabilities

### New Capabilities

- `acp-agent-discovery`: Ping + agent manifest endpoints (`GET /acp/v1/ping`, `GET /acp/v1/agents`, `GET /acp/v1/agents/:name`) returning enriched manifests with live stats for externally-visible agents
- `acp-run-lifecycle`: Full run management — create (sync/async/stream modes), get, cancel, resume (human-in-the-loop), event log as JSON (`GET /acp/v1/agents/:name/runs/:runId/events`)
- `acp-sessions`: Thin session grouping (`POST /acp/v1/sessions`, `GET /acp/v1/sessions/:sessionId`) linking runs by `acp_session_id`, history as run URI list, no cross-run context injection
- `acp-cli`: New `memory acp` command group covering all 9 ACP operations via the Go SDK
- `acp-mcp-tools`: New MCP tools registered in the agents domain for agent-to-agent ACP invocation: `acp-list-agents`, `acp-trigger-run`, `acp-get-run-status`

### Modified Capabilities

(none)

## Impact

- **New files**: `domain/agents/acp_dto.go`, `domain/agents/acp_handler.go`, `domain/agents/acp_routes.go`, `migrations/00082_acp_sessions.sql`, `pkg/sdk/acp/client.go`, `tools/cli/internal/cmd/acp.go`
- **Modified files**: `domain/agents/entity.go` (ACPConfig expansion, `cancelling` status), `domain/agents/repository.go` (session/event/stats methods), `domain/agents/module.go` (wire ACPHandler + ACP MCP tools), `domain/agents/mcp_tools.go` (3 new ACP tools)
- **Auth**: Reuses existing `Bearer emt_*` API token auth — no new auth mechanism
- **No breaking changes**: All existing `/api/projects/:projectId/agents/` routes, CLI commands, and MCP tools are unaffected
- **DB**: Two new tables (`kb.acp_sessions`, `kb.acp_run_events`), one new column (`kb.agent_runs.acp_session_id`) — additive only
