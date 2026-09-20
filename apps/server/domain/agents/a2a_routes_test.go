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

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

const (
	a2aTestStandaloneKey   = "test-standalone-key"
	a2aTestStandaloneEmail = "admin@localhost"
	a2aTestProjectID       = "proj-test-id"
	a2aTestOrgID           = "org-test-id"
)

// addA2AStandaloneAuth attaches standalone-mode auth headers so a request
// authenticates without a DB. X-Org-ID is set so RequireAuth skips the org
// resolution DB lookup (the test middleware has a nil DB).
func addA2AStandaloneAuth(req *http.Request) {
	req.Header.Set("X-API-Key", a2aTestStandaloneKey)
	req.Header.Set("X-Project-ID", a2aTestProjectID)
	req.Header.Set("X-Org-ID", a2aTestOrgID)
}

// newA2ATestRouter builds an Echo instance with RegisterA2ARoutes and a
// standalone-mode auth middleware so requests can authenticate without a DB.
func newA2ATestRouter(t *testing.T) (*echo.Echo, *A2AHandler) {
	t.Helper()
	e := echo.New()
	h := &A2AHandler{log: slog.Default()}
	cfg := &config.Config{
		Standalone: config.StandaloneConfig{Enabled: true, APIKey: a2aTestStandaloneKey, UserEmail: a2aTestStandaloneEmail},
	}
	mw := auth.NewMiddleware(auth.MiddlewareParams{Cfg: cfg, Log: slog.Default()})
	RegisterA2ARoutes(e, h, mw)
	return e, h
}

// newA2AActionContext builds an Echo context with the terminal wildcard param
// set to the given value and (optionally) an authenticated user.
func newA2AActionContext(wildcard string, user *auth.AuthUser) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/tasks/"+wildcard, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("*")
	c.SetParamValues(wildcard)
	if user != nil {
		c.Set(string(auth.UserContextKey), user)
	}
	return c, rec
}

func TestParseTaskActionSuffix(t *testing.T) {
	cases := []struct {
		name     string
		wildcard string
		wantID   string
		wantAct  string
		wantOK   bool
	}{
		{name: "cancel", wildcard: "abc:cancel", wantID: "abc", wantAct: "cancel", wantOK: true},
		{name: "subscribe", wildcard: "abc:subscribe", wantID: "abc", wantAct: "subscribe", wantOK: true},
		{name: "uuid id", wildcard: "3f2c0e5a-1234-5678-9abc-def012345678:cancel", wantID: "3f2c0e5a-1234-5678-9abc-def012345678", wantAct: "cancel", wantOK: true},
		{name: "unknown action", wildcard: "abc:bogus", wantOK: false},
		{name: "empty id", wildcard: ":cancel", wantOK: false},
		{name: "no colon", wildcard: "abccancel", wantOK: false},
		{name: "trailing colon", wildcard: "abc:", wantOK: false},
		{name: "empty", wildcard: "", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, act, ok := parseTaskActionSuffix(tc.wildcard)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantID, id)
			assert.Equal(t, tc.wantAct, act)
		})
	}
}

func TestRequireA2AScope(t *testing.T) {
	t.Run("no user is not a scope error (auth enforced upstream)", func(t *testing.T) {
		// Authentication is enforced by a2aStreamingAuthMiddleware wrapping the
		// dispatcher route, so the scope helper only performs the scope check.
		c, _ := newA2AActionContext("abc:cancel", nil)
		assert.Nil(t, requireA2AScope(c, "agents:write"))
	})

	t.Run("non-token session passes through", func(t *testing.T) {
		user := &auth.AuthUser{ID: "u", APITokenID: ""} // OAuth/standalone: no token
		c, _ := newA2AActionContext("abc:cancel", user)
		assert.Nil(t, requireA2AScope(c, "agents:write"))
	})

	t.Run("api token with scope passes", func(t *testing.T) {
		user := &auth.AuthUser{ID: "u", APITokenID: "tok", Scopes: []string{"agents:write"}}
		c, _ := newA2AActionContext("abc:cancel", user)
		assert.Nil(t, requireA2AScope(c, "agents:write"))
	})

	t.Run("api token missing scope returns 403", func(t *testing.T) {
		user := &auth.AuthUser{ID: "u", APITokenID: "tok", Scopes: []string{"agents:read"}}
		c, _ := newA2AActionContext("abc:cancel", user)
		a2aErr := requireA2AScope(c, "agents:write")
		require.NotNil(t, a2aErr)
		assert.Equal(t, http.StatusForbidden, a2aErr.Code.HTTPStatus())
	})
}

func TestResolveTaskAction_ExtractsIDAndAction(t *testing.T) {
	user := &auth.AuthUser{ID: "u", APITokenID: "tok", Scopes: []string{"agents:write", "agents:read"}}
	c, _ := newA2AActionContext("task-123:cancel", user)

	id, action, a2aErr := resolveTaskAction(c)
	require.Nil(t, a2aErr)
	assert.Equal(t, "task-123", id)
	assert.Equal(t, "cancel", action)
}

func TestResolveTaskAction_UnknownActionReturns405(t *testing.T) {
	user := &auth.AuthUser{ID: "u", APITokenID: "tok", Scopes: []string{"agents:write"}}
	c, _ := newA2AActionContext("task-123:bogus", user)

	_, _, a2aErr := resolveTaskAction(c)
	require.NotNil(t, a2aErr)
	assert.Equal(t, http.StatusMethodNotAllowed, a2aErr.Code.HTTPStatus())
}

func TestA2AErrorFrom_ConvertsPlatformErrors(t *testing.T) {
	// echo.HTTPError 403 (RequireAPITokenScopes) → PERMISSION_DENIED.
	a2aErr := a2aErrorFrom(echo.NewHTTPError(http.StatusForbidden, "insufficient"))
	require.NotNil(t, a2aErr)
	assert.Equal(t, http.StatusForbidden, a2aErr.Code.HTTPStatus())
	assert.Equal(t, A2AReasonPermissionDenied, a2aErr.Reason)

	// apperror 401 (RequireAuth's authError) → UNAUTHENTICATED.
	a2aErr = a2aErrorFrom(apperror.ErrUnauthorized)
	require.NotNil(t, a2aErr)
	assert.Equal(t, http.StatusUnauthorized, a2aErr.Code.HTTPStatus())
	assert.Equal(t, A2AReasonUnauthenticated, a2aErr.Reason)

	// An existing A2AError passes through unchanged.
	orig := NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "nope")
	assert.Same(t, orig, a2aErrorFrom(orig))
}

func TestA2AErrorFromStatus(t *testing.T) {
	assert.Equal(t, http.StatusUnauthorized, a2aErrorFromStatus(http.StatusUnauthorized).Code.HTTPStatus())
	assert.Equal(t, http.StatusForbidden, a2aErrorFromStatus(http.StatusForbidden).Code.HTTPStatus())
	assert.Equal(t, http.StatusInternalServerError, a2aErrorFromStatus(http.StatusTeapot).Code.HTTPStatus())
}

func TestA2ARoutes_MessageSendAndStreamAreLiteral(t *testing.T) {
	for _, path := range []string{"/message:send", "/message:stream"} {
		t.Run(path, func(t *testing.T) {
			e, _ := newA2ATestRouter(t)
			req := httptest.NewRequest(http.MethodPost, path, nil)
			addA2AStandaloneAuth(req)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			// Empty body reaches the handler, which rejects it with a 400 A2A
			// validation envelope. A 404 would mean the literal route did not match.
			assert.Equal(t, http.StatusBadRequest, rec.Code, "literal route %s should match and reach its handler", path)
			assert.Equal(t, A2AContentType, rec.Header().Get(echo.HeaderContentType))
		})
	}
}

// serveRecovering runs ServeHTTP and returns any recovered panic. A nil repo
// causes the concrete task handlers to panic once they reach their first DB
// call, which is the signal that routing + id extraction succeeded (a broken
// route would return 404 "task not found" before any DB call).
func serveRecovering(e *echo.Echo, rec *httptest.ResponseRecorder, req *http.Request) (panicked any) {
	defer func() { panicked = recover() }()
	e.ServeHTTP(rec, req)
	return nil
}

func TestA2ARoutes_TaskActionCancelRoutesToHandler(t *testing.T) {
	e, _ := newA2ATestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/tasks/abc:cancel", nil)
	addA2AStandaloneAuth(req)
	rec := httptest.NewRecorder()

	panicked := serveRecovering(e, rec, req)
	require.NotNil(t, panicked, "POST /tasks/abc:cancel must extract id and reach CancelTask (nil repo panic), got status %d", rec.Code)
}

func TestA2ARoutes_TaskActionSubscribeRoutesToHandler(t *testing.T) {
	e, _ := newA2ATestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/tasks/abc:subscribe", nil)
	addA2AStandaloneAuth(req)
	rec := httptest.NewRecorder()

	panicked := serveRecovering(e, rec, req)
	require.NotNil(t, panicked, "POST /tasks/abc:subscribe must extract id and reach SubscribeTask (nil repo panic), got status %d", rec.Code)
}

func TestA2ARoutes_TaskActionUnknownReturns405Envelope(t *testing.T) {
	e, _ := newA2ATestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/tasks/abc:bogus", nil)
	addA2AStandaloneAuth(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, A2AContentType, rec.Header().Get(echo.HeaderContentType))

	var env A2AErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Len(t, env.Error.Details, 1)
	assert.Equal(t, "METHOD_NOT_ALLOWED", env.Error.Details[0].Reason)
	assert.Equal(t, A2AErrorDomain, env.Error.Details[0].Domain)
}

func TestA2ARoutes_NoAuthReturns401Envelope(t *testing.T) {
	e, _ := newA2ATestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/extendedAgentCard", nil) // no auth headers
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, A2AContentType, rec.Header().Get(echo.HeaderContentType))

	var env A2AErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Len(t, env.Error.Details, 1)
	assert.Equal(t, "UNAUTHENTICATED", env.Error.Details[0].Reason)
	assert.Equal(t, A2AErrorDomain, env.Error.Details[0].Domain)
}

func TestA2ARoutes_NonA2ARouteErrorShapeUnchanged(t *testing.T) {
	e := echo.New()
	cfg := &config.Config{
		Standalone: config.StandaloneConfig{Enabled: true, APIKey: a2aTestStandaloneKey, UserEmail: a2aTestStandaloneEmail},
	}
	mw := auth.NewMiddleware(auth.MiddlewareParams{Cfg: cfg, Log: slog.Default()})
	// A plain (non-A2A) route protected only by RequireAuth, with no A2A error
	// middleware.
	e.GET("/plain", func(c echo.Context) error { return c.String(http.StatusOK, "ok") }, mw.RequireAuth())

	req := httptest.NewRequest(http.MethodGet, "/plain", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "missing_token", "non-A2A route must keep the platform error shape")
	assert.NotContains(t, body, `"@type"`, "non-A2A route must not use the A2A envelope")
	assert.NotContains(t, body, A2AErrorDomain)
}

func TestA2ARoutes_PushNotificationConfigsReturnNotSupported(t *testing.T) {
	paths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/tasks/abc/pushNotificationConfigs"},
		{http.MethodPost, "/tasks/abc/pushNotificationConfigs"},
		{http.MethodGet, "/tasks/abc/pushNotificationConfigs/cfg-1"},
		{http.MethodPut, "/tasks/abc/pushNotificationConfigs/cfg-1"},
		{http.MethodDelete, "/tasks/abc/pushNotificationConfigs"},
		{http.MethodDelete, "/tasks/abc/pushNotificationConfigs/cfg-1"},
	}
	for _, tc := range paths {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			e, _ := newA2ATestRouter(t)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			addA2AStandaloneAuth(req)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, A2AContentType, rec.Header().Get(echo.HeaderContentType))

			var env A2AErrorEnvelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			require.Len(t, env.Error.Details, 1)
			assert.Equal(t, "PUSH_NOTIFICATION_NOT_SUPPORTED", env.Error.Details[0].Reason)
			assert.Equal(t, A2AErrorDomain, env.Error.Details[0].Domain)
		})
	}
}

func TestA2ARoutes_StreamNoAuthReturnsA2AEnvelope(t *testing.T) {
	e, _ := newA2ATestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/message:stream", nil) // no auth
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, A2AContentType, rec.Header().Get(echo.HeaderContentType))
	var env A2AErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Len(t, env.Error.Details, 1)
	assert.Equal(t, "UNAUTHENTICATED", env.Error.Details[0].Reason)
	assert.Equal(t, A2AErrorDomain, env.Error.Details[0].Domain)
}

func TestA2ARoutes_SubscribeNoAuthReturnsA2AEnvelope(t *testing.T) {
	e, _ := newA2ATestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/tasks/abc:subscribe", nil) // no auth
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, A2AContentType, rec.Header().Get(echo.HeaderContentType))
	var env A2AErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Len(t, env.Error.Details, 1)
	assert.Equal(t, "UNAUTHENTICATED", env.Error.Details[0].Reason)
	assert.Equal(t, A2AErrorDomain, env.Error.Details[0].Domain)
}

func TestA2ARoutes_StreamAuthenticatedReachesHandler(t *testing.T) {
	e, _ := newA2ATestRouter(t)
	body := `{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"hi"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/message:stream", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	addA2AStandaloneAuth(req)
	rec := httptest.NewRecorder()

	panicked := serveRecovering(e, rec, req)
	require.NotNil(t, panicked, "authenticated stream must reach the streaming handler (nil repo panic), got status %d", rec.Code)
}
