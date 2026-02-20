## Context

The ADK (Agent Development Kit) requires a `session.Service` implementation to store and retrieve interaction states, messages, and events. Emergent currently utilizes ADK's `session.InMemoryService()`, which stores these records in ephemeral memory.
When an agent execution pauses (e.g., when invoking the `ask_user` tool) and is later resumed by the `AgentExecutor.Resume` flow, a new in-memory session is instantiated. As a result, the LLM context of the prior execution is lost. While the overarching goal and the newly provided answer are injected into the context, the agent restarts functionally "amnesic," potentially repeating work or behaving inconsistently.

Emergent uses `uptrace/bun` as its PostgreSQL ORM. Although ADK provides a native database session implementation, it relies on `gorm`, requiring a custom `bun`-backed implementation of the `session.Service` interface for full compatibility with Emergent's ecosystem.

## Goals / Non-Goals

**Goals:**

- Implement an ADK-compliant `session.Service` using `uptrace/bun` and PostgreSQL.
- Define relational database schemas for ADK Sessions, Events, and States inside the `kb` schema.
- Update `AgentExecutor` to inject the new database-backed session service instead of `InMemoryService`.
- Ensure `AgentExecutor.Resume` loads the previous agent conversation history accurately, so agents maintain full context.
- Support transactional boundaries when appending events to ensure data consistency.

**Non-Goals:**

- Removing or altering the existing `agent_run_messages` and `agent_run_tool_calls` tables (these act as audit logs/records, while the new tables drive ADK runtime state natively).
- Modifying the ADK module itself (the implementation will live within the Emergent codebase).
- Modifying the ephemeral workspace persistence (workspace state is managed separately from the conversation context).

## Decisions

**Decision 1: Custom Bun Implementation vs. Using existing logging tables**
_Rationale:_ We could theoretically "hydrate" an `InMemoryService` by reading from the existing `kb.agent_run_messages` table. However, building a native `bun` session service that fully implements the `session.Service`, `session.Session`, and `session.Event` interfaces is much cleaner and robust. It delegates state management directly to ADK as designed, rather than requiring brittle translation layers in the executor.
_Alternative:_ Hydrating the `InMemoryService` from existing tables was considered, but it diverges from the intended ADK architecture.

**Decision 2: Database Schema Design**
_Rationale:_ We will introduce three new tables to closely mirror ADK's conceptual model:

1. `kb.adk_sessions` - Maps to `session.Session` (tracks the session ID, app name, user ID, and timestamps).
2. `kb.adk_events` - Maps to `session.Event` (stores timestamps, branch logic, and the raw LLM responses/tool calls as JSONB).
3. `kb.adk_states` - Maps to `session.State` (stores key-value session state, scope, and values as JSONB).
   These tables isolate ADK-specific runtime state from Emergent's higher-level audit logging.

**Decision 3: Placement of the Implementation**
_Rationale:_ The new package will live under `pkg/adk/session/bun` (or similar) to clearly distinguish it from business logic, serving as a reusable infrastructure adapter.

## Risks / Trade-offs

- **Risk:** **Storage bloat** from storing full LLM responses/events twice (once in `agent_run_messages` for audit, and once in `adk_events` for ADK state).
  - **Mitigation:** Accept this trade-off for architectural cleanliness. The tables serve two different purposes (audit vs. runtime engine). If storage becomes a concern later, we can consolidate the models or archive older sessions.
- **Risk:** **Performance overhead** when appending events to the database sequentially within the LLM generation loop.
  - **Mitigation:** Ensure correct indexing on `session_id` and use `bun` context-aware transactions where necessary. ADK's native `gorm` implementation handles this effectively, and the `bun` equivalent will adopt similar patterns.
