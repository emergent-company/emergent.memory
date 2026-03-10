## Context

Agents executing multi-step tasks in Memory have no way to track or expose their plan. OpenCode (the IDE this project is built within) solves this with a `todowrite`/`todoread` tool pair that agents use to maintain a live task list per session. We are adding the equivalent to Memory's agent runtime.

The existing `ask_user` tool (`ask_user_tool.go`) is the direct architectural precedent: it is a per-run ADK `functiontool` injected into the pipeline by `AgentExecutor.buildAskUserTool()` only when the agent definition opts in. The todo tool pair follows the same opt-in pattern. Workspace instruction augmentation (`augmentInstructionWithWorkspace`) is the precedent for injecting system-prompt guidance when a tool capability is active.

The `ToolPool` (builtin + external MCP tools, filtered per agent definition) is orthogonal — todo tools are not MCP-backed and do not live in the pool.

## Goals / Non-Goals

**Goals:**
- `todo_update` and `todo_read` ADK tools available to agents that opt in via `tools` list
- Per-run persistence in a new `kb.agent_run_todos` table
- Read API endpoint `GET /agents/runs/:runID/todos` for external consumers (UI)
- System prompt augmentation injected when tools are active — tells the agent when/how to use them
- Tool description embeds the full agent guide (when/how/status values/full-replace rule)

**Non-Goals:**
- Frontend UI changes (endpoint is available; consuming it is a separate UI change)
- Real-time SSE push of todo updates (the existing run SSE stream is out of scope)
- Sub-agent todo isolation (sub-agents share the same run scope — not needed now)
- Wildcard opt-in (i.e. `tools: ["*"]` implicitly enabling todo tools) — todo tools are always injected separately, not via ToolPool

## Decisions

### Decision: Opt-in via agent definition `tools` list (not always-on)

**Chosen**: Agent definitions must include `"todo_update"` (or `"todo_read"`) in their `tools` array to activate the tools. Either name triggers both tools being added (they are meaningless alone).

**Alternative considered**: Always-on for all agents (OpenCode behavior). Rejected because Memory agents are purpose-built with explicit tool whitelists. An always-on todo tool would appear as noise to agents that run single-step tasks and adds unnecessary instructions to simple agents.

**Rationale**: Consistent with `ask_user` opt-in pattern already established in the codebase.

---

### Decision: Full-replace semantics (delete + re-insert by position)

**Chosen**: Every `todo_update` call deletes all existing todos for the run and re-inserts the new list. Position is the array index.

**Alternative considered**: Patch/diff operations (update individual items by ID). Rejected because the LLM sends the full list on every call — this is how OpenCode's implementation works and it matches how LLMs naturally handle this: they always return the complete intended state.

**Rationale**: Simpler DB schema (composite PK of `(run_id, position)`), no need for item IDs, perfectly matches the tool's full-replace call contract.

---

### Decision: Not a coordination tool (not added to `coordinationTools` map)

**Chosen**: `ToolNameTodoUpdate` and `ToolNameTodoRead` are NOT added to the `coordinationTools` map in `toolpool.go`. They are not filtered by depth restrictions.

**Alternative considered**: Treating them as coordination tools and denying them to sub-agents by default (matching OpenCode's subagent behavior). Rejected because Memory sub-agents currently share the same run context, and task tracking is still useful in sub-agent flows.

**Rationale**: Keep it simple; if sub-agent todo isolation is needed later it can be added as a depth restriction.

---

### Decision: System prompt augmentation only when opted in

**Chosen**: `augmentInstructionWithTodos()` is called in `runPipeline()` only when `buildTodoTools()` returns tools. The augmentation appends a short `## Task Management` section.

**Rationale**: Avoids polluting the system prompt for agents that never use the tool.

---

### Decision: API endpoint scoped to run, not agent

**Chosen**: `GET /agents/runs/:runID/todos` — todos are per-run, not per-agent. Auth uses the existing project-scoped middleware.

**Rationale**: Runs are the observable unit; the same agent definition can produce many runs, each with a different todo list.

## Risks / Trade-offs

- **[Risk] Verbose system prompt for complex agents** → Mitigation: the augmentation section is kept concise (~6 lines); not repeated in the tool description beyond what's necessary.
- **[Risk] Agent never calls `todo_read` and wastes a tool slot** → Mitigation: `todo_read` is low cost; it is included so agents can resume/re-orient on long runs. Acceptable trade-off.
- **[Risk] Full-replace on every update can cause write amplification on large lists** → Mitigation: Todos are small (< 50 items in practice). A single transaction delete+insert is negligible at this scale.

## Migration Plan

1. Apply DB migration (`00050_create_agent_run_todos.sql`) — additive, no existing data affected
2. Deploy server — hot reload picks up new code
3. Enable for an agent by adding `"todo_update"` to its `tools` array in the agent definition

Rollback: remove `"todo_update"` from agent definitions; drop the migration (table has no foreign key dependents).

## Open Questions

- None — implementation is fully defined.
