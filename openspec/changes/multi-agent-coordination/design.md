## Context

Emergent is a knowledge graph platform with an ADK-Go agent runtime, 30+ built-in MCP graph tools, a product system for declarative configuration, and a PostgreSQL job queue. Agent execution is currently stubbed — `domain/agents/handler.go:421-422` marks runs as `skipped`. The ADK-Go pipeline pattern is proven in `domain/extraction/agents/pipeline.go` (sequential, loop, and parallel agent patterns with Vertex AI + Gemini).

The product layer design establishes that products define agents in `manifest.json`. The existing `Agent` entity in `domain/agents/entity.go` has name, strategy, prompt, capabilities, and config fields. `AgentRun` tracks status, duration, summary, and errors. The `domain/mcp/service.go` exposes 30+ graph tools. None of these are wired together — no component builds an ADK pipeline from an agent definition, resolves its tools, runs it, and tracks results.

This design covers the full multi-agent system: execution, tool management, coordination, state persistence, safety, and task orchestration. It spans 6 implementation phases across ~14 weeks.

### Stakeholders

- **Product developers**: Define agents in product manifests
- **Admin users**: Monitor agent runs, trigger agents, inspect history
- **External integrators**: Invoke agents via ACP (future)
- **The agents themselves**: Discover and delegate to other agents at runtime

## Goals / Non-Goals

**Goals:**

- Replace stubbed agent execution with a working AgentExecutor that builds ADK-Go pipelines from agent definitions
- Provide per-project tool management (ToolPool) with per-agent filtering (ResolveTools) enforced in Go
- Enable dynamic multi-agent coordination via `list_available_agents` and `spawn_agents` tools
- Persist full agent conversation history and tool calls for debugging, audit, and sub-agent resumption
- Implement safety mechanisms preventing runaway agents: step limits, timeouts, doom loop detection, recursive spawning prevention
- Support product-defined agents with three visibility levels (external/project/internal)
- Build a TaskDispatcher that walks task DAGs, selects agents, and manages execution lifecycle
- Deliver an Agent Run History API with progressive disclosure for admin UI

**Non-Goals:**

- Cross-project agent coordination (agents scoped to single project)
- Persistent worker processes (ephemeral goroutines only)
- Custom agent SDKs or external agent frameworks (LangChain, CrewAI integration)
- Real-time agent-to-agent streaming communication (results returned on completion)
- ACP protocol implementation (agent card metadata is stored but ACP serving is future work)
- Admin UI implementation (API endpoints only; UI is a separate change)

## Decisions

### D1: Execution model — ADK-Go pipelines in goroutines

**Decision**: Each agent execution spawns a goroutine that builds an ADK-Go pipeline from the agent definition, runs it against Vertex AI (Gemini), and writes results to PostgreSQL.

**Alternatives considered**:

- _External process spawning_: Run agents as separate processes. Rejected — adds deployment complexity, IPC overhead, and doesn't leverage existing ADK-Go patterns.
- _Persistent worker pool_: Long-lived goroutines pulling from a queue. Rejected — adds state management complexity. The extraction pipeline already proves ephemeral goroutines work well.

**Rationale**: The extraction pipeline (`domain/extraction/agents/pipeline.go`) already demonstrates this pattern with sequential, loop, and parallel agents. Reusing it avoids new infrastructure and keeps agent lifecycle simple (start → run → complete/fail).

### D2: Two-level tool management — ToolPool + ResolveTools

**Decision**: Tool access operates at two levels. Per-project `ToolPool` combines built-in graph tools + external MCP server tools into a unified set. Per-agent `ResolveTools()` filters the pool by the agent's `tools` whitelist.

**Alternatives considered**:

- _Per-agent tool registration_: Each agent definition declares full tool implementations. Rejected — duplicates tool code, prevents tool sharing.
- _Global tool pool (no per-agent filtering)_: All agents see all tools. Rejected — breaks security boundaries (read-only agents would have write access).

**Rationale**: This creates clear security boundaries enforced in Go (not by the LLM). A read-only auditor agent gets `search_*` tools only. A write-focused extractor gets `create_*` tools. The LLM never sees tools outside its whitelist. Glob patterns (`"entities_*"`) reduce manifest verbosity.

### D3: Coordination approach — hybrid (code loop + LLM judgment)

**Decision**: The TaskDispatcher uses a Go polling loop for mechanics (DAG walking, slot management, dispatch, monitoring) and LLM calls only for judgment (agent selection when rules are ambiguous, output evaluation, failure triage).

**Alternatives considered**:

- _Pure code coordinator_: Deterministic rules only. Rejected — can't handle novel task-agent matching or evaluate output quality.
- _Pure LLM coordinator_: LLM orchestrates everything. Rejected — expensive ($0.01+ per decision), slow (1-3s per LLM call), and unreliable for mechanical operations like DAG traversal.

**Rationale**: Code handles $0-cost mechanical work at millisecond speed. LLM is called only when human-like judgment is needed. The `HybridSelector` tries code rules first, falls back to LLM for ambiguous matches.

### D4: State persistence — full message history in PostgreSQL JSONB

**Decision**: Every agent run persists its complete LLM conversation in `kb.agent_run_messages` (role, content as JSONB, step number) and every tool invocation in `kb.agent_run_tool_calls` (tool name, input/output as JSONB, duration, status). Messages are persisted during execution (not after) for crash recovery.

**Alternatives considered**:

- _Summary-only persistence_: Store only final summary. Rejected — can't resume sub-agents, can't debug failures, can't audit tool usage.
- _External store (Redis, S3)_: Offload conversation blobs. Rejected — adds infrastructure dependency. 50-500KB per run in JSONB is negligible for PostgreSQL.

**Rationale**: Full persistence enables three critical capabilities: (1) sub-agent resumption via `resume_run_id` with exact conversation reconstruction, (2) progressive-disclosure debugging in admin UI (run → messages → tool calls), (3) audit trail for every LLM and tool interaction. The storage cost (~50-500KB per run) is trivial compared to the debugging value.

### D5: Sub-agent safety — layered enforcement

**Decision**: Four safety layers:

1. **Step limits**: `max_steps` on agent definition (default 50 for sub-agents, nil=unlimited for top-level). Soft stop via "summarize and stop" prompt injection, hard stop if LLM ignores.
2. **Timeouts**: `default_timeout` on agent definition + per-spawn override. Go `context.WithDeadline` with 30s grace period for soft stop before hard cancel.
3. **Doom loop detection**: `DoomLoopDetector` tracks consecutive identical tool calls. Threshold=3 triggers error injection; threshold=5 triggers hard stop.
4. **Recursive spawning prevention**: `SubAgentDeniedTools` removes `spawn_agents` and `list_available_agents` from sub-agents at depth > 0 by default. Opt-in via explicit tools whitelist + `max_depth=2` hard cap.

**Alternatives considered**:

- _Token budget instead of step limit_: Count tokens, not steps. Rejected — requires token counting library, and steps are a better proxy for "work done" than raw token count.
- _No recursive spawning at all_: Rejected — legitimate use cases exist (research coordinator → sub-coordinator → specialized extractors). Hard cap at depth 2 prevents abuse.

**Rationale**: Layered defense-in-depth. Each layer catches different failure modes: step limits catch slow-but-steady resource drain, timeouts catch individual slow operations, doom loop catches stuck agents, recursive prevention catches exponential spawning.

### D6: Agent visibility — three levels with project as default

**Decision**: `external` (ACP-exposed + admin + agents), `project` (admin + agents, DEFAULT), `internal` (agents only). `list_available_agents` and `spawn_agents` see ALL agents regardless of visibility. Visibility only restricts external access surfaces.

**Alternatives considered**:

- _Two levels (public/private)_: Rejected — no way to distinguish "admin-visible utility agent" from "pure sub-agent implementation detail."
- _Visibility restricts spawn_agents_: Rejected — the LLM needs the full agent catalog for good delegation decisions. Artificial restrictions cause worse agent selection.

**Rationale**: Visibility is about human-facing access surfaces, not agent-to-agent boundaries. Internal agents are implementation details that shouldn't clutter the admin UI but must be spawnable. The LLM makes better delegation decisions when it can see all options.

### D7: Agent Run History API — progressive disclosure with cursor-based pagination

**Decision**: 6 endpoints with three levels of detail:

1. List runs (overview: status, duration, step count)
2. Get run details (summary, agent info, parent/child relationships)
3. List messages for a run (role, step number, truncated content)
4. Get single message (full JSONB content)
5. List tool calls for a run (tool name, status, duration)
6. Get single tool call (full input/output JSONB)

All list endpoints use cursor-based pagination (not offset-based).

**Alternatives considered**:

- _Single endpoint returning everything_: Rejected — a run with 50 messages and 100 tool calls would return megabytes per request. Progressive disclosure loads data on demand.
- _Offset-based pagination_: Rejected — unstable under concurrent inserts (new runs shift offsets). Cursor-based (keyset) pagination is stable and faster for large datasets.

**Rationale**: Admin users typically start with "which runs happened?" (list), then drill into a specific run, then look at specific messages or tool calls. Progressive disclosure matches this workflow and keeps responses small.

### D8: External MCP connections — lazy initialization with stdio + SSE

**Decision**: Projects configure external MCP servers in product manifests, stored in `kb.project_mcp_servers`. Connections are initialized lazily on first tool call (not at server startup). Support both `stdio` (spawn child process) and `sse` (HTTP SSE) transports.

**Alternatives considered**:

- _Eager initialization_: Connect to all MCP servers at startup. Rejected — wastes resources for servers that may never be used, and startup failures block unrelated functionality.
- _SSE only_: Rejected — many MCP servers (like Anthropic's reference implementations) use stdio transport.

**Rationale**: Lazy initialization avoids startup coupling. Environment variable interpolation (`${SEARCH_API_KEY}`) lets product manifests reference project-level secrets without embedding credentials. Connection pooling per project reuses connections across agent executions.

### D9: Coordination state — knowledge graph for task DAGs

**Decision**: Task DAGs (SpecTask objects with `blocks` relationships), sessions, and discussions are stored in the knowledge graph. The `emergent-coordination` template pack defines the schema (SpecTask, Session, SessionMessage, Discussion, DiscussionEntry object types).

**Alternatives considered**:

- _Separate coordination database_: Rejected — adds infrastructure complexity. The knowledge graph already supports typed objects, relationships, and search.
- _In-memory DAG only_: Rejected — no persistence across server restarts, no queryability.

**Rationale**: The knowledge graph provides typed objects, relationships, BFS traversal, and search — exactly what task DAG coordination needs. Storing coordination state as graph objects makes it queryable, versionable, and visible through existing graph APIs.

### D10: Global safety cap — MaxTotalStepsPerRun = 500

**Decision**: A hard cap of 500 total steps across all resumes of a single agent run. This prevents infinite resume loops where a parent keeps resuming a sub-agent that never converges.

**Rationale**: With `max_steps=50` per resume and up to 10 resumes, 500 total steps is generous for legitimate work. At ~1 LLM call per step with Gemini Flash, 500 steps costs ~$0.50 — bounded and predictable.

## Risks / Trade-offs

| Risk                                                           | Likelihood | Impact                                                | Mitigation                                                                                                                                         |
| -------------------------------------------------------------- | ---------- | ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| Agent goroutine leak (goroutine starts but never completes)    | Medium     | High — memory leak, resource exhaustion               | Context cancellation via `context.WithDeadline` + `activeRuns` map tracking + health monitor goroutine that kills stale runs                       |
| Concurrent graph writes from parallel agents                   | Medium     | Medium — data inconsistency                           | PostgreSQL serializable transactions for task state transitions. Graph object versioning for optimistic concurrency.                               |
| LLM cost explosion from coordinator LLM calls                  | Low        | High — unexpected billing                             | Code-first hybrid selector (LLM only for ambiguous cases). Fast models (Gemini Flash) for selection decisions. Step limits cap per-agent cost.     |
| Task DAG cycles causing infinite dispatch                      | Low        | High — infinite loop                                  | Validate DAG on creation with topological sort. Reject cycles. Runtime cycle detection in TaskDispatcher.                                          |
| External MCP server unavailability                             | Medium     | Low — degraded functionality                          | Lazy connection with retry. Tool calls to unavailable servers return error (agent can work around). Circuit breaker pattern for repeated failures. |
| Stale agent definitions after product update                   | Low        | Low — incorrect behavior                              | Product version tracking. Re-sync definitions on product install/update. Invalidate ToolPool cache on MCP server config change.                    |
| State persistence overhead (writing messages during execution) | Low        | Low — slight latency                                  | Batch JSONB inserts. Messages are small (1-10KB). Async writes with WAL buffering. Benchmark shows PostgreSQL JSONB insert < 1ms.                  |
| Sub-agent resumption conversation drift                        | Medium     | Medium — LLM loses context from reconstructed history | Preserve exact message format (JSONB, not summarized). Include step count context in continuation prompt. Test with long conversations.            |

## Migration Plan

No data migration needed — all tables are new. The only modification to existing data is adding columns to `kb.agent_runs` (nullable, with defaults). Existing agent runs are unaffected.

**Deployment sequence** (phases can be deployed independently):

1. Phase 1: Database migration (new columns on `agent_runs`) → deploy AgentExecutor → existing trigger endpoint works
2. Phase 2: Database migration (`agent_definitions` table) → deploy ToolPool + coordination tools → product-defined agents work
3. Phase 3: Database migration (`agent_run_messages`, `agent_run_tool_calls`) → deploy persistence + History API
4. Phase 4: Database migration (`project_mcp_servers`) → deploy MCP client + SpecTask template pack
5. Phase 5: Deploy TaskDispatcher module
6. Phase 6: Deploy collaborative intelligence features

**Rollback**: Each phase's migration has a down migration. Code changes are behind the agent execution path — no risk to existing graph/search/extraction functionality.

## Open Questions

1. **Agent pool strategy**: Ephemeral goroutines (clean, simple) vs persistent worker pool (reuse connections, warm caches). Recommendation: start with ephemeral, optimize to pooled if benchmarks show connection overhead is significant.
2. **Admin UI scope**: How much agent visibility should the admin UI expose in the first iteration? Recommendation: run list + run detail + message viewer. Tool call inspection and DAG visualization in a later iteration.
3. **Multi-project agents**: Can an agent in project A spawn a sub-agent that reads project B's graph? Recommendation: no — single project scoping for security. Cross-project coordination is a future capability.
4. **Chat module integration**: Should the chat module always route through an agent, or should it have a fallback direct-LLM path for when no agents are defined? Recommendation: fallback to direct LLM — maintains backward compatibility.
