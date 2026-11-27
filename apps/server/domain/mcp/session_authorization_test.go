package mcp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// newSessionAuthzHandler builds a transport with empty session maps. The paths
// under test (session lookup and authorization) never reach the service, so a
// zero Service and no database are enough.
func newSessionAuthzHandler() *StreamableHTTPHandler {
	return NewStreamableHTTPHandler(&Service{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func sessionAuthzContext(method, sessionID string, user *auth.AuthUser) echo.Context {
	e := echo.New()
	req := httptest.NewRequest(method, "/api/mcp", nil)
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	if method == http.MethodGet {
		req.Header.Set("Accept", "text/event-stream")
	}
	c := e.NewContext(req, httptest.NewRecorder())
	c.Set(string(auth.UserContextKey), user)
	return c
}

// TestAuthorizeSession pins the session authorization contract: a session is
// bound to its creating user, its creating credential and its project, and the
// session ID alone authorizes nothing.
func TestAuthorizeSession(t *testing.T) {
	base := func() *MCPSession {
		return &MCPSession{
			ID:          "s1",
			UserID:      "user-1",
			APITokenID:  "token-1",
			ProjectID:   "project-a",
			Initialized: true,
		}
	}
	oidcSession := func() *MCPSession {
		session := base()
		session.APITokenID = ""
		return session
	}
	oidcUser := func() *auth.AuthUser {
		return &auth.AuthUser{ID: "user-1", ProjectID: "project-a"}
	}
	tokenUser := func() *auth.AuthUser {
		return &auth.AuthUser{ID: "user-1", APITokenID: "token-1", ProjectID: "project-a"}
	}

	tests := []struct {
		name    string
		session *MCPSession
		user    *auth.AuthUser
		admin   bool
		wantErr bool
	}{
		{name: "same user, same token, same project", session: base(), user: tokenUser()},
		{name: "same user, OIDC both sides", session: oidcSession(), user: oidcUser()},
		{
			name:    "different user",
			session: base(),
			user:    &auth.AuthUser{ID: "user-2", APITokenID: "token-2", ProjectID: "project-a"},
			wantErr: true,
		},
		{
			name:    "same user, different token",
			session: base(),
			user:    &auth.AuthUser{ID: "user-1", APITokenID: "token-2", ProjectID: "project-a"},
			wantErr: true,
		},
		{
			name:    "token session resumed by OIDC caller",
			session: base(),
			user:    oidcUser(),
			wantErr: true,
		},
		{
			name:    "OIDC session resumed by token caller",
			session: oidcSession(),
			user:    tokenUser(),
			wantErr: true,
		},
		{
			name:    "same credential, different project",
			session: oidcSession(),
			user:    &auth.AuthUser{ID: "user-1", ProjectID: "project-b"},
			wantErr: true,
		},
		{
			name:    "token caller addressing another project",
			session: base(),
			user:    &auth.AuthUser{ID: "user-1", APITokenID: "token-1", ProjectID: "project-b"},
			wantErr: true,
		},
		{
			name:    "project-less session adopts the caller project",
			session: func() *MCPSession { s := base(); s.ProjectID = ""; return s }(),
			user:    tokenUser(),
		},
		{
			name:    "project session used without a caller project",
			session: base(),
			user:    &auth.AuthUser{ID: "user-1", APITokenID: "token-1"},
		},
		{
			name:    "operator transport skips the project rule",
			session: base(), // session bound to project-a
			user:    &auth.AuthUser{ID: "user-1", APITokenID: "token-1", ProjectID: "project-b"},
			admin:   true,
		},
		{
			name:    "operator transport still checks the credential",
			session: base(),
			user:    &auth.AuthUser{ID: "user-1", APITokenID: "token-2", ProjectID: "project-a"},
			admin:   true,
			wantErr: true,
		},
		{
			name:    "operator transport still checks identity",
			session: base(),
			user:    &auth.AuthUser{ID: "user-2", APITokenID: "token-1"},
			admin:   true,
			wantErr: true,
		},
		{name: "nil user fails closed", session: base(), user: nil, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newSessionAuthzHandler()
			h.admin = tc.admin
			err := h.authorizeSession(tc.session, tc.user)
			if tc.wantErr {
				require.ErrorIs(t, err, errSessionForbidden)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestGetOrCreateSessionRejectsForeignCaller proves the POST path distinguishes
// a missing session from one the caller may not use, while both are reported as
// session_not_found on the wire.
func TestGetOrCreateSessionRejectsForeignCaller(t *testing.T) {
	h := newSessionAuthzHandler()
	h.putSession(&MCPSession{
		ID:          "s1",
		UserID:      "user-1",
		APITokenID:  "token-1",
		ProjectID:   "project-a",
		Initialized: true,
	})
	owner := &auth.AuthUser{ID: "user-1", APITokenID: "token-1", ProjectID: "project-a"}

	_, err := h.getOrCreateSession("s1",
		&auth.AuthUser{ID: "user-2", APITokenID: "token-2", ProjectID: "project-b"}, LatestProtocolVersion)
	require.ErrorIs(t, err, errSessionForbidden)

	_, err = h.getOrCreateSession("s1",
		&auth.AuthUser{ID: "user-1", APITokenID: "token-2", ProjectID: "project-a"}, LatestProtocolVersion)
	require.ErrorIs(t, err, errSessionForbidden)

	_, err = h.getOrCreateSession("s1",
		&auth.AuthUser{ID: "user-1", ProjectID: "project-b"}, LatestProtocolVersion)
	require.ErrorIs(t, err, errSessionForbidden)

	_, err = h.getOrCreateSession("does-not-exist", owner, LatestProtocolVersion)
	require.ErrorIs(t, err, errSessionNotFound)

	session, err := h.getOrCreateSession("s1", owner, LatestProtocolVersion)
	require.NoError(t, err)
	require.Equal(t, "project-a", session.ProjectID)
}

// TestStreamableGetAndDeleteRejectForeignCaller covers the two other request
// paths that address a session by ID: the SSE stream (GET) and termination
// (DELETE). A refused DELETE must not destroy the owner's session.
func TestStreamableGetAndDeleteRejectForeignCaller(t *testing.T) {
	h := newSessionAuthzHandler()
	owner := &auth.AuthUser{ID: "user-1", APITokenID: "token-1", ProjectID: "project-a"}
	intruder := &auth.AuthUser{ID: "user-2", APITokenID: "token-2", ProjectID: "project-b"}
	h.putSession(&MCPSession{
		ID:          "s1",
		UserID:      "user-1",
		APITokenID:  "token-1",
		ProjectID:   "project-a",
		Initialized: true,
	})

	// handleGET would block forever on a successful stream, so only the refusal
	// path is exercised here; the refusal happens before streaming starts.
	getCtx := sessionAuthzContext(http.MethodGet, "s1", intruder)
	require.NoError(t, h.handleGET(getCtx, LatestProtocolVersion))
	require.Equal(t, http.StatusNotFound, getCtx.Response().Status)

	delCtx := sessionAuthzContext(http.MethodDelete, "s1", intruder)
	require.NoError(t, h.handleDELETE(delCtx))
	require.Equal(t, http.StatusNotFound, delCtx.Response().Status)
	require.NotNil(t, h.getSession("s1"), "a refused DELETE must not terminate the session")

	ownerDelCtx := sessionAuthzContext(http.MethodDelete, "s1", owner)
	require.NoError(t, h.handleDELETE(ownerDelCtx))
	require.Equal(t, http.StatusNoContent, ownerDelCtx.Response().Status)
	require.Nil(t, h.getSession("s1"))
}

// TestAuthorizeSSESession pins the legacy SSE transport's equivalent binding.
func TestAuthorizeSSESession(t *testing.T) {
	user := &auth.AuthUser{ID: "user-1", APITokenID: "token-1", ProjectID: "project-a"}
	state := &sessionInitState{ProjectID: "project-a", UserID: "user-1", APITokenID: "token-1"}
	stream := &SSESession{ID: "sse-1", ProjectID: "project-a", UserID: "user-1", APITokenID: "token-1"}

	require.NoError(t, authorizeSSESession("project-a", user, state, stream))
	require.NoError(t, authorizeSSESession("project-a", user, nil, nil), "an unbound session id is created on initialize")

	require.Error(t, authorizeSSESession("project-a", nil, nil, nil), "a nil caller has no identity to bind the session to")
	require.Error(t, authorizeSSESession("project-b", user, state, nil))
	require.Error(t, authorizeSSESession("project-b", user, nil, stream))
	require.Error(t, authorizeSSESession("project-a",
		&auth.AuthUser{ID: "user-2", APITokenID: "token-2"}, state, stream))
	require.Error(t, authorizeSSESession("project-a",
		&auth.AuthUser{ID: "user-1", APITokenID: "token-2"}, state, nil))
}

// TestHandleSSEMessageRejectsForeignSession pins that the SSE message path
// actually calls the authorization it documents: a session id minted by one
// caller must be refused for another user or another credential, before the
// request is parsed or its response is pushed onto the owner's stream.
func TestHandleSSEMessageRejectsForeignSession(t *testing.T) {
	const projectA = "11111111-1111-4111-8111-111111111111"
	newHandler := func() *SSEHandler {
		h := NewSSEHandler(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		h.sseSessions["sse-1"] = &SSESession{
			ID: "sse-1", ProjectID: projectA, UserID: "user-1", APITokenID: "token-1",
			Done: make(chan struct{}),
		}
		h.sessionState["sse-1"] = &sessionInitState{
			ProjectID: projectA, UserID: "user-1", APITokenID: "token-1", Initialized: true,
		}
		return h
	}
	post := func(h *SSEHandler, user *auth.AuthUser) error {
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/mcp/sse/"+projectA+"/message?sessionId=sse-1", nil)
		c := e.NewContext(req, httptest.NewRecorder())
		c.SetParamNames("projectId")
		c.SetParamValues(projectA)
		c.Set(string(auth.UserContextKey), user)
		return h.HandleSSEMessage(c)
	}

	for _, tc := range []struct {
		name string
		user *auth.AuthUser
	}{
		{"another user", &auth.AuthUser{ID: "user-2", APITokenID: "token-2", ProjectID: projectA}},
		{"another credential", &auth.AuthUser{ID: "user-1", APITokenID: "token-2", ProjectID: projectA}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := post(newHandler(), tc.user)
			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, http.StatusForbidden, appErr.HTTPStatus)
		})
	}
}

// TestPutSessionDoesNotResurrectDeletedSession covers the write-back rule: a
// request that read a session before a concurrent DELETE must not insert it
// back, and the notifications/initialized update must not either.
func TestPutSessionDoesNotResurrectDeletedSession(t *testing.T) {
	h := newSessionAuthzHandler()
	user := &auth.AuthUser{ID: "user-1", APITokenID: "token-1", ProjectID: "project-a"}
	h.putSession(&MCPSession{
		ID: "s1", UserID: user.ID, APITokenID: user.APITokenID, ProjectID: user.ProjectID,
	})
	snapshot, err := h.getOrCreateSession("s1", user, LatestProtocolVersion)
	require.NoError(t, err)

	// A concurrent DELETE terminates the session after the snapshot was taken.
	h.sessionsMu.Lock()
	delete(h.sessions, "s1")
	h.sessionsMu.Unlock()

	h.putSession(snapshot)
	require.Nil(t, h.getSession("s1"), "an in-flight write must not resurrect a terminated session")

	h.markSessionInitialized("s1")
	require.Nil(t, h.getSession("s1"), "initialized must not insert a terminated session")
}

// TestMarkSessionInitializedKeepsReboundProject pins the field-scoped update: a
// stale notifications/initialized must not restore the project id that a
// re-initialize changed.
func TestMarkSessionInitializedKeepsReboundProject(t *testing.T) {
	h := newSessionAuthzHandler()
	user := &auth.AuthUser{ID: "user-1", APITokenID: "token-1", ProjectID: "project-a"}
	h.putSession(&MCPSession{
		ID: "s1", UserID: user.ID, APITokenID: user.APITokenID, ProjectID: "project-a",
	})

	stale, err := h.getOrCreateSession("s1", user, LatestProtocolVersion)
	require.NoError(t, err)
	require.Equal(t, "project-a", stale.ProjectID)

	// A re-initialize by the same user and credential rebinds the session.
	rebound := *stale
	rebound.ProjectID = "project-b"
	rebound.Initialized = true
	h.putSession(&rebound)

	h.markSessionInitialized(stale.ID) // carries the pre-rebind project id
	stored := h.getSession("s1")
	require.NotNil(t, stored)
	require.Equal(t, "project-b", stored.ProjectID, "the stale notification must not revert the project")
	require.True(t, stored.Initialized)
}
