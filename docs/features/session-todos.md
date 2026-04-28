# Session Todos

Persistent task lists scoped to an agent session (`kb.acp_sessions`). Todos survive across multiple agent runs within the same session and are visible to any tool or API consumer.

## Data Model

| Field             | Type                  | Notes                                      |
|-------------------|-----------------------|--------------------------------------------|
| `id`              | UUID                  | Primary key                                |
| `session_id`      | UUID (FK)             | `kb.acp_sessions(id) ON DELETE CASCADE`    |
| `content`         | text                  | Task description                           |
| `status`          | `session_todo_status` | Enum — see below                           |
| `author`          | text (nullable)       | Free-form author label (e.g. agent name)   |
| `order`           | int                   | Sort order within session (default 0)      |
| `context_snapshot`| text (nullable)       | Optional context blob stored at creation   |
| `created_at`      | timestamptz           |                                            |
| `updated_at`      | timestamptz           | Updated on every PATCH                     |

### Status Enum (`kb.session_todo_status`)

```
draft → pending → in_progress → completed
                             ↘ cancelled
```

## REST API

Base path: `/api/v1/agent/sessions/:sessionId/todos`

| Method   | Path          | Description                      |
|----------|---------------|----------------------------------|
| `GET`    | `/`           | List todos (optional `?status=`) |
| `POST`   | `/`           | Create todo                      |
| `PATCH`  | `/:todoId`    | Update status / content / order  |
| `DELETE` | `/:todoId`    | Delete todo                      |

Auth: session-scoped bearer token (same as other `/api/v1/agent/` routes).

## MCP Built-in Tools

Agents receive these tools automatically — no extra configuration required.

### `session-todo-list`
Returns todos for a session.

| Arg         | Type   | Required | Description                              |
|-------------|--------|----------|------------------------------------------|
| `session_id`| string | yes      | The session to list todos for            |
| `statuses`  | string | no       | Comma-separated filter, e.g. `pending,in_progress` |

### `session-todo-update`
Updates a single todo.

| Arg         | Type   | Required | Description                             |
|-------------|--------|----------|-----------------------------------------|
| `session_id`| string | yes      | Session the todo belongs to             |
| `todo_id`   | string | yes      | Todo ID to update                       |
| `status`    | string | no       | New status value                        |
| `content`   | string | no       | New content text                        |
| `order`     | int    | no       | New sort order                          |

## Go SDK

Methods live on `client.Agents` (the `agents.Client`):

```go
import "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"

// List pending + in-progress todos
todos, err := client.Agents.ListSessionTodos(ctx, sessionID, []agents.TodoStatus{
    agents.TodoStatusPending,
    agents.TodoStatusInProgress,
})

// Create a new todo
todo, err := client.Agents.CreateSessionTodo(ctx, sessionID, agents.CreateTodoRequest{
    Content: "Implement the payment handler",
})

// Mark in-progress
st := agents.TodoStatusInProgress
todo, err = client.Agents.UpdateSessionTodo(ctx, sessionID, todo.ID, agents.UpdateTodoRequest{
    Status: &st,
})

// Delete when no longer needed
err = client.Agents.DeleteSessionTodo(ctx, sessionID, todo.ID)
```

## Migration

`apps/server/migrations/00101_create_session_todos.sql`
