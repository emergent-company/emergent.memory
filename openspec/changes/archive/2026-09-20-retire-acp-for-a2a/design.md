## Context

The A2A v1.0 interop change (archived 2026-09-20) left the ACP implementation live behind deprecation headers, with the final deletion explicitly deferred to "a later release" (its `design.md` Decision 6, phase D3). This change executes D3.

## Decisions

### 1. Delete the ACP wire surface; keep the shared persistence and types

The `kb.acp_sessions` / `kb.acp_run_events` tables and the `ACPSession` / `ACPRunEvent` / `ACPSessionID` Go types are no longer ACP-specific: A2A maps `contextId` → `kb.acp_sessions.id` and task history → `kb.acp_run_events`, and the chat domain uses the same `acp_session_id` column for conversation sessions. Deleting or renaming them would be a cross-domain migration with no functional gain and real risk. This change removes only the ACP protocol surface and the dead code it uniquely owns.

### 2. `pkg/acpslug` stays (misnamed, but now A2A-shared)

The RFC 1123 slug helper is used by A2A discovery (`a2a_discovery.go`) and the shared agent-resolution helpers (`FindExternalAgentBySlug` / `FindAgentDefinitionBySlug`). It is retained as-is; renaming it to a neutral name belongs to the deferred naming cleanup, not this removal. The only edit is re-pointing the repository helpers at `acpslug.FromName` directly instead of the deleted `ACPSlugFromName` wrapper.

### 3. `AgentStatusMetrics` + `GetAgentStatusMetrics` are dead once ACP goes

Their only callers are the ACP `ListAgents`/`GetAgent` handlers and `acp-list-agents`. The type lives in the deleted `acp_dto.go`; the repository method is removed with it rather than left orphaned.

### 4. MCP tool removal spans three packages

The `acp-*` tools are defined in `domain/agents/mcp_tools.go`, dispatched through the `AgentToolHandler` interface in `domain/mcp`, and catalogued in `domain/agentcompat/tool_namer.go`. Removing them touches all three, plus the test stubs that satisfy the interface (`share_instance_test.go`, `relay_tools_test.go`), plus the `suspend_resume_test.go` cases that exercise the deleted `RunToACPObject`.

### 5. Table/type/naming cleanup is deferred

A set of identifiers survive this change with their `ACP`/`acp` prefix intact because they are now shared infrastructure, not ACP protocol surface. Deleting them is out of scope; renaming them is deferred to a separate follow-up change:

- **Persistence + models**: `kb.acp_sessions`, `kb.acp_run_events`, and the `ACPSession` / `ACPRunEvent` / `ACPSessionID` / `ACPConfig` types — reused by A2A (contextId / history) and chat (conversation sessions). A rename is a cross-domain migration across `agents`, `chat`, `mcp`, the web-ui gateway, plus a new migration, with zero behavioural gain.
- **`pkg/acpslug`** — the RFC 1123 slug helper, now used by A2A discovery and the shared agent-resolution helpers.
- **`acpProjectID`** and the `ACPEvent*` run-event constants — relocated out of the deleted ACP files into `a2a_discovery.go` / `entity.go` because A2A message/stream/discovery depend on them. Kept under their original names to minimize the diff.
- **`acpTools`, `applyACPRestrictions`, and the `ToolNameACP*` constants** in `toolpool.go` — these gate the live `trigger_agent`, `agent-run-get`, `mcp-server-list`, `mcp-server-get`, `search_mcp_registry` tools behind explicit opt-in. They are misnamed (not ACP tools anymore) but live; renaming them is the naming-cleanup follow-up, not a deletion.

## Risks

- **No rollback via deprecation headers.** Once deleted, ACP clients receive 404. The deprecation window is presumed closed; the sunset date (Sep 2027) is intentionally brought forward by this change.
- **Test-stub breakage.** Removing the four interface methods breaks `stubAgentHandler`/`stubAgentToolHandler` test implementations; those must be updated in lockstep with the interface.
