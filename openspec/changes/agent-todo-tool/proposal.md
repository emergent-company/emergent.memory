## Why

Agents running multi-step tasks have no way to communicate their plan or progress to the user. Adding a `todo_update` / `todo_read` tool pair gives agents a structured task list scoped to each run — letting the user see what the agent intends to do, what it's working on now, and what it has completed, in real time.

## What Changes

- New opt-in ADK tool: `todo_update` — replaces the full todo list for the current run (full-replace semantics, mirrors OpenCode's `todowrite`)
- New opt-in ADK tool: `todo_read` — returns the current todo list for the current run
- New DB table: `kb.agent_run_todos` (run_id, position, content, status, priority)
- New repository methods: `UpsertTodos`, `GetTodos` on agents `Repository`
- New tool constants in `toolpool.go`: `ToolNameTodoUpdate`, `ToolNameTodoRead`
- New `buildTodoTools()` method on `AgentExecutor` — mirrors `buildAskUserTool()` pattern
- System prompt augmentation when todo tools are active — mirrors `augmentInstructionWithWorkspace()`
- New API endpoint: `GET /agents/runs/:runID/todos` — returns current todo list for a run
- Tool description embeds the full agent guide (when/how to use, status/priority values, full-replace rule)

## Capabilities

### New Capabilities

- `agent-todo-tool`: Opt-in `todo_update` and `todo_read` ADK tools for agent runs; DB-persisted per run; exposed via read API endpoint; system prompt guidance injected when active.

### Modified Capabilities

- `agent-tool-pool`: Add `ToolNameTodoUpdate` and `ToolNameTodoRead` coordination tool name constants; update depth restriction logic to exclude todo tools from the coordination-tool gate.

## Impact

- **Go backend**: `apps/server/domain/agents/` — new file `todo_tool.go`, changes to `entity.go`, `repository.go`, `toolpool.go`, `executor.go`, `handler.go`, `routes.go`
- **DB migration**: new `kb.agent_run_todos` table with FK cascade from `kb.agent_runs`
- **API**: new read-only endpoint; no breaking changes to existing endpoints
- **No frontend changes required** — endpoint is available for the UI to consume
