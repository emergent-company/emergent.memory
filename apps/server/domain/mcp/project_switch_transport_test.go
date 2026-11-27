package mcp

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

const (
	testProjectA = "11111111-1111-1111-1111-111111111111"
	testProjectB = "22222222-2222-2222-2222-222222222222"
	testProjectC = "33333333-3333-3333-3333-333333333333"
)

// expectProjectSummary sets the expectation for the single-project lookup used
// by `project` action=use results.
func expectProjectSummary(t *testing.T, mock sqlmock.Sqlmock, id, name, orgID, orgName string) {
	t.Helper()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT") + `[\s\S]*kb\.projects[\s\S]*kb\.orgs[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "organization_id", "organization_name"}).
			AddRow(id, name, orgID, orgName))
}

func projectCall(name string, args map[string]any) ToolsCallParams {
	return ToolsCallParams{Name: name, Arguments: args}
}

// TestStreamableProjectSwitchFlow drives the whole issue-#1163 flow through the
// Streamable HTTP transport: list the user's projects, switch the session, then
// prove a later call re-checks membership rather than trusting the switch.
func TestStreamableProjectSwitchFlow(t *testing.T) {
	db, mock := newAuthzMockDB(t)
	svc := &Service{db: db}
	h := NewStreamableHTTPHandler(svc, slog.Default())
	user := &auth.AuthUser{ID: "u1", Scopes: []string{"graph:read", "projects:read"}}

	initRec, initResp := streamEndpointRequest(t, h, user, "initialize", InitializeParams{
		ProtocolVersion: LatestProtocolVersion,
		ClientInfo:      ClientInfo{Name: "claude-connector", Version: "1.0"},
	}, "")
	require.Nil(t, initResp.Error, "%+v", initResp.Error)
	sessionID := initRec.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID)

	// list: the user belongs to two projects; proj-a is the active one.
	expectMemberProjectRows(t, mock,
		[]string{testProjectA, "Alpha", "org-1", "Org One", "org_member"},
		[]string{testProjectB, "Beta", "org-1", "Org One", "org_member"},
	)
	_, listResp := streamEndpointRequest(t, h, user, "tools/call", projectCall("project", map[string]any{"action": "list"}), sessionID)
	require.Nil(t, listResp.Error, "%+v", listResp.Error)
	listResult := listResp.Result.(map[string]any)
	listData := listResult["structuredContent"].(map[string]any)["data"].(map[string]any)
	require.EqualValues(t, 2, listData["count"])

	// use: proj-b is a project the user belongs to.
	expectProjectOrg(t, mock, "org-1")
	expectMembership(t, mock, true)
	expectProjectSummary(t, mock, testProjectB, "Beta", "org-1", "Org One")
	_, useResp := streamEndpointRequest(t, h, user, "tools/call", projectCall("project", map[string]any{
		"action": "use", "project_id": testProjectB,
	}), sessionID)
	require.Nil(t, useResp.Error, "%+v", useResp.Error)

	h.sessionsMu.RLock()
	session := h.sessions[sessionID]
	h.sessionsMu.RUnlock()
	require.NotNil(t, session)
	require.Equal(t, testProjectB, session.ProjectID)
	require.True(t, session.ProjectSwitched, "an explicit switch must satisfy the write gate")

	// A later call re-checks membership: the user has since been removed from
	// the project's organization, so the switched session must be refused.
	expectProjectOrg(t, mock, "org-1")
	expectMembership(t, mock, false)
	_, denied := streamEndpointRequest(t, h, user, "tools/call", projectCall("entity-query", map[string]any{"type_name": "Person"}), sessionID)
	require.NotNil(t, denied.Error)
	require.Equal(t, ErrCodeForbidden, denied.Error.Code)
	require.Contains(t, denied.Error.Message, "Project access denied")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestStreamableProjectSwitchDeniedLeavesSessionUnchanged pins the
// mutation-before-check rule from initialize onto the switch action.
func TestStreamableProjectSwitchDeniedLeavesSessionUnchanged(t *testing.T) {
	db, mock := newAuthzMockDB(t)
	svc := &Service{db: db}
	h := NewStreamableHTTPHandler(svc, slog.Default())
	user := &auth.AuthUser{ID: "u1", Scopes: []string{"graph:read", "projects:read"}}

	initRec, initResp := streamEndpointRequest(t, h, user, "initialize", InitializeParams{
		ProtocolVersion: LatestProtocolVersion,
		ClientInfo:      ClientInfo{Name: "claude-connector", Version: "1.0"},
	}, "")
	require.Nil(t, initResp.Error)
	sessionID := initRec.Header().Get("Mcp-Session-Id")

	expectProjectOrg(t, mock, "org-victim")
	expectMembership(t, mock, false)
	_, resp := streamEndpointRequest(t, h, user, "tools/call", projectCall("project", map[string]any{
		"action": "use", "project_id": testProjectC,
	}), sessionID)
	require.NotNil(t, resp.Error)
	require.Equal(t, ErrCodeForbidden, resp.Error.Code)

	h.sessionsMu.RLock()
	session := h.sessions[sessionID]
	h.sessionsMu.RUnlock()
	require.Equal(t, "", session.ProjectID, "a denied switch must not poison the session")
	require.False(t, session.ProjectSwitched)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestStreamableRequireProjectSwitchGatesWrites pins the opt-in strict mode:
// until the client explicitly switches, write tools are refused with an
// actionable message, while the project control actions stay available.
func TestStreamableRequireProjectSwitchGatesWrites(t *testing.T) {
	svc := &Service{requireProjectSwitch: true}
	h := NewStreamableHTTPHandler(svc, slog.Default())
	user := &auth.AuthUser{ID: "u1", Scopes: []string{"graph:read", "graph:write", "projects:read"}}

	initRec, initResp := streamEndpointRequest(t, h, user, "initialize", InitializeParams{
		ProtocolVersion: LatestProtocolVersion,
		ClientInfo:      ClientInfo{Name: "claude-connector", Version: "1.0"},
	}, "")
	require.Nil(t, initResp.Error)
	sessionID := initRec.Header().Get("Mcp-Session-Id")

	_, resp := streamEndpointRequest(t, h, user, "tools/call", projectCall("entity-write", map[string]any{
		"action": "create", "entities": []any{},
	}), sessionID)
	require.NotNil(t, resp.Error, "a write before an explicit switch must be refused")
	require.Contains(t, resp.Error.Message, "Project switch required")

	_, listResp := streamEndpointRequest(t, h, user, "tools/call", projectCall("project", map[string]any{"action": "list"}), sessionID)
	require.Nil(t, listResp.Error, "the list action must stay available in strict mode: %+v", listResp.Error)
}

// TestStreamableInitializeProjectCountsAsExplicitSwitch pins that an explicit
// initialize project_id satisfies the strict-mode gate, while a session that
// inherits a project does not.
func TestStreamableInitializeProjectCountsAsExplicitSwitch(t *testing.T) {
	db, mock := newAuthzMockDB(t)
	svc := &Service{db: db, requireProjectSwitch: true}
	h := NewStreamableHTTPHandler(svc, slog.Default())
	user := &auth.AuthUser{ID: "u1", Scopes: []string{"graph:read", "graph:write"}}

	expectProjectOrg(t, mock, "org-1")
	expectMembership(t, mock, true)
	initRec, initResp := streamEndpointRequest(t, h, user, "initialize", InitializeParams{
		ProtocolVersion: LatestProtocolVersion,
		ClientInfo:      ClientInfo{Name: "claude-connector", Version: "1.0"},
		ProjectID:       testProjectA,
	}, "")
	require.Nil(t, initResp.Error, "%+v", initResp.Error)
	sessionID := initRec.Header().Get("Mcp-Session-Id")

	h.sessionsMu.RLock()
	session := h.sessions[sessionID]
	h.sessionsMu.RUnlock()
	require.True(t, session.ProjectSwitched)
	require.NoError(t, mock.ExpectationsWereMet())
}

// legacyRPCRequest drives one JSON-RPC request through the legacy /api/mcp/rpc
// handler, keyed on the bearer token so sessions persist across calls.
func legacyRPCRequest(t *testing.T, h *Handler, user *auth.AuthUser, token, method string, params any) Response {
	t.Helper()

	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: method}
	if params != nil {
		body, err := json.Marshal(params)
		require.NoError(t, err)
		req.Params = body
	}
	body, err := json.Marshal(req)
	require.NoError(t, err)

	httpReq := httptest.NewRequest(http.MethodPost, "/api/mcp/rpc", bytes.NewReader(body))
	httpReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	httpReq.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httpReq, rec)
	c.Set(string(auth.UserContextKey), user)
	require.NoError(t, h.HandleRPC(c))

	var resp Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

// TestLegacyHandlerProjectSwitch pins the same switch behaviour on the legacy
// JSON-RPC transport, whose project resolution previously always preferred the
// auth-resolved header project over the session.
func TestLegacyHandlerProjectSwitch(t *testing.T) {
	db, mock := newAuthzMockDB(t)
	svc := &Service{db: db}
	h := NewHandler(svc, slog.Default(), nil)
	user := &auth.AuthUser{ID: "u1", Scopes: []string{"graph:read", "projects:read"}}

	initResp := legacyRPCRequest(t, h, user, "tok-1", "initialize", InitializeParams{
		ProtocolVersion: LatestProtocolVersion,
		ClientInfo:      ClientInfo{Name: "claude-connector", Version: "1.0"},
	})
	require.Nil(t, initResp.Error, "%+v", initResp.Error)

	expectProjectOrg(t, mock, "org-1")
	expectMembership(t, mock, true)
	expectProjectSummary(t, mock, testProjectB, "Beta", "org-1", "Org One")
	useResp := legacyRPCRequest(t, h, user, "tok-1", "tools/call", projectCall("project", map[string]any{
		"action": "use", "project_id": testProjectB,
	}))
	require.Nil(t, useResp.Error, "%+v", useResp.Error)

	h.sessionsMu.RLock()
	session := h.sessions["tok-1"]
	h.sessionsMu.RUnlock()
	require.NotNil(t, session)
	require.Equal(t, testProjectB, session.ProjectID)
	require.True(t, session.ProjectSwitched)

	// A later call re-checks membership and uses the switched project.
	expectProjectOrg(t, mock, "org-1")
	expectMembership(t, mock, false)
	denied := legacyRPCRequest(t, h, user, "tok-1", "tools/call", projectCall("entity-query", map[string]any{"type_name": "Person"}))
	require.NotNil(t, denied.Error)
	require.Equal(t, ErrCodeForbidden, denied.Error.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
