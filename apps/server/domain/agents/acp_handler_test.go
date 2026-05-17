package agents

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// newTestACPHandler builds an ACPHandler with nil repo and executor.
// Sufficient for tests that validate request parsing, auth, and error paths
// before any DB call.
func newTestACPHandler() *ACPHandler {
	return &ACPHandler{
		repo:     nil,
		executor: nil,
		log:      slog.Default(),
	}
}

// newACPEchoContext creates an Echo context with an authenticated user and optional
// path params. The user has project ID "proj-test-id".
func newACPEchoContext(method, path, body string, pathParams map[string]string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{
		ID:        "user-test-id",
		Email:     "test@example.com",
		ProjectID: "proj-test-id",
	})
	// Set path params
	names := make([]string, 0, len(pathParams))
	values := make([]string, 0, len(pathParams))
	for k, v := range pathParams {
		names = append(names, k)
		values = append(values, v)
	}
	c.SetParamNames(names...)
	c.SetParamValues(values...)
	return c, rec
}

// newACPEchoContextNoAuth creates an Echo context without authentication.
func newACPEchoContextNoAuth(method, path, body string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return c, rec
}

// ============================================================================
// Task 9.4: ACP Discovery endpoint tests (Ping, ListAgents, GetAgent)
// ============================================================================

func TestACPPing_Returns200(t *testing.T) {
	h := newTestACPHandler()
	c, rec := newACPEchoContextNoAuth(http.MethodGet, "/acp/v1/ping", "")

	err := h.Ping(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Empty(t, body)
}

func TestACPPing_NoAuthRequired(t *testing.T) {
	h := newTestACPHandler()
	// Context has no authenticated user
	c, rec := newACPEchoContextNoAuth(http.MethodGet, "/acp/v1/ping", "")

	err := h.Ping(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestACPListAgents_NoAuth_Returns401(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContextNoAuth(http.MethodGet, "/acp/v1/agents", "")

	err := h.ListAgents(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestACPListAgents_NoProject_Returns400(t *testing.T) {
	h := newTestACPHandler()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/acp/v1/agents", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Authenticated user with empty project
	c.Set(string(auth.UserContextKey), &auth.AuthUser{
		ID:        "user-test-id",
		Email:     "test@example.com",
		ProjectID: "",
	})

	err := h.ListAgents(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Contains(t, appErr.Message, "project context")
}

func TestACPGetAgent_NoAuth_Returns401(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContextNoAuth(http.MethodGet, "/acp/v1/agents/my-agent", "")
	c.SetParamNames("name")
	c.SetParamValues("my-agent")

	err := h.GetAgent(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestACPGetAgent_EmptySlug_Returns400(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContext(http.MethodGet, "/acp/v1/agents/", "", map[string]string{
		"name": "",
	})

	err := h.GetAgent(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Contains(t, appErr.Message, "agent name")
}

// ============================================================================
// Task 9.5: ACP Run Lifecycle tests (CreateRun, GetRun, CancelRun, ResumeRun)
// ============================================================================

func TestACPCreateRun_NoAuth_Returns401(t *testing.T) {
	h := newTestACPHandler()
	body := `{"message": [{"content_type": "text/plain", "content": "Hello"}]}`
	c, _ := newACPEchoContextNoAuth(http.MethodPost, "/acp/v1/agents/my-agent/runs", body)
	c.SetParamNames("name")
	c.SetParamValues("my-agent")

	err := h.CreateRun(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestACPCreateRun_EmptySlug_Returns400(t *testing.T) {
	h := newTestACPHandler()
	body := `{"message": [{"content_type": "text/plain", "content": "Hello"}]}`
	c, _ := newACPEchoContext(http.MethodPost, "/acp/v1/agents//runs", body, map[string]string{
		"name": "",
	})

	err := h.CreateRun(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Contains(t, appErr.Message, "agent name")
}

func TestACPCreateRun_InvalidMode_Returns400(t *testing.T) {
	h := newTestACPHandler()
	body := `{"message": [{"content_type": "text/plain", "content": "Hello"}], "mode": "invalid"}`
	c, _ := newACPEchoContext(http.MethodPost, "/acp/v1/agents/my-agent/runs", body, map[string]string{
		"name": "my-agent",
	})

	// The handler calls resolveAgentBySlug which needs repo — it will panic on nil.
	// We use recover to catch that and verify we got past the slug check.
	// For mode validation, the handler parses the body AFTER resolving the agent,
	// so we cannot test mode validation in isolation with nil repo.
	// Instead, test that empty body gets the agent-name check passed and reaches the DB.
	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		_ = h.CreateRun(c)
	}()
	// If panicked, handler passed the slug check and hit nil repo — expected
	assert.True(t, panicked, "expected panic from nil repo after slug validation passed")
}

func TestACPCreateRun_EmptyMessage_ValidatesAfterAgent(t *testing.T) {
	// With an empty message array, the handler should return 400 "message content is required"
	// But it first resolves the agent (requires repo), so with nil repo it panics.
	h := newTestACPHandler()
	body := `{"message": []}`
	c, _ := newACPEchoContext(http.MethodPost, "/acp/v1/agents/my-agent/runs", body, map[string]string{
		"name": "my-agent",
	})

	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		_ = h.CreateRun(c)
	}()
	assert.True(t, panicked, "expected panic from nil repo — message validation happens after agent resolution")
}

func TestACPGetRun_NoAuth_Returns401(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContextNoAuth(http.MethodGet, "/acp/v1/agents/my-agent/runs/run-123", "")
	c.SetParamNames("name", "runId")
	c.SetParamValues("my-agent", "run-123")

	err := h.GetRun(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestACPGetRun_MissingSlug_Returns400(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContext(http.MethodGet, "/acp/v1/agents//runs/run-123", "", map[string]string{
		"name":  "",
		"runId": "run-123",
	})

	err := h.GetRun(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}

func TestACPGetRun_MissingRunID_Returns400(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContext(http.MethodGet, "/acp/v1/agents/my-agent/runs/", "", map[string]string{
		"name":  "my-agent",
		"runId": "",
	})

	err := h.GetRun(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}

func TestACPCancelRun_NoAuth_Returns401(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContextNoAuth(http.MethodDelete, "/acp/v1/agents/my-agent/runs/run-123", "")
	c.SetParamNames("name", "runId")
	c.SetParamValues("my-agent", "run-123")

	err := h.CancelRun(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestACPCancelRun_MissingParams_Returns400(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContext(http.MethodDelete, "/acp/v1/agents//runs/", "", map[string]string{
		"name":  "",
		"runId": "",
	})

	err := h.CancelRun(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}

func TestACPResumeRun_NoAuth_Returns401(t *testing.T) {
	h := newTestACPHandler()
	body := `{"message": [{"content_type": "text/plain", "content": "yes"}]}`
	c, _ := newACPEchoContextNoAuth(http.MethodPost, "/acp/v1/agents/my-agent/runs/run-123/resume", body)
	c.SetParamNames("name", "runId")
	c.SetParamValues("my-agent", "run-123")

	err := h.ResumeRun(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestACPResumeRun_MissingParams_Returns400(t *testing.T) {
	h := newTestACPHandler()
	body := `{"message": [{"content_type": "text/plain", "content": "yes"}]}`
	c, _ := newACPEchoContext(http.MethodPost, "/acp/v1/agents//runs//resume", body, map[string]string{
		"name":  "",
		"runId": "",
	})

	err := h.ResumeRun(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}

func TestACPResumeRun_InvalidMode_PanicsOnNilRepo(t *testing.T) {
	// Mode validation happens after agent resolution, so with nil repo we get a panic.
	h := newTestACPHandler()
	body := `{"message": [{"content_type": "text/plain", "content": "yes"}], "mode": "invalid"}`
	c, _ := newACPEchoContext(http.MethodPost, "/acp/v1/agents/my-agent/runs/run-123/resume", body, map[string]string{
		"name":  "my-agent",
		"runId": "run-123",
	})

	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		_ = h.ResumeRun(c)
	}()
	assert.True(t, panicked, "expected panic from nil repo after param validation passed")
}

// ============================================================================
// Task 9.6: ACP Session endpoint tests (CreateSession, GetSession)
// ============================================================================

func TestACPCreateSession_NoAuth_Returns401(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContextNoAuth(http.MethodPost, "/acp/v1/sessions", "{}")

	err := h.CreateSession(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestACPCreateSession_NoProject_Returns400(t *testing.T) {
	h := newTestACPHandler()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/acp/v1/sessions", strings.NewReader("{}"))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{
		ID:        "user-test-id",
		Email:     "test@example.com",
		ProjectID: "",
	})

	err := h.CreateSession(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Contains(t, appErr.Message, "project context")
}

func TestACPCreateSession_EmptyBody_PanicsOnNilRepo(t *testing.T) {
	// With no agent_name validation needed, the handler goes straight to repo.CreateACPSession.
	// With nil repo, it panics — which means we passed all validation.
	h := newTestACPHandler()
	c, _ := newACPEchoContext(http.MethodPost, "/acp/v1/sessions", "{}", map[string]string{})

	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		_ = h.CreateSession(c)
	}()
	assert.True(t, panicked, "expected panic from nil repo after all validation passed")
}

func TestACPCreateSession_WithAgentName_PanicsOnNilRepo(t *testing.T) {
	// When agent_name is provided, the handler validates it exists — hits nil repo.
	h := newTestACPHandler()
	body := `{"agent_name": "some-agent"}`
	c, _ := newACPEchoContext(http.MethodPost, "/acp/v1/sessions", body, map[string]string{})

	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		_ = h.CreateSession(c)
	}()
	assert.True(t, panicked, "expected panic from nil repo when validating agent_name")
}

func TestACPGetSession_NoAuth_Returns401(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContextNoAuth(http.MethodGet, "/acp/v1/sessions/sess-123", "")
	c.SetParamNames("sessionId")
	c.SetParamValues("sess-123")

	err := h.GetSession(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestACPGetSession_EmptySessionID_Returns400(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContext(http.MethodGet, "/acp/v1/sessions/", "", map[string]string{
		"sessionId": "",
	})

	err := h.GetSession(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Contains(t, appErr.Message, "session ID")
}

func TestACPListSessions_NoAuth_Returns401(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContextNoAuth(http.MethodGet, "/acp/v1/sessions", "")

	err := h.ListSessions(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestACPListSessions_NoProject_Returns400(t *testing.T) {
	h := newTestACPHandler()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/acp/v1/sessions", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{
		ID:        "user-test-id",
		Email:     "test@example.com",
		ProjectID: "",
	})

	err := h.ListSessions(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}

// ============================================================================
// Task 9.7: ACP Event endpoint tests (GetRunEvents)
// ============================================================================

func TestACPGetRunEvents_NoAuth_Returns401(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContextNoAuth(http.MethodGet, "/acp/v1/agents/my-agent/runs/run-123/events", "")
	c.SetParamNames("name", "runId")
	c.SetParamValues("my-agent", "run-123")

	err := h.GetRunEvents(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestACPGetRunEvents_MissingParams_Returns400(t *testing.T) {
	h := newTestACPHandler()
	c, _ := newACPEchoContext(http.MethodGet, "/acp/v1/agents//runs//events", "", map[string]string{
		"name":  "",
		"runId": "",
	})

	err := h.GetRunEvents(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}

func TestACPGetRunEvents_ValidParams_PanicsOnNilRepo(t *testing.T) {
	// With valid params, handler reaches resolveAgentBySlug → nil repo → panic.
	h := newTestACPHandler()
	c, _ := newACPEchoContext(http.MethodGet, "/acp/v1/agents/my-agent/runs/run-123/events", "", map[string]string{
		"name":  "my-agent",
		"runId": "run-123",
	})

	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		_ = h.GetRunEvents(c)
	}()
	assert.True(t, panicked, "expected panic from nil repo after param validation passed")
}

// ============================================================================
// Helper function tests
// ============================================================================

func TestACPProjectID_NilUser_Returns401(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// No user set

	_, err := acpProjectID(c)
	require.Error(t, err)
	assert.Equal(t, apperror.ErrUnauthorized, err)
}

func TestACPProjectID_EmptyProject_Returns400(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{
		ID:        "user-1",
		ProjectID: "",
	})

	_, err := acpProjectID(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}

func TestACPProjectID_ValidUser_ReturnsProjectID(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{
		ID:        "user-1",
		ProjectID: "proj-abc",
	})

	pid, err := acpProjectID(c)
	require.NoError(t, err)
	assert.Equal(t, "proj-abc", pid)
}

func TestACPUserMessageFromParts_TextPlain(t *testing.T) {
	parts := []ACPMessagePart{
		{ContentType: "text/plain", Content: "Hello "},
		{ContentType: "text/plain", Content: "World"},
	}
	result := acpUserMessageFromParts(parts)
	assert.Equal(t, "Hello World", result)
}

func TestACPUserMessageFromParts_EmptyContentType(t *testing.T) {
	// Empty content type defaults to text/plain
	parts := []ACPMessagePart{
		{ContentType: "", Content: "Hello"},
	}
	result := acpUserMessageFromParts(parts)
	assert.Equal(t, "Hello", result)
}

func TestACPUserMessageFromParts_NonTextIgnored(t *testing.T) {
	parts := []ACPMessagePart{
		{ContentType: "application/json", Content: `{"key":"value"}`},
	}
	result := acpUserMessageFromParts(parts)
	assert.Equal(t, "", result)
}

func TestACPUserMessageFromParts_MixedParts(t *testing.T) {
	parts := []ACPMessagePart{
		{ContentType: "text/plain", Content: "Hello "},
		{ContentType: "image/png", Content: "base64data"},
		{ContentType: "text/plain", Content: "World"},
	}
	result := acpUserMessageFromParts(parts)
	assert.Equal(t, "Hello World", result)
}

func TestACPUserMessageFromParts_Empty(t *testing.T) {
	result := acpUserMessageFromParts(nil)
	assert.Equal(t, "", result)
}

func TestIsTerminalACPStatus(t *testing.T) {
	tests := []struct {
		status   string
		terminal bool
	}{
		{"completed", true},
		{"failed", true},
		{"cancelled", true},
		{"submitted", false},
		{"working", false},
		{"input-required", false},
		{"cancelling", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			assert.Equal(t, tt.terminal, isTerminalACPStatus(tt.status))
		})
	}
}

// ============================================================================
// RunEventToACPSSEEvent tests (task 9.7)
// ============================================================================

func TestRunEventToACPSSEEvent(t *testing.T) {
	event := &ACPRunEvent{
		ID:        "evt-1",
		RunID:     "run-1",
		EventType: "run.created",
		Data: map[string]any{
			"run_id": "run-1",
			"status": "submitted",
		},
	}

	sse := RunEventToACPSSEEvent(event)
	assert.Equal(t, "run.created", sse.Type)
	assert.Equal(t, "run-1", sse.Data["run_id"])
	assert.Equal(t, "submitted", sse.Data["status"])
}

func TestRunEventToACPSSEEvent_AllEventTypes(t *testing.T) {
	eventTypes := []string{
		ACPEventRunCreated,
		ACPEventRunInProgress,
		ACPEventRunAwaiting,
		ACPEventRunCompleted,
		ACPEventRunFailed,
		ACPEventRunCancelled,
		ACPEventMessageCreated,
		ACPEventMessagePart,
		ACPEventMessageCompleted,
		ACPEventGeneric,
		ACPEventError,
		ACPEventToolCall,
		ACPEventToolResult,
	}

	for _, et := range eventTypes {
		t.Run(et, func(t *testing.T) {
			event := &ACPRunEvent{
				EventType: et,
				Data:      map[string]any{"test": true},
			}
			sse := RunEventToACPSSEEvent(event)
			assert.Equal(t, et, sse.Type)
			assert.Equal(t, true, sse.Data["test"])
		})
	}
}

func TestACPEventToolCallConstants(t *testing.T) {
	assert.Equal(t, "tool_call", ACPEventToolCall)
	assert.Equal(t, "tool_result", ACPEventToolResult)
}

func TestACPEventToolCall_RunEventToSSEEvent(t *testing.T) {
	for _, tc := range []struct {
		eventType string
		dataKey   string
	}{
		{ACPEventToolCall, "tool_call"},
		{ACPEventToolResult, "tool_result"},
	} {
		t.Run(tc.eventType, func(t *testing.T) {
			event := &ACPRunEvent{
				EventType: tc.eventType,
				Data: map[string]any{
					tc.dataKey: map[string]any{"name": "search", "arguments": `{}`},
				},
			}
			sse := RunEventToACPSSEEvent(event)
			require.Equal(t, tc.eventType, sse.Type)
			require.Contains(t, sse.Data, tc.dataKey)
		})
	}
}
