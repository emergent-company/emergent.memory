package sessiontodos

import (
	"context"
	"log/slog"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Service provides business logic for session todos.
type Service struct {
	repo *Repository
	log  *slog.Logger
}

// NewService creates a new session todos service.
func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.Scope("sessiontodos.service")),
	}
}

// List returns todos for a session, optionally filtered by status.
func (s *Service) List(ctx context.Context, sessionID string, statuses []TodoStatus) ([]*SessionTodo, error) {
	if sessionID == "" {
		return nil, apperror.NewBadRequest("sessionId is required")
	}
	return s.repo.List(ctx, sessionID, statuses)
}

// Create creates a new todo draft for a session.
func (s *Service) Create(ctx context.Context, sessionID string, req CreateTodoRequest) (*SessionTodo, error) {
	if sessionID == "" {
		return nil, apperror.NewBadRequest("sessionId is required")
	}
	if req.Content == "" {
		return nil, apperror.NewBadRequest("content is required")
	}
	order := 0
	if req.Order != nil {
		order = *req.Order
	}
	todo := &SessionTodo{
		SessionID:       sessionID,
		Content:         req.Content,
		Status:          StatusDraft,
		Author:          req.Author,
		Order:           order,
		ContextSnapshot: req.ContextSnapshot,
	}
	if err := s.repo.Create(ctx, todo); err != nil {
		return nil, err
	}
	return todo, nil
}

// Update applies a partial update to a todo.
func (s *Service) Update(ctx context.Context, sessionID, todoID string, req UpdateTodoRequest) (*SessionTodo, error) {
	todo, err := s.repo.Get(ctx, todoID)
	if err != nil {
		return nil, err
	}
	if todo.SessionID != sessionID {
		return nil, apperror.NewNotFound("session_todo", todoID)
	}
	cols := []string{}
	if req.Status != nil {
		todo.Status = *req.Status
		cols = append(cols, "status")
	}
	if req.Content != nil {
		todo.Content = *req.Content
		cols = append(cols, "content")
	}
	if req.Order != nil {
		todo.Order = *req.Order
		cols = append(cols, "order")
	}
	if len(cols) == 0 {
		return todo, nil
	}
	todo.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, todo, cols...); err != nil {
		return nil, err
	}
	return todo, nil
}

// Delete removes a todo from a session.
func (s *Service) Delete(ctx context.Context, sessionID, todoID string) error {
	todo, err := s.repo.Get(ctx, todoID)
	if err != nil {
		return err
	}
	if todo.SessionID != sessionID {
		return apperror.NewNotFound("session_todo", todoID)
	}
	return s.repo.Delete(ctx, todoID)
}
