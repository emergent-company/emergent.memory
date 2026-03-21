package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	assert.Equal(t, false, m["truncated"])
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
		fmt.Fprint(w, buildSseLines(`{"type":"token","token":"ok"}`))
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
		fmt.Fprint(w, buildSseLines(`{"type":"token","token":"ok"}`))
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
