package mcp

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

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func agentEndpointService(handler AgentToolHandler) *Service {
	store := newFakeAgentShareStore()
	store.byID["s1"] = &AgentMCPShare{ID: "s1", ProjectID: "proj-1", AgentID: agentA, Name: "Team A", TokenID: "tok-1"}
	if handler == nil {
		handler = &stubAgentHandler{runReply: "the reply"}
	}
	return newAgentShareService(store, &fakeTokenSvc{}, &fakeAgentDir{
		agents: []AgentRef{{ID: agentA, Name: "Alpha", Enabled: true}, {ID: agentB, Name: "Beta", Enabled: true}},
	}, handler)
}

func agentEndpointRequest(t *testing.T, method string, params any, apiTokenID string) (*echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: method}
	if params != nil {
		req.Params = mustJSON(t, params)
	}
	body := string(mustJSON(t, req))
	user := &auth.AuthUser{ID: "user-1", ProjectID: "proj-1", APITokenID: apiTokenID, Scopes: []string{AgentCallScope}}
	return newJSONContext(t, http.MethodPost, body, user, []string{"agentId"}, []string{agentA})
}

// agentShareUser returns an authenticated caller holding the agent-share marker.
func agentShareUser() *auth.AuthUser {
	return &auth.AuthUser{ID: "user-1", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{AgentCallScope}}
}

func TestAgentEndpointListsExactlyOneTool(t *testing.T) {
	ep := NewAgentEndpointHandler(agentEndpointService(nil), slog.Default())
	c, rec := agentEndpointRequest(t, "tools/list", nil, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	tools, _ := result["tools"].([]any)
	require.Len(t, tools, 1)
	tool := tools[0].(map[string]any)
	assert.Equal(t, agentCallToolName, tool["name"])
	assert.Contains(t, tool["description"], "Alpha")

	// No project tool surfaces.
	assert.NotContains(t, rec.Body.String(), "entity-search")
	assert.NotContains(t, rec.Body.String(), "schema-list")
}

func TestAgentEndpointUnknownToolRejected(t *testing.T) {
	ep := NewAgentEndpointHandler(agentEndpointService(nil), slog.Default())
	c, rec := agentEndpointRequest(t, "tools/call", ToolsCallParams{Name: "schema-list", Arguments: map[string]any{"message": "hi"}}, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))

	var resp Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeMethodNotFound, resp.Error.Code)
}

func TestAgentEndpointMissingMessageRejected(t *testing.T) {
	handler := &stubAgentHandler{runReply: "unused"}
	ep := NewAgentEndpointHandler(agentEndpointService(handler), slog.Default())
	c, rec := agentEndpointRequest(t, "tools/call", ToolsCallParams{Name: agentCallToolName, Arguments: map[string]any{}}, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))

	var resp Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeInvalidParams, resp.Error.Code)
	assert.False(t, handler.runCalled, "no run should start")
}

func TestAgentEndpointBlankMessageRejected(t *testing.T) {
	handler := &stubAgentHandler{runReply: "unused"}
	ep := NewAgentEndpointHandler(agentEndpointService(handler), slog.Default())
	c, _ := agentEndpointRequest(t, "tools/call", ToolsCallParams{Name: agentCallToolName, Arguments: map[string]any{"message": "   "}}, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	assert.False(t, handler.runCalled)
}

func TestAgentEndpointCallReturnsReply(t *testing.T) {
	handler := &stubAgentHandler{runReply: "hello world"}
	ep := NewAgentEndpointHandler(agentEndpointService(handler), slog.Default())
	c, rec := agentEndpointRequest(t, "tools/call", ToolsCallParams{Name: agentCallToolName, Arguments: map[string]any{"message": "ping"}}, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	content, _ := result["content"].([]any)
	require.Len(t, content, 1)
	assert.Equal(t, "hello world", content[0].(map[string]any)["text"])
	assert.True(t, handler.runCalled)
}

func TestAgentEndpointUnboundCredentialForbidden(t *testing.T) {
	ep := NewAgentEndpointHandler(agentEndpointService(nil), slog.Default())
	// Valid user but no share bound to this token.
	c, rec := agentEndpointRequest(t, "tools/list", nil, "some-other-token")
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAgentEndpointDifferentAgentForbidden(t *testing.T) {
	ep := NewAgentEndpointHandler(agentEndpointService(nil), slog.Default())
	user := &auth.AuthUser{ID: "user-1", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{AgentCallScope}}
	c, rec := newJSONContext(t, http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, user, []string{"agentId"}, []string{agentB})
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAgentEndpointInitialize(t *testing.T) {
	ep := NewAgentEndpointHandler(agentEndpointService(nil), slog.Default())
	params := InitializeParams{ProtocolVersion: "2025-11-25", ClientInfo: ClientInfo{Name: "test", Version: "1"}}
	c, rec := agentEndpointRequest(t, "initialize", params, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))

	var resp Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
}

func TestAgentEndpointPausedReturnsErrorResult(t *testing.T) {
	handler := &stubAgentHandler{runErr: &AgentRunError{Kind: AgentRunErrorPaused, Message: "awaiting input", Question: "Which one?"}}
	ep := NewAgentEndpointHandler(agentEndpointService(handler), slog.Default())
	c, rec := agentEndpointRequest(t, "tools/call", ToolsCallParams{Name: agentCallToolName, Arguments: map[string]any{"message": "hi"}}, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))

	var resp Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Nil(t, resp.Error, "tool failures are results, not transport errors")
	result, _ := resp.Result.(map[string]any)
	assert.True(t, result["isError"].(bool))
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	assert.Contains(t, text, "input_required")
	assert.Contains(t, text, "Which one?")
}

func TestAgentEndpointRunFailureReturnsErrorResult(t *testing.T) {
	handler := &stubAgentHandler{runErr: &AgentRunError{Kind: AgentRunErrorFailed, Message: "boom"}}
	ep := NewAgentEndpointHandler(agentEndpointService(handler), slog.Default())
	c, rec := agentEndpointRequest(t, "tools/call", ToolsCallParams{Name: agentCallToolName, Arguments: map[string]any{"message": "hi"}}, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))

	var resp Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	result, _ := resp.Result.(map[string]any)
	assert.True(t, result["isError"].(bool))
	assert.Contains(t, result["content"].([]any)[0].(map[string]any)["text"].(string), "boom")
}

func TestAgentEndpointUnknownMethod(t *testing.T) {
	ep := NewAgentEndpointHandler(agentEndpointService(nil), slog.Default())
	c, rec := agentEndpointRequest(t, "resources/list", nil, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))

	var resp Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeMethodNotFound, resp.Error.Code)
}

// TestAgentEndpointRouteRequiresAuth verifies the route is registered behind
// RequireAuth: an unauthenticated request is rejected with HTTP 401 before any
// share resolution happens.
func TestAgentEndpointRouteRequiresAuth(t *testing.T) {
	e := echo.New()
	svc := &Service{}
	h := NewHandler(svc, slog.Default(), nil)
	sse := NewSSEHandler(svc, h, slog.Default())
	stream := NewStreamableHTTPHandler(svc, slog.Default())
	ep := NewAgentEndpointHandler(svc, slog.Default())
	mw := auth.NewMiddleware(auth.MiddlewareParams{Cfg: &config.Config{}, Log: slog.Default()})
	RegisterRoutes(e, h, sse, stream, ep, mw)

	req := httptest.NewRequest(http.MethodPost, "/api/mcp/agents/"+agentA, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAgentEndpointTwoCallsAreIndependent(t *testing.T) {
	handler := &stubAgentHandler{runReply: "r"}
	ep := NewAgentEndpointHandler(agentEndpointService(handler), slog.Default())
	for i := 0; i < 2; i++ {
		c, _ := agentEndpointRequest(t, "tools/call", ToolsCallParams{Name: agentCallToolName, Arguments: map[string]any{"message": "ping"}}, "tok-1")
		require.NoError(t, ep.HandleAgentEndpoint(*c))
	}
	assert.Equal(t, 2, handler.runCount, "each call starts a new run")
}

func TestAgentEndpointUnavailableAgentReturnsErrorResult(t *testing.T) {
	handler := &stubAgentHandler{runErr: &AgentRunError{Kind: AgentRunErrorUnavailable, Message: "agent not found"}}
	ep := NewAgentEndpointHandler(agentEndpointService(handler), slog.Default())
	c, rec := agentEndpointRequest(t, "tools/call", ToolsCallParams{Name: agentCallToolName, Arguments: map[string]any{"message": "hi"}}, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))

	var resp Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	result, _ := resp.Result.(map[string]any)
	assert.True(t, result["isError"].(bool))
	assert.Contains(t, result["content"].([]any)[0].(map[string]any)["text"].(string), "agent_unavailable")
}

// ============================================================================
// C1: agent-share credentials are only valid at the per-agent endpoint
// ============================================================================

// TestAgentEndpointRequiresAgentCallScope verifies a normal project credential
// (no marker scope) is rejected at the per-agent endpoint.
func TestAgentEndpointRequiresAgentCallScope(t *testing.T) {
	ep := NewAgentEndpointHandler(agentEndpointService(&stubAgentHandler{runReply: "nope"}), slog.Default())
	user := &auth.AuthUser{ID: "user-1", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{"data:read", "agents:read"}}
	c, rec := newJSONContext(t, http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, user, []string{"agentId"}, []string{agentA})
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestAgentEndpointAcceptsAgentShareToken is the positive half of C1: a token
// carrying the marker scope IS accepted at the per-agent endpoint.
func TestAgentEndpointAcceptsAgentShareToken(t *testing.T) {
	ep := NewAgentEndpointHandler(agentEndpointService(&stubAgentHandler{runReply: "hi"}), slog.Default())
	c, rec := agentEndpointRequest(t, "tools/list", nil, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, jsonError(rec))
}

// TestProjectRPCEndpointRejectsAgentShareToken verifies the legacy project
// JSON-RPC transport rejects a marker-scoped credential with 403.
func TestProjectRPCEndpointRejectsAgentShareToken(t *testing.T) {
	h := NewHandler(&Service{}, slog.Default(), nil)
	c, rec := newJSONContext(t, http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, agentShareUser(), nil, nil)
	require.NoError(t, h.HandleRPC(*c))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestProjectRPCEndpointAllowsNormalToken verifies the rejection is specific to
// the marker: a normal project token is not rejected with 403 (it may still
// fail later for other reasons, e.g. no session).
func TestProjectRPCEndpointAllowsNormalToken(t *testing.T) {
	h := NewHandler(&Service{}, slog.Default(), nil)
	user := &auth.AuthUser{ID: "user-1", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{"data:read"}}
	c, rec := newJSONContext(t, http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, user, nil, nil)
	require.NoError(t, h.HandleRPC(*c))
	assert.NotEqual(t, http.StatusForbidden, rec.Code)
}

// TestStreamableEndpointRejectsAgentShareToken verifies the project Streamable
// HTTP transport rejects a marker-scoped credential with 403.
func TestStreamableEndpointRejectsAgentShareToken(t *testing.T) {
	h := NewStreamableHTTPHandler(&Service{}, slog.Default())
	c, rec := newJSONContext(t, http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, agentShareUser(), nil, nil)
	require.NoError(t, h.HandleUnifiedEndpoint(*c))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestSSEMessageRejectsAgentShareToken verifies the project SSE message
// transport rejects a marker-scoped credential.
func TestSSEMessageRejectsAgentShareToken(t *testing.T) {
	svc := &Service{}
	h := NewSSEHandler(svc, NewHandler(svc, slog.Default(), nil), slog.Default())
	c, _ := newJSONContext(t, http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, agentShareUser(), []string{"projectId"}, []string{"11111111-1111-1111-1111-111111111111"})
	err := h.HandleSSEMessage(*c)
	assertAppError(t, err, http.StatusForbidden)
}

// TestSSEConnectRejectsAgentShareToken verifies the project SSE connect
// transport rejects a marker-scoped credential.
func TestSSEConnectRejectsAgentShareToken(t *testing.T) {
	svc := &Service{}
	h := NewSSEHandler(svc, NewHandler(svc, slog.Default(), nil), slog.Default())
	c, _ := newJSONContext(t, http.MethodGet, "", agentShareUser(), []string{"projectId"}, []string{"11111111-1111-1111-1111-111111111111"})
	err := h.HandleSSEConnect(*c)
	assertAppError(t, err, http.StatusForbidden)
}

// jsonError decodes a JSON-RPC error from a recorded response, if any.
func jsonError(rec *httptest.ResponseRecorder) *ErrorObject {
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		return nil
	}
	return resp.Error
}
