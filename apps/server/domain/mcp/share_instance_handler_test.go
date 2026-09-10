package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
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

func newTestHandler(store shareInstanceStore, tok shareTokenService, dir agentDirectory) *Handler {
	svc := &Service{shareInstances: store, shareTokens: tok, agentDir: dir}
	return NewHandler(svc, slog.Default(), nil)
}

// newJSONContext builds an echo context with path params and an auth user.
func newJSONContext(t *testing.T, method, body string, user *auth.AuthUser, paramNames, paramValues []string) (*echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, "/", reader)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames(paramNames...)
	c.SetParamValues(paramValues...)
	if user != nil {
		c.Set(string(auth.UserContextKey), user)
	}
	return &c, rec
}

func adminUser() *auth.AuthUser {
	return &auth.AuthUser{ID: "user-1", ProjectID: "proj-1", APITokenID: "tok-1"}
}

func TestHandleCreateShareInstanceSuccess(t *testing.T) {
	store := newFakeShareStore()
	tok := &fakeTokenSvc{createToken: "emt_secret", createID: "tok-1"}
	h := newTestHandler(store, tok, &fakeAgentDir{})

	c, rec := newJSONContext(t, http.MethodPost, `{"name":"Team A","tools":["entity-search","schema-list"]}`, adminUser(), []string{"projectId"}, []string{"proj-1"})
	require.NoError(t, h.HandleCreateShareInstance(*c))
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateShareInstanceResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "emt_secret", resp.Token)
	assert.NotEmpty(t, resp.ID)
	assert.True(t, strings.HasPrefix(resp.MCPURL, "http"))
}

func TestHandleCreateShareInstanceForbidden(t *testing.T) {
	h := newTestHandler(newFakeShareStore(), &fakeTokenSvc{role: "project_user"}, &fakeAgentDir{})
	c, _ := newJSONContext(t, http.MethodPost, `{"name":"Team A"}`, adminUser(), []string{"projectId"}, []string{"proj-1"})
	err := h.HandleCreateShareInstance(*c)
	assertAppError(t, err, 403)
}

func TestHandleCreateShareInstanceBadBody(t *testing.T) {
	h := newTestHandler(newFakeShareStore(), &fakeTokenSvc{}, &fakeAgentDir{})
	c, _ := newJSONContext(t, http.MethodPost, `{not-json`, adminUser(), []string{"projectId"}, []string{"proj-1"})
	err := h.HandleCreateShareInstance(*c)
	assertAppError(t, err, 400)
}

func TestHandleCreateShareInstanceDuplicate(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "t0"}
	h := newTestHandler(store, &fakeTokenSvc{}, &fakeAgentDir{})
	c, _ := newJSONContext(t, http.MethodPost, `{"name":"Team A"}`, adminUser(), []string{"projectId"}, []string{"proj-1"})
	err := h.HandleCreateShareInstance(*c)
	assertAppError(t, err, 409)
}

func TestHandleCreateShareInstanceEmptyTools(t *testing.T) {
	h := newTestHandler(newFakeShareStore(), &fakeTokenSvc{}, &fakeAgentDir{})
	c, _ := newJSONContext(t, http.MethodPost, `{"name":"Team A","tools":[]}`, adminUser(), []string{"projectId"}, []string{"proj-1"})
	err := h.HandleCreateShareInstance(*c)
	assertAppError(t, err, 422)
}

func TestHandleCreateShareInstanceUnknownTool(t *testing.T) {
	h := newTestHandler(newFakeShareStore(), &fakeTokenSvc{}, &fakeAgentDir{})
	c, _ := newJSONContext(t, http.MethodPost, `{"name":"Team A","tools":["no-such-tool"]}`, adminUser(), []string{"projectId"}, []string{"proj-1"})
	err := h.HandleCreateShareInstance(*c)
	assertAppError(t, err, 422)
}

func TestHandleListAndGetOmitTokenSecret(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1", AllowedTools: []string{"entity-search"}}
	store.legacy = []*LegacyTokenRef{{ID: "legacy-1", Name: "MCP Read-Only Share — old"}}
	h := newTestHandler(store, &fakeTokenSvc{}, &fakeAgentDir{})

	c, rec := newJSONContext(t, http.MethodGet, "", adminUser(), []string{"projectId"}, []string{"proj-1"})
	require.NoError(t, h.HandleListShareInstances(*c))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "emt_")
	assert.NotContains(t, rec.Body.String(), `"token"`)

	var list ShareInstanceListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	assert.Equal(t, 2, list.Total)

	c2, rec2 := newJSONContext(t, http.MethodGet, "", adminUser(), []string{"projectId", "id"}, []string{"proj-1", "i1"})
	require.NoError(t, h.HandleGetShareInstance(*c2))
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.NotContains(t, rec2.Body.String(), "emt_")
}

func TestHandleGetNotFound(t *testing.T) {
	h := newTestHandler(newFakeShareStore(), &fakeTokenSvc{}, &fakeAgentDir{})
	c, _ := newJSONContext(t, http.MethodGet, "", adminUser(), []string{"projectId", "id"}, []string{"proj-1", "missing"})
	err := h.HandleGetShareInstance(*c)
	assertAppError(t, err, 404)
}

func TestHandleUpdateShareInstance(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1"}
	tok := &fakeTokenSvc{}
	h := newTestHandler(store, tok, &fakeAgentDir{})

	c, rec := newJSONContext(t, http.MethodPatch, `{"name":"Team B","tools":["entity-search"]}`, adminUser(), []string{"projectId", "id"}, []string{"proj-1", "i1"})
	require.NoError(t, h.HandleUpdateShareInstance(*c))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Team B", store.byID["i1"].Name)
	assert.ElementsMatch(t, []string{"graph:read", "projects:read"}, tok.updateScopes)
}

func TestHandleRevokeShareInstance(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1"}
	tok := &fakeTokenSvc{}
	h := newTestHandler(store, tok, &fakeAgentDir{})

	c, rec := newJSONContext(t, http.MethodDelete, "", adminUser(), []string{"projectId", "id"}, []string{"proj-1", "i1"})
	require.NoError(t, h.HandleRevokeShareInstance(*c))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotNil(t, store.byID["i1"].RevokedAt)
}

func TestHandleRotateShareInstance(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "old", AllowedTools: []string{"entity-search"}}
	tok := &fakeTokenSvc{regenerateID: "new", regenerateToken: "emt_new"}
	h := newTestHandler(store, tok, &fakeAgentDir{})

	c, rec := newJSONContext(t, http.MethodPost, "", adminUser(), []string{"projectId", "id"}, []string{"proj-1", "i1"})
	require.NoError(t, h.HandleRotateShareInstance(*c))
	assert.Equal(t, http.StatusOK, rec.Code)
	var resp CreateShareInstanceResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "emt_new", resp.Token)
	assert.Equal(t, "new", store.byID["i1"].TokenID)
}

func TestHandleListToolCatalog(t *testing.T) {
	h := newTestHandler(newFakeShareStore(), &fakeTokenSvc{}, &fakeAgentDir{})
	c, rec := newJSONContext(t, http.MethodGet, "", adminUser(), []string{"projectId"}, []string{"proj-1"})
	require.NoError(t, h.HandleListToolCatalog(*c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp CatalogResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Tools)
	for i := 1; i < len(resp.Tools); i++ {
		prev, cur := resp.Tools[i-1], resp.Tools[i]
		if prev.Category == cur.Category {
			assert.LessOrEqual(t, prev.Name, cur.Name)
		} else {
			assert.Less(t, prev.Category, cur.Category)
		}
	}
	// agent-only tools are never exposed
	for _, tool := range resp.Tools {
		assert.NotEmpty(t, tool.Name)
	}
}

func TestHandleListToolCatalogForbidden(t *testing.T) {
	h := newTestHandler(newFakeShareStore(), &fakeTokenSvc{role: "project_user"}, &fakeAgentDir{})
	c, _ := newJSONContext(t, http.MethodGet, "", adminUser(), []string{"projectId"}, []string{"proj-1"})
	err := h.HandleListToolCatalog(*c)
	assertAppError(t, err, 403)
}

// ============================================================================
// Transport enforcement
// ============================================================================

func TestHandlerToolsListAppliesInstanceAllowlist(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1",
		AllowedTools: []string{"entity-search"},
	}
	h := newTestHandler(store, &fakeTokenSvc{}, &fakeAgentDir{})
	token := "api-key"
	h.sessions[token] = &Session{Initialized: true, ProjectID: "proj-1"}

	user := &auth.AuthUser{ID: "u", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{"data:read", "schema:read"}}
	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/list"}

	e := echo.New()
	httpReq := httptest.NewRequest(http.MethodPost, "/api/mcp/rpc", nil)
	httpReq.Header.Set("X-API-Key", token)
	c := e.NewContext(httpReq, httptest.NewRecorder())
	c.Set(string(auth.UserContextKey), user)

	resp := h.handleToolsList(c, &req, user)
	require.Nil(t, resp.Error)
	result := resp.Result.(ToolsListResult)
	require.Len(t, result.Tools, 1)
	assert.Equal(t, "entity-search", result.Tools[0].Name)
}

func TestHandlerToolsCallRejectsNonAllowlisted(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1",
		AllowedTools: []string{"entity-search"},
	}
	h := newTestHandler(store, &fakeTokenSvc{}, &fakeAgentDir{})
	token := "api-key"
	h.sessions[token] = &Session{Initialized: true, ProjectID: "proj-1"}

	user := &auth.AuthUser{ID: "u", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{"schema:read"}}
	params, _ := json.Marshal(ToolsCallParams{Name: "schema-list"})
	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/call", Params: params}

	e := echo.New()
	httpReq := httptest.NewRequest(http.MethodPost, "/api/mcp/rpc", nil)
	httpReq.Header.Set("X-API-Key", token)
	c := e.NewContext(httpReq, httptest.NewRecorder())
	c.Set(string(auth.UserContextKey), user)

	resp := h.handleToolsCall(c, &req, user)
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeForbidden, resp.Error.Code)
}

func TestStreamableToolsListAppliesInstanceAllowlist(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1",
		AllowedTools: []string{"entity-search"},
	}
	h := NewStreamableHTTPHandler(&Service{shareInstances: store, shareTokens: &fakeTokenSvc{}, agentDir: &fakeAgentDir{}}, slog.Default())
	session := &MCPSession{ID: "s1", ProjectID: "proj-1", Scopes: []string{"data:read"}, Initialized: true}
	user := &auth.AuthUser{ID: "u", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{"data:read"}}

	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/mcp", nil), httptest.NewRecorder())
	resp := h.handleToolsList(c, &Request{JSONRPC: "2.0", ID: json.RawMessage(`1`)}, session, user)
	require.Nil(t, resp.Error)
	result := resp.Result.(ToolsListResult)
	require.Len(t, result.Tools, 1)
	assert.Equal(t, "entity-search", result.Tools[0].Name)
}

func TestStreamableToolsCallRejectsNonAllowlisted(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1",
		AllowedTools: []string{"entity-search"},
	}
	h := NewStreamableHTTPHandler(&Service{shareInstances: store, shareTokens: &fakeTokenSvc{}, agentDir: &fakeAgentDir{}}, slog.Default())
	session := &MCPSession{ID: "s1", ProjectID: "proj-1", Scopes: []string{"schema:read"}, Initialized: true}
	user := &auth.AuthUser{ID: "u", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{"schema:read"}}
	params, _ := json.Marshal(ToolsCallParams{Name: "schema-list"})

	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/mcp", nil), httptest.NewRecorder())
	resp := h.handleToolsCall(c, &Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Params: params}, session, user)
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeForbidden, resp.Error.Code)
}

func TestSSEToolsListAppliesInstanceAllowlist(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1",
		AllowedTools: []string{"entity-search"},
	}
	h := NewSSEHandler(&Service{shareInstances: store, shareTokens: &fakeTokenSvc{}, agentDir: &fakeAgentDir{}}, nil, slog.Default())
	user := &auth.AuthUser{ID: "u", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{"data:read"}}

	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/mcp/sse/proj-1/message", nil), httptest.NewRecorder())
	resp := h.handleToolsList(c, &Request{JSONRPC: "2.0", ID: json.RawMessage(`1`)}, "proj-1", user)
	require.Nil(t, resp.Error)
	result := resp.Result.(ToolsListResult)
	require.Len(t, result.Tools, 1)
	assert.Equal(t, "entity-search", result.Tools[0].Name)
}

func TestSSEToolsCallRejectsNonAllowlisted(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1",
		AllowedTools: []string{"entity-search"},
	}
	h := NewSSEHandler(&Service{shareInstances: store, shareTokens: &fakeTokenSvc{}, agentDir: &fakeAgentDir{}}, nil, slog.Default())
	user := &auth.AuthUser{ID: "u", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{"schema:read"}}
	params, _ := json.Marshal(ToolsCallParams{Name: "schema-list"})

	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/mcp/sse/proj-1/message", nil), httptest.NewRecorder())
	resp := h.handleToolsCall(c, &Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Params: params}, "proj-1", user)
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeForbidden, resp.Error.Code)
}

func assertAppError(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, status, appErr.HTTPStatus)
}

// ============================================================================
// Security hardening: scope guard, fail-closed resolution, content gating
// ============================================================================

// TestShareAdminScopeGuardRejectsShareToken proves the guard applied in
// routes.go to /mcp/shares*, /mcp/tools: a narrowly-scoped MCP share token
// (which never carries "admin") is forbidden, while a Zitadel/OAuth session and
// an admin-scoped API token still pass.
func TestShareAdminScopeGuardRejectsShareToken(t *testing.T) {
	m := &auth.Middleware{}
	guard := m.RequireAPITokenScopes("admin")
	e := echo.New()

	newCtx := func(user *auth.AuthUser) echo.Context {
		c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/projects/p/mcp/shares", nil), httptest.NewRecorder())
		c.Set(string(auth.UserContextKey), user)
		return c
	}

	t.Run("share token is forbidden", func(t *testing.T) {
		shareToken := &auth.AuthUser{
			ID: "u", APITokenID: "tok-1",
			Scopes: []string{"data:read", "schema:read", "agents:read", "projects:read", "chat:use"},
		}
		nextCalled := false
		err := guard(func(echo.Context) error { nextCalled = true; return nil })(newCtx(shareToken))
		require.Error(t, err)
		he, ok := err.(*echo.HTTPError)
		require.True(t, ok, "expected echo.HTTPError, got %T", err)
		assert.Equal(t, http.StatusForbidden, he.Code)
		assert.False(t, nextCalled, "share token must not reach the share-admin handler")
	})

	t.Run("admin-scoped token passes", func(t *testing.T) {
		adminToken := &auth.AuthUser{ID: "u", APITokenID: "tok-admin", Scopes: []string{"admin"}}
		nextCalled := false
		err := guard(func(echo.Context) error { nextCalled = true; return nil })(newCtx(adminToken))
		require.NoError(t, err)
		assert.True(t, nextCalled)
	})

	t.Run("oauth session bypasses token scope check", func(t *testing.T) {
		session := &auth.AuthUser{ID: "u", APITokenID: ""}
		nextCalled := false
		err := guard(func(echo.Context) error { nextCalled = true; return nil })(newCtx(session))
		require.NoError(t, err)
		assert.True(t, nextCalled)
	})
}

// TestHandlerResourcesAndPromptsDeniedForRestrictedInstance proves resources
// and prompts fail closed for a share instance with an active allowlist.
func TestHandlerResourcesAndPromptsDeniedForRestrictedInstance(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1",
		AllowedTools: []string{"entity-search"},
	}
	h := newTestHandler(store, &fakeTokenSvc{}, &fakeAgentDir{})
	user := &auth.AuthUser{ID: "u", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{"data:read", "schema:read"}}
	e := echo.New()

	for _, method := range []string{"resources/list", "resources/read", "prompts/list", "prompts/get"} {
		t.Run(method, func(t *testing.T) {
			req := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: method}
			c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/mcp/rpc", nil), httptest.NewRecorder())
			c.Set(string(auth.UserContextKey), user)
			resp := h.routeMethod(c, &req, user)
			require.NotNil(t, resp.Error, method)
			assert.Equal(t, ErrCodeForbidden, resp.Error.Code, method)
		})
	}

	// A null-allowlist (unrestricted/legacy) instance keeps access.
	store.byID["i2"] = &MCPShareInstance{ID: "i2", ProjectID: "proj-1", Name: "All", TokenID: "tok-2"}
	legacyUser := &auth.AuthUser{ID: "u", ProjectID: "proj-1", APITokenID: "tok-2", Scopes: []string{"data:read", "schema:read"}}
	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "resources/list"}
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/mcp/rpc", nil), httptest.NewRecorder())
	c.Set(string(auth.UserContextKey), legacyUser)
	resp := h.handleResourcesList(c, &req, legacyUser)
	require.Nil(t, resp.Error)
}

// TestHandlerToolsCallFailsClosedOnResolveError proves a storage error while
// resolving the allowlist denies the call instead of executing the tool.
func TestHandlerToolsCallFailsClosedOnResolveError(t *testing.T) {
	store := newFakeShareStore()
	store.getByTokenErr = errors.New("db down")
	h := newTestHandler(store, &fakeTokenSvc{}, &fakeAgentDir{})
	token := "api-key"
	h.sessions[token] = &Session{Initialized: true, ProjectID: "proj-1"}

	user := &auth.AuthUser{ID: "u", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{"data:read"}}
	params, _ := json.Marshal(ToolsCallParams{Name: "entity-search"})
	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/call", Params: params}

	e := echo.New()
	httpReq := httptest.NewRequest(http.MethodPost, "/api/mcp/rpc", nil)
	httpReq.Header.Set("X-API-Key", token)
	c := e.NewContext(httpReq, httptest.NewRecorder())
	c.Set(string(auth.UserContextKey), user)

	resp := h.handleToolsCall(c, &req, user)
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeInternalError, resp.Error.Code)
}

// TestHandlerToolsListFailsClosedOnResolveError proves a resolution error does
// not fall back to listing the full scope-permitted catalog.
func TestHandlerToolsListFailsClosedOnResolveError(t *testing.T) {
	store := newFakeShareStore()
	store.getByTokenErr = errors.New("db down")
	h := newTestHandler(store, &fakeTokenSvc{}, &fakeAgentDir{})
	token := "api-key"
	h.sessions[token] = &Session{Initialized: true, ProjectID: "proj-1"}

	user := &auth.AuthUser{ID: "u", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{"data:read"}}
	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/list"}

	e := echo.New()
	httpReq := httptest.NewRequest(http.MethodPost, "/api/mcp/rpc", nil)
	httpReq.Header.Set("X-API-Key", token)
	c := e.NewContext(httpReq, httptest.NewRecorder())
	c.Set(string(auth.UserContextKey), user)

	resp := h.handleToolsList(c, &req, user)
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeInternalError, resp.Error.Code)
	assert.Nil(t, resp.Result)
}

// TestStreamablePromptsDeniedForRestrictedInstance proves the streamable
// transport applies the same content gate.
func TestStreamablePromptsDeniedForRestrictedInstance(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1",
		AllowedTools: []string{"entity-search"},
	}
	h := NewStreamableHTTPHandler(&Service{shareInstances: store, shareTokens: &fakeTokenSvc{}, agentDir: &fakeAgentDir{}}, slog.Default())
	session := &MCPSession{ID: "s1", ProjectID: "proj-1", Initialized: true}
	user := &auth.AuthUser{ID: "u", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{"data:read"}}

	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/mcp", nil), httptest.NewRecorder())
	resp := h.handlePromptsList(c, &Request{JSONRPC: "2.0", ID: json.RawMessage(`1`)}, session, user)
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeForbidden, resp.Error.Code)
}
