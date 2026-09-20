## Why

The A2A v1.0 interop surface (`add-a2a-v1-interop`) shipped and was archived, superseding the in-house ACP interface. Since then the ACP routes have carried `Deprecation`/`Sunset` headers, the first-party consumers (`memory a2a`, `pkg/sdk/a2a`) are live, and the `acp-*` MCP tools have been marked `[Deprecated: ...]` in place. The deprecation window is now closed. This change performs the final deletion of the ACP protocol implementation: the HTTP handlers, DTOs, routes, SDK client, CLI command group, and the deprecated MCP tools.

The two `kb.acp_sessions` / `kb.acp_run_events` tables are **not** deleted. They now back A2A `contextId` and task history, plus chat-conversation sessions. Only the ACP wire surface and the dead code it uniquely owns go away.

## What Changes

- Delete `apps/server/domain/agents/acp_handler.go`, `acp_dto.go`, `acp_routes.go`, and their tests (`acp_handler_test.go`, `acp_dto_test.go`).
- Delete `apps/server/pkg/sdk/acp/` (the deprecated ACP SDK client package).
- Delete `apps/cli/internal/cmd/acp.go` (the `memory acp` command group).
- Remove ACP fx wiring from `apps/server/domain/agents/module.go` (`provideACPHandler` + `RegisterACPRoutes`).
- Remove the four deprecated `acp-*` MCP tools from `domain/agents/mcp_tools.go`, their `ExecuteACP*` handler methods, the `AgentToolHandler` interface methods in `domain/mcp/entity.go`, and the dispatch cases in `domain/mcp/service.go`.
- Remove the now-dead `AgentStatusMetrics` type and `Repository.GetAgentStatusMetrics` (their only callers were the ACP handlers and `acp-list-agents`).
- Re-point `Repository.FindExternalAgentBySlug` / `FindAgentDefinitionBySlug` at `pkg/acpslug` directly (they currently route through the deleted `ACPSlugFromName` wrapper).
- Remove the `acp-*` entries from `domain/agentcompat/tool_namer.go` and clean the associated test stubs.
- Regenerate swagger docs to drop the `domain_agents.*` ACP definitions.

## Capabilities

### Removed Capabilities

- `acp-agent-discovery`: the `/acp/v1/` + `/agent-chat/v1/` discovery endpoints (ping, list agents, get manifest).
- `acp-cli`: the `memory acp` command group and the `pkg/sdk/acp` Go client.
- `acp-mcp-tools`: the four `acp-*` MCP tools.
- `acp-run-lifecycle`: the ACP run create/sync/stream/resume/cancel/events lifecycle.
- `acp-sessions`: the ACP session create/get endpoints.

### Modified Capabilities

- `a2a-acp-migration`: the "ACP implementation removal" milestone has landed; the persistence tables are retained (rename deferred), so the requirement is updated to reflect completion rather than an open deprecation window.

## Impact

- **Deleted**: `apps/server/domain/agents/acp_{handler,dto,routes}.go` (+ their tests), `apps/server/pkg/sdk/acp/`, `apps/cli/internal/cmd/acp.go`.
- **Modified**: `apps/server/domain/agents/module.go`, `repository.go`, `mcp_tools.go`, `suspend_resume_test.go`, `entity.go`, `a2a_discovery.go`, `agent_run_once.go`; `apps/server/domain/mcp/entity.go`, `service.go`, `share_instance.go`, `share_instance_test.go`, `relay_tools_test.go`; `apps/server/domain/agentcompat/tool_namer.go`; `blueprints/code-memory-blueprint/tools/graph-fix/main.go`; `apps/server/docs/swagger/*` (regenerated).
- **Relocated shared symbols** (kept under their original names, see `design.md` Decision 5): the `ACPEvent*` run-event constants → `entity.go`; `acpProjectID` → `a2a_discovery.go`; `agent_run_once.go` inlines the single-part text extraction that previously came from the deleted `memoryContentToACPParts`.
- **No new migration.** `kb.acp_sessions` / `kb.acp_run_events` are retained as-is; renaming them is deferred to a separate change.
- **Breaking for legacy clients**: `/acp/v1/` and `/agent-chat/v1/` now return 404. A2A is the replacement surface. The deprecation sunset date (Sep 2027) is intentionally brought forward by this change.

## Scope

- **In scope**: deleting the ACP HTTP/CLI/SDK/MCP surface and its dead code; retiring the five ACP capability specs.
- **Out of scope (deferred)**: renaming `kb.acp_sessions` / `kb.acp_run_events`, the `ACPSession` / `ACPRunEvent` / `ACPSessionID` type and field names, and `pkg/acpslug` — all now shared A2A/chat/MCP infrastructure. That is a separate naming-cleanup change.
