package sessiontodos

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ---------------------------------------------------------------------------
// Fake repository — implements the same interface as Repository but in-memory
// ---------------------------------------------------------------------------

type fakeRepo struct {
	todos  map[string]*SessionTodo
	nextID int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{todos: make(map[string]*SessionTodo)}
}

func (f *fakeRepo) List(_ context.Context, sessionID string, statuses []TodoStatus) ([]*SessionTodo, error) {
	var out []*SessionTodo
	for _, t := range f.todos {
		if t.SessionID != sessionID {
			continue
		}
		if len(statuses) > 0 {
			match := false
			for _, s := range statuses {
				if t.Status == s {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeRepo) Get(_ context.Context, todoID string) (*SessionTodo, error) {
	t, ok := f.todos[todoID]
	if !ok {
		return nil, apperror.NewNotFound("session_todo", todoID)
	}
	return t, nil
}

func (f *fakeRepo) Create(_ context.Context, todo *SessionTodo) error {
	f.nextID++
	todo.ID = fmt.Sprintf("todo-%d", f.nextID)
	clone := *todo
	f.todos[todo.ID] = &clone
	return nil
}

func (f *fakeRepo) Update(_ context.Context, todo *SessionTodo, _ ...string) error {
	clone := *todo
	f.todos[todo.ID] = &clone
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, todoID string) error {
	delete(f.todos, todoID)
	return nil
}

// ---------------------------------------------------------------------------
// testService — mirrors Service but accepts a repoIface to avoid bun.IDB dep
// ---------------------------------------------------------------------------

type repoIface interface {
	List(ctx context.Context, sessionID string, statuses []TodoStatus) ([]*SessionTodo, error)
	Get(ctx context.Context, todoID string) (*SessionTodo, error)
	Create(ctx context.Context, todo *SessionTodo) error
	Update(ctx context.Context, todo *SessionTodo, columns ...string) error
	Delete(ctx context.Context, todoID string) error
}

type testService struct {
	repo repoIface
	log  *slog.Logger
}

func newTestService() (*testService, *fakeRepo) {
	fake := newFakeRepo()
	return &testService{repo: fake, log: slog.Default()}, fake
}

func (s *testService) List(ctx context.Context, sessionID string, statuses []TodoStatus) ([]*SessionTodo, error) {
	if sessionID == "" {
		return nil, apperror.NewBadRequest("sessionId is required")
	}
	return s.repo.List(ctx, sessionID, statuses)
}

func (s *testService) Create(ctx context.Context, sessionID string, req CreateTodoRequest) (*SessionTodo, error) {
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
		SessionID: sessionID,
		Content:   req.Content,
		Status:    StatusDraft,
		Author:    req.Author,
		Order:     order,
	}
	if err := s.repo.Create(ctx, todo); err != nil {
		return nil, err
	}
	return todo, nil
}

func (s *testService) Update(ctx context.Context, sessionID, todoID string, req UpdateTodoRequest) (*SessionTodo, error) {
	todo, err := s.repo.Get(ctx, todoID)
	if err != nil {
		return nil, err
	}
	if todo.SessionID != sessionID {
		return nil, apperror.NewNotFound("session_todo", todoID)
	}
	var cols []string
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
	if err := s.repo.Update(ctx, todo, cols...); err != nil {
		return nil, err
	}
	return todo, nil
}

func (s *testService) Delete(ctx context.Context, sessionID, todoID string) error {
	todo, err := s.repo.Get(ctx, todoID)
	if err != nil {
		return err
	}
	if todo.SessionID != sessionID {
		return apperror.NewNotFound("session_todo", todoID)
	}
	return s.repo.Delete(ctx, todoID)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	appErr, ok := err.(*apperror.Error)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T: %v", err, err)
	}
	if appErr.HTTPStatus != 400 {
		t.Errorf("expected HTTP 400, got %d", appErr.HTTPStatus)
	}
}

func assertNotFound(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	appErr, ok := err.(*apperror.Error)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T: %v", err, err)
	}
	if appErr.HTTPStatus != 404 {
		t.Errorf("expected HTTP 404, got %d", appErr.HTTPStatus)
	}
}

func TestService_List_EmptySessionID(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.List(context.Background(), "", nil)
	assertBadRequest(t, err)
}

func TestService_Create_EmptySessionID(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.Create(context.Background(), "", CreateTodoRequest{Content: "do something"})
	assertBadRequest(t, err)
}

func TestService_Create_EmptyContent(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.Create(context.Background(), "session-1", CreateTodoRequest{})
	assertBadRequest(t, err)
}

func TestService_Create_DefaultsStatusToDraft(t *testing.T) {
	svc, _ := newTestService()
	todo, err := svc.Create(context.Background(), "sess-abc", CreateTodoRequest{Content: "write tests"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if todo.Status != StatusDraft {
		t.Errorf("expected status draft, got %s", todo.Status)
	}
	if todo.SessionID != "sess-abc" {
		t.Errorf("expected sessionID sess-abc, got %s", todo.SessionID)
	}
	if todo.Content != "write tests" {
		t.Errorf("expected content 'write tests', got %s", todo.Content)
	}
}

func TestService_Create_OrderFromRequest(t *testing.T) {
	svc, _ := newTestService()
	order := 5
	todo, err := svc.Create(context.Background(), "sess-abc", CreateTodoRequest{Content: "step 5", Order: &order})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if todo.Order != 5 {
		t.Errorf("expected order 5, got %d", todo.Order)
	}
}

func TestService_Create_DefaultOrderZero(t *testing.T) {
	svc, _ := newTestService()
	todo, err := svc.Create(context.Background(), "sess", CreateTodoRequest{Content: "task"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if todo.Order != 0 {
		t.Errorf("expected order 0, got %d", todo.Order)
	}
}

func TestService_Update_WrongSession_ReturnsNotFound(t *testing.T) {
	svc, _ := newTestService()
	todo, _ := svc.Create(context.Background(), "session-A", CreateTodoRequest{Content: "task"})
	status := StatusCompleted
	_, err := svc.Update(context.Background(), "session-B", todo.ID, UpdateTodoRequest{Status: &status})
	assertNotFound(t, err)
}

func TestService_Update_NoFields_ReturnsUnchangedTodo(t *testing.T) {
	svc, _ := newTestService()
	todo, _ := svc.Create(context.Background(), "sess", CreateTodoRequest{Content: "original"})
	updated, err := svc.Update(context.Background(), "sess", todo.ID, UpdateTodoRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Content != "original" {
		t.Errorf("content should be unchanged, got %s", updated.Content)
	}
}

func TestService_Update_Status(t *testing.T) {
	svc, _ := newTestService()
	todo, _ := svc.Create(context.Background(), "sess", CreateTodoRequest{Content: "task"})
	status := StatusInProgress
	updated, err := svc.Update(context.Background(), "sess", todo.ID, UpdateTodoRequest{Status: &status})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != StatusInProgress {
		t.Errorf("expected in_progress, got %s", updated.Status)
	}
}

func TestService_Update_Content(t *testing.T) {
	svc, _ := newTestService()
	todo, _ := svc.Create(context.Background(), "sess", CreateTodoRequest{Content: "old"})
	newContent := "new content"
	updated, err := svc.Update(context.Background(), "sess", todo.ID, UpdateTodoRequest{Content: &newContent})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Content != "new content" {
		t.Errorf("expected 'new content', got %s", updated.Content)
	}
}

func TestService_Delete_WrongSession_ReturnsNotFound(t *testing.T) {
	svc, _ := newTestService()
	todo, _ := svc.Create(context.Background(), "session-A", CreateTodoRequest{Content: "task"})
	err := svc.Delete(context.Background(), "session-B", todo.ID)
	assertNotFound(t, err)
}

func TestService_Delete_OwnSession_Succeeds(t *testing.T) {
	svc, fake := newTestService()
	todo, _ := svc.Create(context.Background(), "sess", CreateTodoRequest{Content: "task"})
	err := svc.Delete(context.Background(), "sess", todo.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := fake.todos[todo.ID]; exists {
		t.Error("todo should have been deleted from repo")
	}
}

func TestService_Delete_NonexistentTodo_ReturnsNotFound(t *testing.T) {
	svc, _ := newTestService()
	err := svc.Delete(context.Background(), "sess", "no-such-id")
	assertNotFound(t, err)
}

func TestService_List_StatusFilter(t *testing.T) {
	svc, _ := newTestService()
	svc.Create(context.Background(), "sess", CreateTodoRequest{Content: "a"}) // draft
	todo2, _ := svc.Create(context.Background(), "sess", CreateTodoRequest{Content: "b"})
	completedStatus := StatusCompleted
	svc.Update(context.Background(), "sess", todo2.ID, UpdateTodoRequest{Status: &completedStatus})

	todos, err := svc.List(context.Background(), "sess", []TodoStatus{StatusCompleted})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(todos) != 1 {
		t.Errorf("expected 1 completed todo, got %d", len(todos))
	}
	if todos[0].ID != todo2.ID {
		t.Errorf("expected todo2 %s, got %s", todo2.ID, todos[0].ID)
	}
}

func TestService_List_NoFilter_ReturnsAll(t *testing.T) {
	svc, _ := newTestService()
	svc.Create(context.Background(), "sess", CreateTodoRequest{Content: "a"})
	svc.Create(context.Background(), "sess", CreateTodoRequest{Content: "b"})

	todos, err := svc.List(context.Background(), "sess", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(todos) != 2 {
		t.Errorf("expected 2 todos, got %d", len(todos))
	}
}

func TestService_List_IsolatesBySession(t *testing.T) {
	svc, _ := newTestService()
	svc.Create(context.Background(), "sess-1", CreateTodoRequest{Content: "for sess-1"})
	svc.Create(context.Background(), "sess-2", CreateTodoRequest{Content: "for sess-2"})

	todos, err := svc.List(context.Background(), "sess-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(todos) != 1 {
		t.Errorf("expected 1 todo for sess-1, got %d", len(todos))
	}
	if todos[0].Content != "for sess-1" {
		t.Errorf("unexpected content: %s", todos[0].Content)
	}
}
