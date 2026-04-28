package sessiontodos

import (
	"context"
	"log/slog"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Repository handles DB operations for session todos.
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new session todos repository.
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("sessiontodos.repo")),
	}
}

// List returns todos for a session, optionally filtered by status.
func (r *Repository) List(ctx context.Context, sessionID string, statuses []TodoStatus) ([]*SessionTodo, error) {
	var todos []*SessionTodo
	q := r.db.NewSelect().Model(&todos).
		Where("st.session_id = ?", sessionID).
		OrderExpr("st.order ASC, st.created_at ASC")
	if len(statuses) > 0 {
		q = q.Where("st.status IN (?)", bun.In(statuses))
	}
	if err := q.Scan(ctx); err != nil {
		return nil, apperror.NewInternal("failed to list session todos", err)
	}
	return todos, nil
}

// Get returns a single todo by ID.
func (r *Repository) Get(ctx context.Context, todoID string) (*SessionTodo, error) {
	todo := &SessionTodo{}
	err := r.db.NewSelect().Model(todo).Where("st.id = ?", todoID).Scan(ctx)
	if err != nil {
		return nil, apperror.NewNotFound("session_todo", todoID)
	}
	return todo, nil
}

// Create inserts a new todo and returns it.
func (r *Repository) Create(ctx context.Context, todo *SessionTodo) error {
	_, err := r.db.NewInsert().Model(todo).Returning("*").Exec(ctx)
	if err != nil {
		return apperror.NewInternal("failed to create session todo", err)
	}
	return nil
}

// Update applies partial updates to a todo.
func (r *Repository) Update(ctx context.Context, todo *SessionTodo, columns ...string) error {
	cols := append([]string{"updated_at"}, columns...)
	_, err := r.db.NewUpdate().Model(todo).Column(cols...).Where("id = ?", todo.ID).Exec(ctx)
	if err != nil {
		return apperror.NewInternal("failed to update session todo", err)
	}
	return nil
}

// Delete removes a todo by ID.
func (r *Repository) Delete(ctx context.Context, todoID string) error {
	_, err := r.db.NewDelete().Model((*SessionTodo)(nil)).Where("id = ?", todoID).Exec(ctx)
	if err != nil {
		return apperror.NewInternal("failed to delete session todo", err)
	}
	return nil
}
