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
	todos      map[string]*SessionTodo
	nextID     int
	accessible bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{todos: make(map[string]*SessionTodo), accessible: true}
}

func (f *fakeRepo) SessionAccessible(_ context.Context, _ string, _ string, _ string) (bool, error) {
	return f.accessible, nil
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
	SessionAccessible(ctx context.Context, sessionID, projectID, ownerUserID string) (bool, error)
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

func (s *testService) checkAccess(ctx context.Context, sessionID, projectID, ownerUserID string) error {
	ok, err := s.repo.SessionAccessible(ctx, sessionID, projectID, ownerUserID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NewNotFound("session", sessionID)
	}
	return nil
}

func (s *testService) List(ctx context.Context, projectID, ownerUserID, sessionID string, statuses []TodoStatus) ([]*SessionTodo, error) {
	if sessionID == "" {
		return nil, apperror.NewBadRequest("sessionId is required")
	}
	if err := s.checkAccess(ctx, sessionID, projectID, ownerUserID); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, sessionID, statuses)
}

func (s *testService) Create(ctx context.Context, projectID, ownerUserID, sessionID string, req CreateTodoRequest) (*SessionTodo, error) {
	if sessionID == "" {
		return nil, apperror.NewBadRequest("sessionId is required")
	}
	if req.Content == "" {
		return nil, apperror.NewBadRequest("content is required")
	}
	if err := s.checkAccess(ctx, sessionID, projectID, ownerUserID); err != nil {
		return nil, err
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

func (s *testService) Update(ctx context.Context, projectID, ownerUserID, sessionID, todoID string, req UpdateTodoRequest) (*SessionTodo, error) {
	if err := s.checkAccess(ctx, sessionID, projectID, ownerUserID); err != nil {
		return nil, err
	}
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

func (s *testService) Delete(ctx context.Context, projectID, ownerUserID, sessionID, todoID string) error {
	if err := s.checkAccess(ctx, sessionID, projectID, ownerUserID); err != nil {
		return err
	}
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

// testProject/testOwner are the arbitrary caller identity used by the in-memory
// mirror tests; the fakeRepo reports every session as accessible, so ownership
// is not exercised here (see ownership_test.go for the hermetic two-user suite).
const (
	testProject = "proj-1"
	testOwner   = "user-1"
)

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
	_, err := svc.List(context.Background(), testProject, testOwner, "", nil)
	assertBadRequest(t, err)
}

func TestService_Create_EmptySessionID(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.Create(context.Background(), testProject, testOwner, "", CreateTodoRequest{Content: "do something"})
	assertBadRequest(t, err)
}

func TestService_Create_EmptyContent(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.Create(context.Background(), testProject, testOwner, "session-1", CreateTodoRequest{})
	assertBadRequest(t, err)
}

func TestService_Create_DefaultsStatusToDraft(t *testing.T) {
	svc, _ := newTestService()
	todo, err := svc.Create(context.Background(), testProject, testOwner, "sess-abc", CreateTodoRequest{Content: "write tests"})
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
	todo, err := svc.Create(context.Background(), testProject, testOwner, "sess-abc", CreateTodoRequest{Content: "step 5", Order: &order})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if todo.Order != 5 {
		t.Errorf("expected order 5, got %d", todo.Order)
	}
}

func TestService_Create_DefaultOrderZero(t *testing.T) {
	svc, _ := newTestService()
	todo, err := svc.Create(context.Background(), testProject, testOwner, "sess", CreateTodoRequest{Content: "task"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if todo.Order != 0 {
		t.Errorf("expected order 0, got %d", todo.Order)
	}
}

func TestService_Update_WrongSession_ReturnsNotFound(t *testing.T) {
	svc, _ := newTestService()
	todo, _ := svc.Create(context.Background(), testProject, testOwner, "session-A", CreateTodoRequest{Content: "task"})
	status := StatusCompleted
	_, err := svc.Update(context.Background(), testProject, testOwner, "session-B", todo.ID, UpdateTodoRequest{Status: &status})
	assertNotFound(t, err)
}

func TestService_Update_NoFields_ReturnsUnchangedTodo(t *testing.T) {
	svc, _ := newTestService()
	todo, _ := svc.Create(context.Background(), testProject, testOwner, "sess", CreateTodoRequest{Content: "original"})
	updated, err := svc.Update(context.Background(), testProject, testOwner, "sess", todo.ID, UpdateTodoRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Content != "original" {
		t.Errorf("content should be unchanged, got %s", updated.Content)
	}
}

func TestService_Update_Status(t *testing.T) {
	svc, _ := newTestService()
	todo, _ := svc.Create(context.Background(), testProject, testOwner, "sess", CreateTodoRequest{Content: "task"})
	status := StatusInProgress
	updated, err := svc.Update(context.Background(), testProject, testOwner, "sess", todo.ID, UpdateTodoRequest{Status: &status})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != StatusInProgress {
		t.Errorf("expected in_progress, got %s", updated.Status)
	}
}

func TestService_Update_Content(t *testing.T) {
	svc, _ := newTestService()
	todo, _ := svc.Create(context.Background(), testProject, testOwner, "sess", CreateTodoRequest{Content: "old"})
	newContent := "new content"
	updated, err := svc.Update(context.Background(), testProject, testOwner, "sess", todo.ID, UpdateTodoRequest{Content: &newContent})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Content != "new content" {
		t.Errorf("expected 'new content', got %s", updated.Content)
	}
}

func TestService_Delete_WrongSession_ReturnsNotFound(t *testing.T) {
	svc, _ := newTestService()
	todo, _ := svc.Create(context.Background(), testProject, testOwner, "session-A", CreateTodoRequest{Content: "task"})
	err := svc.Delete(context.Background(), testProject, testOwner, "session-B", todo.ID)
	assertNotFound(t, err)
}

func TestService_Delete_OwnSession_Succeeds(t *testing.T) {
	svc, fake := newTestService()
	todo, _ := svc.Create(context.Background(), testProject, testOwner, "sess", CreateTodoRequest{Content: "task"})
	err := svc.Delete(context.Background(), testProject, testOwner, "sess", todo.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := fake.todos[todo.ID]; exists {
		t.Error("todo should have been deleted from repo")
	}
}

func TestService_Delete_NonexistentTodo_ReturnsNotFound(t *testing.T) {
	svc, _ := newTestService()
	err := svc.Delete(context.Background(), testProject, testOwner, "sess", "no-such-id")
	assertNotFound(t, err)
}

func TestService_List_StatusFilter(t *testing.T) {
	svc, _ := newTestService()
	_, _ = svc.Create(context.Background(), testProject, testOwner, "sess", CreateTodoRequest{Content: "a"}) // draft
	todo2, _ := svc.Create(context.Background(), testProject, testOwner, "sess", CreateTodoRequest{Content: "b"})
	completedStatus := StatusCompleted
	_, _ = svc.Update(context.Background(), testProject, testOwner, "sess", todo2.ID, UpdateTodoRequest{Status: &completedStatus})

	todos, err := svc.List(context.Background(), testProject, testOwner, "sess", []TodoStatus{StatusCompleted})
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
	_, _ = svc.Create(context.Background(), testProject, testOwner, "sess", CreateTodoRequest{Content: "a"})
	_, _ = svc.Create(context.Background(), testProject, testOwner, "sess", CreateTodoRequest{Content: "b"})

	todos, err := svc.List(context.Background(), testProject, testOwner, "sess", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(todos) != 2 {
		t.Errorf("expected 2 todos, got %d", len(todos))
	}
}

func TestService_List_IsolatesBySession(t *testing.T) {
	svc, _ := newTestService()
	_, _ = svc.Create(context.Background(), testProject, testOwner, "sess-1", CreateTodoRequest{Content: "for sess-1"})
	_, _ = svc.Create(context.Background(), testProject, testOwner, "sess-2", CreateTodoRequest{Content: "for sess-2"})

	todos, err := svc.List(context.Background(), testProject, testOwner, "sess-1", nil)
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

func TestService_List_ForeignSession_ReturnsNotFound(t *testing.T) {
	svc, fake := newTestService()
	fake.accessible = false
	_, err := svc.List(context.Background(), testProject, testOwner, "foreign-sess", nil)
	assertNotFound(t, err)
}

func TestService_Create_ForeignSession_ReturnsNotFound(t *testing.T) {
	svc, fake := newTestService()
	fake.accessible = false
	_, err := svc.Create(context.Background(), testProject, testOwner, "foreign-sess", CreateTodoRequest{Content: "x"})
	assertNotFound(t, err)
}

func TestService_Update_ForeignSession_ReturnsNotFound(t *testing.T) {
	svc, fake := newTestService()
	todo, _ := svc.Create(context.Background(), testProject, testOwner, "owned-sess", CreateTodoRequest{Content: "task"})
	fake.accessible = false
	status := StatusCompleted
	_, err := svc.Update(context.Background(), testProject, testOwner, "owned-sess", todo.ID, UpdateTodoRequest{Status: &status})
	assertNotFound(t, err)
}

func TestService_Delete_ForeignSession_ReturnsNotFound(t *testing.T) {
	svc, fake := newTestService()
	todo, _ := svc.Create(context.Background(), testProject, testOwner, "owned-sess", CreateTodoRequest{Content: "task"})
	fake.accessible = false
	err := svc.Delete(context.Background(), testProject, testOwner, "owned-sess", todo.ID)
	assertNotFound(t, err)
}
