package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sseServer starts a fake SSE endpoint on a random port that serves the given
// SSE body verbatim. It returns the server and the port it is listening on.
// The caller should defer server.Close().
func sseServer(t *testing.T, handler http.HandlerFunc) (ts *httptest.Server, port int) {
	t.Helper()
	ts = httptest.NewUnstartedServer(handler)
	ts.Start()
	addr := ts.Listener.Addr().(*net.TCPAddr)
	return ts, addr.Port
}

// buildSseLines assembles an SSE response body from a slice of data payloads.
func buildSseLines(payloads ...string) string {
	var sb strings.Builder
	for _, p := range payloads {
		sb.WriteString("data: ")
		sb.WriteString(p)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// parseResultMap parses the JSON in ToolResult.Content[0].Text into a map.
func parseResultMap(t *testing.T, result *ToolResult) map[string]any {
	t.Helper()
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	var m map[string]any
	err := json.Unmarshal([]byte(result.Content[0].Text), &m)
	require.NoError(t, err, "ToolResult content is not valid JSON: %s", result.Content[0].Text)
	return m
}

// =============================================================================
// Missing / empty question
// =============================================================================

func TestExecuteQueryKnowledge_MissingQuestion(t *testing.T) {
	svc := &Service{}
	_, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'question' is required")
}

func TestExecuteQueryKnowledge_EmptyQuestion(t *testing.T) {
	svc := &Service{}
	_, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{"question": ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'question' is required")
}

// =============================================================================
// Token accumulation
// =============================================================================

func TestExecuteQueryKnowledge_CollectsTokens(t *testing.T) {
	body := buildSseLines(
		`{"type":"token","token":"Hello"}`,
		`{"type":"token","token":" world"}`,
		`[DONE]`,
	)
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	})
	defer ts.Close()

	svc := &Service{serverPort: port}
	result, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{"question": "q"})
	require.NoError(t, err)

	m := parseResultMap(t, result)
	assert.Equal(t, "Hello world", m["answer"])
	_, hasTruncated := m["truncated"]
	assert.False(t, hasTruncated, "success payload must no longer carry the removed truncated field")
	_, hasSessionID := m["session_id"]
	assert.False(t, hasSessionID, "session_id should not be present when meta event is absent")
}

// =============================================================================
// session_id from meta event
// =============================================================================

func TestExecuteQueryKnowledge_SessionIDFromMeta(t *testing.T) {
	body := buildSseLines(
		`{"type":"token","token":"Answer"}`,
		`{"type":"meta","conversationId":"sess-abc123"}`,
		`[DONE]`,
	)
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	})
	defer ts.Close()

	svc := &Service{serverPort: port}
	result, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{"question": "q"})
	require.NoError(t, err)

	m := parseResultMap(t, result)
	assert.Equal(t, "Answer", m["answer"])
	assert.Equal(t, "sess-abc123", m["session_id"])
}

// =============================================================================
// Error event
// =============================================================================

func TestExecuteQueryKnowledge_ErrorEvent(t *testing.T) {
	body := buildSseLines(`{"type":"error","error":"something went wrong"}`)
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	})
	defer ts.Close()

	svc := &Service{serverPort: port}
	_, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{"question": "q"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "something went wrong")
}

// =============================================================================
// session_id forwarded as conversation_id in request body
// =============================================================================

func TestExecuteQueryKnowledge_ForwardsSessionIDInRequest(t *testing.T) {
	var receivedBody map[string]any
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &receivedBody)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, buildSseLines(`{"type":"token","token":"ok"}`, `[DONE]`))
	})
	defer ts.Close()

	svc := &Service{serverPort: port}
	_, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{
		"question":   "q",
		"session_id": "my-session-42",
	})
	require.NoError(t, err)
	assert.Equal(t, "my-session-42", receivedBody["conversation_id"])
}

// =============================================================================
// mode forwarded in request body
// =============================================================================

func TestExecuteQueryKnowledge_ForwardsModeInRequest(t *testing.T) {
	var receivedBody map[string]any
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &receivedBody)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, buildSseLines(`{"type":"token","token":"ok"}`, `[DONE]`))
	})
	defer ts.Close()

	svc := &Service{serverPort: port}
	_, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{
		"question": "q",
		"mode":     "hybrid",
	})
	require.NoError(t, err)
	assert.Equal(t, "hybrid", receivedBody["mode"])
}

// =============================================================================
// HTTP 4xx response
// =============================================================================

func TestExecuteQueryKnowledge_HTTP4xx(t *testing.T) {
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity) // 422
	})
	defer ts.Close()

	svc := &Service{serverPort: port}
	_, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{"question": "q"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "422")
}

// =============================================================================
// Unknown SSE event types are ignored
// =============================================================================

func TestExecuteQueryKnowledge_IgnoresUnknownSSETypes(t *testing.T) {
	body := buildSseLines(
		`{"type":"unknown","data":"ignored"}`,
		`{"type":"token","token":"valid"}`,
		`{"type":"ping"}`,
		`[DONE]`,
	)
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	})
	defer ts.Close()

	svc := &Service{serverPort: port}
	result, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{"question": "q"})
	require.NoError(t, err)

	m := parseResultMap(t, result)
	assert.Equal(t, "valid", m["answer"])
}

// =============================================================================
// Non-data SSE lines are ignored
// =============================================================================

func TestExecuteQueryKnowledge_IgnoresNonDataLines(t *testing.T) {
	// Lines without "data:" prefix (comments, event:, id:) must be skipped.
	raw := "event: start\n\ndata: {\"type\":\"token\",\"token\":\"hi\"}\n\nid: 1\n\ndata: [DONE]\n\n"
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, raw)
	})
	defer ts.Close()

	svc := &Service{serverPort: port}
	result, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{"question": "q"})
	require.NoError(t, err)

	m := parseResultMap(t, result)
	assert.Equal(t, "hi", m["answer"])
}

// =============================================================================
// HTTP 403 with JSON error envelope (missing scopes surfaced)
// =============================================================================

// missingScopeBody mirrors the server's permission-denied error envelope:
// {"error":{"code":"forbidden","message":"...","details":{"missing":["chat:use"]}}}
const missingScopeBody = `{"error":{"code":"forbidden","message":"Insufficient permissions","details":{"missing":["chat:use"]}}}`

// missingScopeServer starts a fake server that answers every request with the
// missing-scope 403 envelope.
func missingScopeServer(t *testing.T) (ts *httptest.Server, port int) {
	t.Helper()
	return sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, missingScopeBody)
	})
}

func TestExecuteQueryKnowledge_403MissingScopeSurfaced(t *testing.T) {
	ts, port := missingScopeServer(t)
	defer ts.Close()

	svc := &Service{serverPort: port}
	_, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{"question": "q"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "forbidden")
	assert.Contains(t, err.Error(), "Insufficient permissions")
	assert.Contains(t, err.Error(), "chat:use")
	assert.Contains(t, err.Error(), "missing required scope")
	// The parsed envelope replaces the bare "server returned" fallback.
	assert.NotContains(t, err.Error(), "server returned")
}

func TestExecuteRemember_403MissingScopeSurfaced(t *testing.T) {
	ts, port := missingScopeServer(t)
	defer ts.Close()

	svc := &Service{serverPort: port}
	_, err := svc.executeRemember(context.Background(), "proj-id", map[string]any{"message": "remember me"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remember: 403 forbidden")
	assert.Contains(t, err.Error(), "Insufficient permissions")
	assert.Contains(t, err.Error(), "chat:use")
	assert.Contains(t, err.Error(), "missing required scope")
}

func TestExecuteForget_403MissingScopeSurfaced(t *testing.T) {
	ts, port := missingScopeServer(t)
	defer ts.Close()

	svc := &Service{serverPort: port}
	_, err := svc.executeForget(context.Background(), "proj-id", map[string]any{"message": "forget me"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forget: 403 forbidden")
	assert.Contains(t, err.Error(), "Insufficient permissions")
	assert.Contains(t, err.Error(), "chat:use")
	assert.Contains(t, err.Error(), "missing required scope")
}

// =============================================================================
// Timeout / cancellation surface as errors, never as empty successes (#1187)
// =============================================================================

// A caller-supplied deadline is not the tool's internal budget: it must be
// reported as a caller cancellation, not mis-attributed to queryKnowledgeTimeout.
func TestExecuteQueryKnowledge_CallerDeadlineReportedAsCaller(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	svc := &Service{serverPort: 1} // never dialed: the context is already expired
	_, err := svc.executeQueryKnowledge(ctx, "proj-id", map[string]any{"question": "q"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "caller deadline")
	assert.NotContains(t, err.Error(), "timed out after")
	assert.NotContains(t, err.Error(), "60s", "must not resurrect the stale 60s literal")
}

// queryKnowledgeCtxError distinguishes the tool budget, a caller deadline, and a
// plain cancellation without needing to wait out the real 120s budget.
func TestQueryKnowledgeCtxError_AttributesCause(t *testing.T) {
	t.Run("internal budget when caller has no deadline", func(t *testing.T) {
		err := queryKnowledgeCtxError(context.Background(), time.Now().Add(-time.Second), context.DeadlineExceeded)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timed out after")
		assert.Contains(t, err.Error(), queryKnowledgeTimeout.String())
	})

	t.Run("caller deadline sooner than the tool budget", func(t *testing.T) {
		callerCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
		defer cancel()
		err := queryKnowledgeCtxError(callerCtx, time.Now().Add(queryKnowledgeTimeout), context.DeadlineExceeded)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "caller deadline")
		assert.NotContains(t, err.Error(), "timed out after")
	})

	t.Run("plain cancellation is propagated", func(t *testing.T) {
		err := queryKnowledgeCtxError(context.Background(), time.Now().Add(queryKnowledgeTimeout), context.Canceled)
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))
		assert.Contains(t, err.Error(), "cancelled")
		assert.NotContains(t, err.Error(), "timed out")
	})
}

func TestExecuteQueryKnowledge_CancelledContextReturnsCancelledError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svc := &Service{serverPort: 1}
	_, err := svc.executeQueryKnowledge(ctx, "proj-id", map[string]any{"question": "q"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Contains(t, err.Error(), "cancelled")
	assert.NotContains(t, err.Error(), "timed out")
}

func TestExecuteQueryKnowledge_DeadlineDuringStreamReturnsError(t *testing.T) {
	// The server emits a token then stalls. When the caller's deadline lands
	// mid-stream the handler must fail rather than hand back the partial answer
	// as success, and it must attribute the expiry to the caller.
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, buildSseLines(`{"type":"token","token":"partial answer"}`))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	svc := &Service{serverPort: port}
	result, err := svc.executeQueryKnowledge(ctx, "proj-id", map[string]any{"question": "q"})
	require.Error(t, err)
	assert.Nil(t, result, "a timed-out query must not return a successful result")
	assert.Contains(t, err.Error(), "caller deadline")
}

// A stream that ends without its terminal event (proxy/backend disconnect) must
// fail even though no context expired and part of an answer was received.
func TestExecuteQueryKnowledge_StreamWithoutTerminalEventReturnsError(t *testing.T) {
	body := buildSseLines(
		`{"type":"token","token":"partial answer"}`,
		`{"type":"meta","conversationId":"sess-x"}`,
	)
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	})
	defer ts.Close()

	svc := &Service{serverPort: port}
	result, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{"question": "q"})
	require.Error(t, err)
	assert.Nil(t, result, "an incomplete stream must not return a successful result")
	assert.Contains(t, err.Error(), "without a terminal event")
}

// The chat handler's real terminal marker is a `done` chunk (not only the
// `[DONE]` sentinel); both must be accepted as stream completion.
func TestExecuteQueryKnowledge_DoneEventIsTerminal(t *testing.T) {
	body := buildSseLines(
		`{"type":"token","token":"Answer"}`,
		`{"type":"done","runId":"run-1"}`,
	)
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	})
	defer ts.Close()

	svc := &Service{serverPort: port}
	result, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{"question": "q"})
	require.NoError(t, err)

	m := parseResultMap(t, result)
	assert.Equal(t, "Answer", m["answer"])
	assert.Equal(t, "run-1", m["run_id"])
}

// =============================================================================
// token-create scopes param advertises the authoritative scope list
// =============================================================================

func TestTokenCreateScopesParam_AdvertisesAllScopes(t *testing.T) {
	defs := tokenToolDefinitions()

	var scopesDesc string
	for _, def := range defs {
		if def.Name == "token-create" {
			scopesProp, ok := def.InputSchema.Properties["scopes"]
			require.True(t, ok, "token-create schema has no scopes param")
			scopesDesc = scopesProp.Description
			break
		}
	}
	require.NotEmpty(t, scopesDesc, "token-create scopes param description not found")

	// Drift guard: the advertised list must mirror the server's authoritative
	// scope set (e.g. chat:use must not be missing again).
	want := "Comma-separated list of scopes. Valid values: " + strings.Join(apitoken.ValidApiTokenScopes, ", ")
	assert.Equal(t, want, scopesDesc)
	assert.Contains(t, scopesDesc, "chat:use")
}
