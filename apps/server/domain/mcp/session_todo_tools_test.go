package mcp

import (
	"context"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/sessiontodos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nonNilTodoSvc returns a *sessiontodos.Service pointer that is non-nil but has
// nil internal fields. This is sufficient to pass the nil-guard check in the
// execute functions; tests only exercise argument-validation paths that return
// before any method on the service is called.
func nonNilTodoSvc() *sessiontodos.Service {
	return new(sessiontodos.Service)
}

// ---------------------------------------------------------------------------
// executeSessionTodoList — argument validation tests
// ---------------------------------------------------------------------------

// TestExecuteSessionTodoList_NilService verifies the nil-guard returns an error
// rather than panicking when sessionTodoSvc is not injected.
func TestExecuteSessionTodoList_NilService(t *testing.T) {
	svc := &Service{sessionTodoSvc: nil}
	_, err := svc.executeSessionTodoList(context.Background(), map[string]any{
		"session_id": "sess-abc",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

// TestExecuteSessionTodoList_MissingSessionID returns an error when session_id is absent.
func TestExecuteSessionTodoList_MissingSessionID(t *testing.T) {
	// We need a non-nil sessionTodoSvc to pass the nil guard; we use a zero-value
	// pointer — the function checks sessionTodoSvc != nil before checking args,
	// so we must provide a non-nil pointer. We craft one via new().
	// Note: calling List on this service would panic (nil repo), but the
	// session_id check fires first so it never reaches List.
	svc := &Service{sessionTodoSvc: nonNilTodoSvc()}
	_, err := svc.executeSessionTodoList(context.Background(), map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session_id")
}

// TestExecuteSessionTodoList_EmptySessionID same but with empty string value.
func TestExecuteSessionTodoList_EmptySessionID(t *testing.T) {
	svc := &Service{sessionTodoSvc: nonNilTodoSvc()}
	_, err := svc.executeSessionTodoList(context.Background(), map[string]any{
		"session_id": "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session_id")
}

// ---------------------------------------------------------------------------
// executeSessionTodoUpdate — argument validation tests
// ---------------------------------------------------------------------------

// TestExecuteSessionTodoUpdate_NilService verifies the nil-guard.
func TestExecuteSessionTodoUpdate_NilService(t *testing.T) {
	svc := &Service{sessionTodoSvc: nil}
	_, err := svc.executeSessionTodoUpdate(context.Background(), map[string]any{
		"session_id": "sess",
		"todo_id":    "todo-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

// TestExecuteSessionTodoUpdate_MissingSessionID returns error when session_id absent.
func TestExecuteSessionTodoUpdate_MissingSessionID(t *testing.T) {
	svc := &Service{sessionTodoSvc: nonNilTodoSvc()}
	_, err := svc.executeSessionTodoUpdate(context.Background(), map[string]any{
		"todo_id": "todo-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session_id")
}

// TestExecuteSessionTodoUpdate_MissingTodoID returns error when todo_id absent.
func TestExecuteSessionTodoUpdate_MissingTodoID(t *testing.T) {
	svc := &Service{sessionTodoSvc: nonNilTodoSvc()}
	_, err := svc.executeSessionTodoUpdate(context.Background(), map[string]any{
		"session_id": "sess",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "todo_id")
}

// ---------------------------------------------------------------------------
// sessionTodoToolDefinitions — schema validation
// ---------------------------------------------------------------------------

// TestSessionTodoToolDefinitions_Names verifies the two tool names are correct.
func TestSessionTodoToolDefinitions_Names(t *testing.T) {
	defs := sessionTodoToolDefinitions()
	require.Len(t, defs, 2)

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	assert.True(t, names["session-todo-list"], "expected session-todo-list tool")
	assert.True(t, names["session-todo-update"], "expected session-todo-update tool")
}

// TestSessionTodoToolDefinitions_RequiredFields checks required fields in schemas.
func TestSessionTodoToolDefinitions_RequiredFields(t *testing.T) {
	defs := sessionTodoToolDefinitions()
	for _, d := range defs {
		require.NotEmpty(t, d.InputSchema.Required,
			"tool %q should declare required fields", d.Name)
		require.Contains(t, d.InputSchema.Required, "session_id",
			"tool %q must require session_id", d.Name)
	}

	// session-todo-update also requires todo_id
	for _, d := range defs {
		if d.Name == "session-todo-update" {
			assert.Contains(t, d.InputSchema.Required, "todo_id")
		}
	}
}

// ---------------------------------------------------------------------------
// stub — satisfies the interface check for sessionTodoSvc field type
// ---------------------------------------------------------------------------
// sessionTodosSvcStub is a zero-value placeholder that satisfies the field type
// (*sessiontodos.Service). We only need it to be non-nil so the nil guard
// passes; the test cases that use it always trigger the arg-validation path
// before any method on the service is actually called.
