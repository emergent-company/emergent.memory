package agents_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/testutil"
)

// fixtureTodo returns a sample SessionTodo for tests.
func fixtureTodo() *agents.SessionTodo {
	author := "test-agent"
	return &agents.SessionTodo{
		ID:        "todo-abc123",
		SessionID: "sess-xyz",
		Content:   "implement feature",
		Status:    agents.TodoStatusPending,
		Author:    &author,
		Order:     1,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}
}

func newAgentsClient(t *testing.T, mockURL string) *agents.Client {
	t.Helper()
	client, err := sdk.New(sdk.Config{
		ServerURL: mockURL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})
	if err != nil {
		t.Fatalf("failed to create SDK client: %v", err)
	}
	return client.Agents
}

// ---------------------------------------------------------------------------
// ListSessionTodos
// ---------------------------------------------------------------------------

func TestListSessionTodos_Success(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	todo := fixtureTodo()
	mock.OnJSON("GET", "/api/v1/agent/sessions/sess-xyz/todos", http.StatusOK, []*agents.SessionTodo{todo})

	client := newAgentsClient(t, mock.URL)
	todos, err := client.ListSessionTodos(context.Background(), "sess-xyz", nil)
	if err != nil {
		t.Fatalf("ListSessionTodos() error = %v", err)
	}
	if len(todos) != 1 {
		t.Fatalf("expected 1 todo, got %d", len(todos))
	}
	if todos[0].ID != todo.ID {
		t.Errorf("expected todo ID %s, got %s", todo.ID, todos[0].ID)
	}
	if todos[0].Content != todo.Content {
		t.Errorf("expected content %q, got %q", todo.Content, todos[0].Content)
	}
}

func TestListSessionTodos_EmptySessionID_ReturnsError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	client := newAgentsClient(t, mock.URL)
	_, err := client.ListSessionTodos(context.Background(), "", nil)
	if err == nil {
		t.Fatal("expected error for empty sessionID")
	}
}

func TestListSessionTodos_WithStatusFilter_BuildsQueryParam(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	var capturedQuery string
	mock.On("GET", "/api/v1/agent/sessions/sess-1/todos", func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]*agents.SessionTodo{})
	})

	client := newAgentsClient(t, mock.URL)
	_, err := client.ListSessionTodos(context.Background(), "sess-1", []agents.TodoStatus{
		agents.TodoStatusPending,
		agents.TodoStatusInProgress,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedQuery == "" {
		t.Error("expected query param to be set")
	}
	if capturedQuery != "status=pending,in_progress" {
		t.Errorf("unexpected query param: %s", capturedQuery)
	}
}

// ---------------------------------------------------------------------------
// CreateSessionTodo
// ---------------------------------------------------------------------------

func TestCreateSessionTodo_Success(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	todo := fixtureTodo()
	mock.OnJSON("POST", "/api/v1/agent/sessions/sess-xyz/todos", http.StatusCreated, todo)

	client := newAgentsClient(t, mock.URL)
	result, err := client.CreateSessionTodo(context.Background(), "sess-xyz", agents.CreateTodoRequest{
		Content: "implement feature",
	})
	if err != nil {
		t.Fatalf("CreateSessionTodo() error = %v", err)
	}
	if result.ID != todo.ID {
		t.Errorf("expected todo ID %s, got %s", todo.ID, result.ID)
	}
	if result.Content != todo.Content {
		t.Errorf("expected content %q, got %q", todo.Content, result.Content)
	}
}

func TestCreateSessionTodo_EmptySessionID_ReturnsError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	client := newAgentsClient(t, mock.URL)
	_, err := client.CreateSessionTodo(context.Background(), "", agents.CreateTodoRequest{Content: "task"})
	if err == nil {
		t.Fatal("expected error for empty sessionID")
	}
}

func TestCreateSessionTodo_ServerError_ReturnsError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("POST", "/api/v1/agent/sessions/sess-1/todos", http.StatusBadRequest, map[string]any{
		"error": map[string]any{"code": "bad_request", "message": "content is required"},
	})

	client := newAgentsClient(t, mock.URL)
	_, err := client.CreateSessionTodo(context.Background(), "sess-1", agents.CreateTodoRequest{})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

// ---------------------------------------------------------------------------
// UpdateSessionTodo
// ---------------------------------------------------------------------------

func TestUpdateSessionTodo_Success(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	updated := fixtureTodo()
	updated.Status = agents.TodoStatusCompleted
	mock.OnJSON("PATCH", "/api/v1/agent/sessions/sess-xyz/todos/todo-abc123", http.StatusOK, updated)

	status := agents.TodoStatusCompleted
	client := newAgentsClient(t, mock.URL)
	result, err := client.UpdateSessionTodo(context.Background(), "sess-xyz", "todo-abc123", agents.UpdateTodoRequest{
		Status: &status,
	})
	if err != nil {
		t.Fatalf("UpdateSessionTodo() error = %v", err)
	}
	if result.Status != agents.TodoStatusCompleted {
		t.Errorf("expected completed status, got %s", result.Status)
	}
}

func TestUpdateSessionTodo_EmptyIDs_ReturnsError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	client := newAgentsClient(t, mock.URL)

	// Empty sessionID
	_, err := client.UpdateSessionTodo(context.Background(), "", "todo-1", agents.UpdateTodoRequest{})
	if err == nil {
		t.Fatal("expected error for empty sessionID")
	}

	// Empty todoID
	_, err = client.UpdateSessionTodo(context.Background(), "sess-1", "", agents.UpdateTodoRequest{})
	if err == nil {
		t.Fatal("expected error for empty todoID")
	}
}

// ---------------------------------------------------------------------------
// DeleteSessionTodo
// ---------------------------------------------------------------------------

func TestDeleteSessionTodo_Success(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("DELETE", "/api/v1/agent/sessions/sess-xyz/todos/todo-abc123", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	client := newAgentsClient(t, mock.URL)
	err := client.DeleteSessionTodo(context.Background(), "sess-xyz", "todo-abc123")
	if err != nil {
		t.Fatalf("DeleteSessionTodo() error = %v", err)
	}
}

func TestDeleteSessionTodo_EmptyIDs_ReturnsError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	client := newAgentsClient(t, mock.URL)

	// Empty sessionID
	err := client.DeleteSessionTodo(context.Background(), "", "todo-1")
	if err == nil {
		t.Fatal("expected error for empty sessionID")
	}

	// Empty todoID
	err = client.DeleteSessionTodo(context.Background(), "sess-1", "")
	if err == nil {
		t.Fatal("expected error for empty todoID")
	}
}

func TestDeleteSessionTodo_NotFound_ReturnsError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("DELETE", "/api/v1/agent/sessions/sess-1/todos/no-such-id", http.StatusNotFound, map[string]any{
		"error": map[string]any{"code": "not_found", "message": "session_todo 'no-such-id' not found"},
	})

	client := newAgentsClient(t, mock.URL)
	err := client.DeleteSessionTodo(context.Background(), "sess-1", "no-such-id")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}
