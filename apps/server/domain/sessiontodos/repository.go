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

// SessionAccessible reports whether the caller (projectID, ownerUserID) may
// access the given session's todos.
//
// A session's todos belong to the conversation the session backs, so ownership
// follows the chat conversation model settled in #1010: the session must be in
// the caller's project AND, when the session is linked to a chat conversation
// (kb.chat_conversations.acp_session_id), that conversation must be owned by
// the caller (owner_user_id) or be non-private (is_private = false). Sessions
// with no linked conversation fall back to project scope (the documented
// "scoped to an agent session" model, where sessions carry only project_id).
// A foreign or unknown session id fails closed (no row matches).
func (r *Repository) SessionAccessible(ctx context.Context, sessionID, projectID, ownerUserID string) (bool, error) {
	exists, err := r.db.NewSelect().
		TableExpr("kb.acp_sessions AS s").
		Join("LEFT JOIN kb.chat_conversations AS c ON c.acp_session_id = s.id").
		Where("s.id = ?", sessionID).
		Where("s.project_id = ?", projectID).
		Where("(c.id IS NULL OR c.owner_user_id = ? OR c.is_private = false)", ownerUserID).
		Exists(ctx)
	if err != nil {
		return false, apperror.NewInternal("failed to check session access", err)
	}
	return exists, nil
}

// List returns todos for a session, optionally filtered by status.
func (r *Repository) List(ctx context.Context, sessionID string, statuses []TodoStatus) ([]*SessionTodo, error) {
	var todos []*SessionTodo
	q := r.db.NewSelect().Model(&todos).
		Where("st.session_id = ?", sessionID).
		OrderExpr(`st."order" ASC, st.created_at ASC`)
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
