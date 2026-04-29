package sessiontodos

import (
	"time"

	"github.com/uptrace/bun"
)

// TodoStatus represents the lifecycle status of a session todo.
type TodoStatus string

const (
	StatusDraft      TodoStatus = "draft"
	StatusPending    TodoStatus = "pending"
	StatusInProgress TodoStatus = "in_progress"
	StatusCompleted  TodoStatus = "completed"
	StatusCancelled  TodoStatus = "cancelled"
)

// SessionTodo is a persistent, session-scoped task item.
// Table: kb.session_todos
type SessionTodo struct {
	bun.BaseModel `bun:"table:kb.session_todos,alias:st"`

	ID              string     `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	SessionID       string     `bun:"session_id,type:uuid,notnull"              json:"sessionId"`
	Content         string     `bun:"content,notnull"                           json:"content"`
	Status          TodoStatus `bun:"status,notnull,default:draft"              json:"status"`
	Author          *string    `bun:"author"                                    json:"author,omitempty"`
	Order           int        `bun:"\"order\",notnull,default:0"               json:"order"`
	ContextSnapshot *string    `bun:"context_snapshot"                          json:"contextSnapshot,omitempty"`
	CreatedAt       time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt       time.Time  `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`
}

// CreateTodoRequest is the request body for creating a todo.
type CreateTodoRequest struct {
	Content         string  `json:"content"         validate:"required"`
	Author          *string `json:"author,omitempty"`
	Order           *int    `json:"order,omitempty"`
	ContextSnapshot *string `json:"contextSnapshot,omitempty"`
}

// UpdateTodoRequest is the request body for updating a todo.
type UpdateTodoRequest struct {
	Status  *TodoStatus `json:"status,omitempty"`
	Content *string     `json:"content,omitempty"`
	Order   *int        `json:"order,omitempty"`
}
