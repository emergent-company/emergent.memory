## 1. Database Migration

- [ ] 1.1 Create migration file `apps/server/migrations/00050_create_agent_run_todos.sql` with `kb.agent_run_todos` table (`run_id`, `position`, `content`, `status`, `priority`) and ON DELETE CASCADE FK to `kb.agent_runs`
- [ ] 1.2 Add `-- +goose Up` and `-- +goose Down` sections; Down drops the table

## 2. Entity & Repository

- [ ] 2.1 Add `AgentRunTodo` struct to `apps/server/domain/agents/entity.go` with Bun model tags (`kb.agent_run_todos`, composite PK `run_id + position`)
- [ ] 2.2 Add `UpsertTodos(ctx, runID string, todos []AgentRunTodo) error` to `Repository` interface and `repository.go` — single transaction: delete all for `run_id`, bulk insert new list
- [ ] 2.3 Add `GetTodos(ctx, runID string) ([]AgentRunTodo, error)` to `Repository` interface and `repository.go` — select ordered by `position` ASC

## 3. Tool Pool Constants

- [ ] 3.1 Add `ToolNameTodoUpdate = "todo_update"` and `ToolNameTodoRead = "todo_read"` constants to `apps/server/domain/agents/toolpool.go` alongside existing `ToolNameAskUser`
- [ ] 3.2 Confirm `ToolNameTodoUpdate`/`ToolNameTodoRead` are NOT added to the `coordinationTools` map

## 4. Todo Tools Implementation

- [ ] 4.1 Create `apps/server/domain/agents/todo_tool.go` with `buildTodoTools()` method on `AgentExecutor`
- [ ] 4.2 `buildTodoTools()` returns `([]tool.Tool, error)` — returns nil (no tools) if neither `ToolNameTodoUpdate` nor `ToolNameTodoRead` appears in `req.AgentDefinition.Tools`
- [ ] 4.3 Implement `todo_update` ADK function tool: accepts `todos []struct{Content, Status, Priority string}`, validates status/priority enum values, calls `repo.UpsertTodos`, returns success count or error
- [ ] 4.4 Implement `todo_read` ADK function tool: no parameters, calls `repo.GetTodos`, returns ordered list or empty list
- [ ] 4.5 Write tool description for `todo_update` that explicitly states: full-replace semantics (send complete list every call), status values (`pending`, `in_progress`, `completed`, `cancelled`), priority values (`high`, `medium`, `low`)

## 5. Executor Integration

- [ ] 5.1 In `executor.go` `runPipeline()`, call `buildTodoTools()` after `buildAskUserTool()`; append returned tools to `resolvedTools`
- [ ] 5.2 Add `augmentInstructionWithTodos()` method — appends a concise `## Task Management` section instructing the agent to use `todo_update` proactively, keep one item `in_progress` at a time, and mark items `completed` immediately when done
- [ ] 5.3 In `runPipeline()`, call `augmentInstructionWithTodos()` only when `buildTodoTools()` returned non-empty tools (mirror `augmentInstructionWithWorkspace` pattern)

## 6. HTTP Handler & Route

- [ ] 6.1 Add `GetRunTodos` handler to `apps/server/domain/agents/handler.go` — reads `:runId` path param, calls `repo.GetTodos`, returns JSON array of `{content, status, priority, position}` ordered by position; returns 404 if run not found
- [ ] 6.2 Register route `GET /:runId/todos` on the `runs` group in `apps/server/domain/agents/routes.go` (alongside existing `/:runId/messages`, `/:runId/tool-calls`, `/:runId/questions`)
