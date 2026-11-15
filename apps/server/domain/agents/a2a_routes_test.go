package agents

import (
	"context"
	"encoding/json"
	"io"
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
//
// The handler's project-membership hook is a permissive stub: the tests in this
// file are about routing, envelopes and scope gates, and a nil repository cannot
// answer a membership query. The guard itself is covered by
// a2a_project_authz_test.go (unit, sqlmock rows) and a2a_project_authz_db_test.go
// (real projects, memberships and tokens); the tests that need a denial install
// their own hook through newA2ATestRouterWithAuthz.
func newA2ATestRouter(t *testing.T) (*echo.Echo, *A2AHandler) {
	t.Helper()
	return newA2ATestRouterWithAuthz(t, func(context.Context, string) error { return nil })
}

// newA2ATestRouterWithAuthz is newA2ATestRouter with an explicit
// project-authorization hook, so a test can drive the guard's deny/allow branch
// at the route level.
func newA2ATestRouterWithAuthz(t *testing.T, authorize func(context.Context, string) error) (*echo.Echo, *A2AHandler) {
	t.Helper()
	e := echo.New()
	h := &A2AHandler{log: slog.Default(), authorizeProject: authorize}
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

	// Issue #660: a session credential used to skip the scope check entirely,
	// which left the A2A surface enforcing no scope for OIDC callers.
	t.Run("session without the scope is denied", func(t *testing.T) {
		user := &auth.AuthUser{ID: "u", APITokenID: "", Scopes: []string{"agents:read"}}
		c, _ := newA2AActionContext("abc:cancel", user)
		a2aErr := requireA2AScope(c, "agents:write")
		require.NotNil(t, a2aErr)
		assert.Equal(t, http.StatusForbidden, a2aErr.Code.HTTPStatus())
		assert.Equal(t, A2AReasonPermissionDenied, a2aErr.Reason)
	})

	t.Run("session holding the scope passes", func(t *testing.T) {
		user := &auth.AuthUser{ID: "u", APITokenID: "", Scopes: []string{"agents:write"}}
		c, _ := newA2AActionContext("abc:cancel", user)
		assert.Nil(t, requireA2AScope(c, "agents:write"))
	})

	t.Run("session scope is expanded through umbrellas", func(t *testing.T) {
		// project:admin implies agents:write through ScopeImplies; a session
		// that carries only the umbrella must still pass.
		user := &auth.AuthUser{ID: "u", APITokenID: "", Scopes: []string{"project:admin"}}
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
	unauthenticated := a2aErrorFromStatus(http.StatusUnauthorized)
	assert.Equal(t, http.StatusUnauthorized, unauthenticated.Code.HTTPStatus())
	assert.Equal(t, A2AReasonUnauthenticated, unauthenticated.Reason)

	denied := a2aErrorFromStatus(http.StatusForbidden)
	assert.Equal(t, http.StatusForbidden, denied.Code.HTTPStatus())
	assert.Equal(t, A2AReasonPermissionDenied, denied.Reason)

	// 4xx client mistakes map to their own reasons — never INVALID_AGENT_RESPONSE.
	badRequest := a2aErrorFromStatus(http.StatusBadRequest)
	assert.Equal(t, http.StatusBadRequest, badRequest.Code.HTTPStatus())
	assert.Equal(t, A2AReasonInvalidArgument, badRequest.Reason)

	notFound := a2aErrorFromStatus(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, notFound.Code.HTTPStatus())
	assert.Equal(t, A2AReasonNotFound, notFound.Reason)

	// A non-standard 4xx is still a client error, not a 5xx.
	teapot := a2aErrorFromStatus(http.StatusTeapot)
	assert.Equal(t, http.StatusBadRequest, teapot.Code.HTTPStatus())
	assert.Equal(t, A2AReasonInvalidArgument, teapot.Reason)

	// A genuine server-side failure stays INVALID_AGENT_RESPONSE.
	internal := a2aErrorFromStatus(http.StatusInternalServerError)
	assert.Equal(t, http.StatusInternalServerError, internal.Code.HTTPStatus())
	assert.Equal(t, A2AReasonInvalidAgentResponse, internal.Reason)
}

// TestAcpProjectID_MissingProjectReturnsProjectRequired is the #762 regression
// guard: a missing project selector is a client-side mistake and must surface
// as 400 INVALID_ARGUMENT / PROJECT_REQUIRED, not a 500 INVALID_AGENT_RESPONSE.
func TestAcpProjectID_MissingProjectReturnsProjectRequired(t *testing.T) {
	c, _ := newA2AActionContext("x", &auth.AuthUser{ID: "u"})

	_, err := acpProjectID(c)
	require.Error(t, err)

	a2aErr := a2aErrorFrom(err)
	require.NotNil(t, a2aErr)
	assert.Equal(t, http.StatusBadRequest, a2aErr.Code.HTTPStatus())
	assert.Equal(t, A2AReasonProjectRequired, a2aErr.Reason)
	assert.Contains(t, a2aErr.Message, "X-Project-ID")
}

func TestAcpProjectID_TokenProjectWinsOverHeader(t *testing.T) {
	c, _ := newA2AActionContext("x", &auth.AuthUser{ID: "u", ProjectID: "proj-from-header", APITokenProjectID: "proj-from-token", APITokenID: "tok"})
	got, err := acpProjectID(c)
	require.NoError(t, err)
	assert.Equal(t, "proj-from-token", got)
}

func TestAcpProjectID_HeaderUsedForAccountToken(t *testing.T) {
	c, _ := newA2AActionContext("x", &auth.AuthUser{ID: "u", ProjectID: "proj-from-header"})
	got, err := acpProjectID(c)
	require.NoError(t, err)
	assert.Equal(t, "proj-from-header", got)
}

func TestA2ARoutes_StreamWithoutProjectReturns400ProjectRequired(t *testing.T) {
	e, _ := newA2ATestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/message:stream", strings.NewReader(`{}`))
	req.Header.Set("X-API-Key", a2aTestStandaloneKey)
	req.Header.Set("X-Org-ID", a2aTestOrgID)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, A2AContentType, rec.Header().Get(echo.HeaderContentType))

	var env A2AErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Len(t, env.Error.Details, 1)
	assert.Equal(t, "PROJECT_REQUIRED", env.Error.Details[0].Reason)
	assert.Equal(t, int(A2ACodeInvalidArgument), env.Error.Code)
}

func TestA2ARoutes_ExtendedCardWithoutProjectReturns400ProjectRequired(t *testing.T) {
	e, _ := newA2ATestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/extendedAgentCard", nil)
	req.Header.Set("X-API-Key", a2aTestStandaloneKey)
	req.Header.Set("X-Org-ID", a2aTestOrgID)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, A2AContentType, rec.Header().Get(echo.HeaderContentType))

	var env A2AErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Len(t, env.Error.Details, 1)
	assert.Equal(t, "PROJECT_REQUIRED", env.Error.Details[0].Reason)
	assert.Equal(t, "INVALID_ARGUMENT", env.Error.Status)
	assert.Equal(t, int(A2ACodeInvalidArgument), env.Error.Code)
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

// ---------------------------------------------------------------------------
// Project authorization (issue #660)
// ---------------------------------------------------------------------------

// a2aGuardedOperations enumerates every A2A operation that addresses a project.
// Each one must reach the membership guard; the global card is the one exception
// (it is tenant-blind and unauthenticated).
var a2aGuardedOperations = []struct {
	name   string
	method string
	path   string
	body   string
}{
	{name: "message:send", method: http.MethodPost, path: "/message:send", body: `{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"hi"}]}}`},
	{name: "message:stream", method: http.MethodPost, path: "/message:stream", body: `{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"hi"}]}}`},
	{name: "tasks list", method: http.MethodGet, path: "/tasks"},
	{name: "task get", method: http.MethodGet, path: "/tasks/abc"},
	{name: "task cancel", method: http.MethodPost, path: "/tasks/abc:cancel"},
	{name: "task subscribe", method: http.MethodPost, path: "/tasks/abc:subscribe"},
	{name: "extended agent card", method: http.MethodGet, path: "/extendedAgentCard"},
}

// TestA2ARoutes_DeniedProjectReturnsPermissionEnvelopeOnEveryOperation is the
// #660 regression guard for the route wiring: a request that authenticates and
// names a project it is not entitled to must be refused on every guarded
// operation, in the A2A envelope, and must not reach the handler. Before the
// fix none of these handlers consulted project membership at all.
func TestA2ARoutes_DeniedProjectReturnsPermissionEnvelopeOnEveryOperation(t *testing.T) {
	for _, op := range a2aGuardedOperations {
		t.Run(op.name, func(t *testing.T) {
			var calls []string
			e, _ := newA2ATestRouterWithAuthz(t, func(_ context.Context, projectID string) error {
				calls = append(calls, projectID)
				return apperror.NewForbidden("access to project denied")
			})

			var body io.Reader
			if op.body != "" {
				body = strings.NewReader(op.body)
			}
			req := httptest.NewRequest(op.method, op.path, body)
			if op.body != "" {
				req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			}
			addA2AStandaloneAuth(req)
			rec := httptest.NewRecorder()

			// Recovered so a regression that skips the guard reaches a nil-repo
			// handler and fails the assertion instead of panicking the suite.
			panicked := serveRecovering(e, rec, req)
			assert.Nil(t, panicked, "%s %s reached its handler without the guard", op.method, op.path)

			assert.Equal(t, http.StatusForbidden, rec.Code, "%s %s must be refused", op.method, op.path)
			assert.Equal(t, A2AContentType, rec.Header().Get(echo.HeaderContentType))

			var env A2AErrorEnvelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			require.Len(t, env.Error.Details, 1)
			assert.Equal(t, "PERMISSION_DENIED", env.Error.Details[0].Reason)
			assert.Equal(t, A2AErrorDomain, env.Error.Details[0].Domain)

			// The guard must see the project the request addressed: the header
			// value for a session credential.
			require.Equal(t, []string{a2aTestProjectID}, calls,
				"%s %s must ask the membership guard about the addressed project", op.method, op.path)
		})
	}
}

// TestA2ARoutes_EveryAuthenticatedRouteConsultsTheGuard is the structural half
// of #660: rather than trusting the hand-written operation table above, it walks
// the routes the server actually registered and proves every registered route
// answers what it should when the guard denies: an authenticated route is refused,
// the two tenant-blind routes are not. A future A2A handler that forgets to
// resolve its project through addressedProject fails here.
//
// There is no silent exemption. The global card and the push-notification-config
// stubs are asserted to answer their own thing rather than skipped, so a real
// implementation appearing under those paths cannot hide from this sweep. Echo's
// synthetic "route not found" entries are the only skipped rows: they are the
// fallback for paths that match no handler and cannot reach one.
func TestA2ARoutes_EveryAuthenticatedRouteConsultsTheGuard(t *testing.T) {
	e, _ := newA2ATestRouterWithAuthz(t, func(context.Context, string) error {
		return apperror.NewForbidden("access to project denied")
	})

	substitutions := []struct{ from, to string }{
		{":id", "3f2c0e5a-1234-5678-9abc-def012345678"},
		{"/*", "/3f2c0e5a-1234-5678-9abc-def012345678:cancel"},
	}

	guarded := 0
	for _, route := range e.Routes() {
		if route.Method == echo.RouteNotFound {
			continue
		}
		path := route.Path
		for _, sub := range substitutions {
			path = strings.ReplaceAll(path, sub.from, sub.to)
		}

		t.Run(route.Method+" "+path, func(t *testing.T) {
			var body io.Reader
			if route.Method == http.MethodPost || route.Method == http.MethodPut {
				body = strings.NewReader(`{}`)
			}
			req := httptest.NewRequest(route.Method, path, body)
			addA2AStandaloneAuth(req)
			rec := httptest.NewRecorder()

			// Recovering keeps a regression readable: a route that skips the guard
			// reaches a nil-repo handler, and that must fail this assertion rather
			// than take the whole test binary down with it.
			panicked := serveRecovering(e, rec, req)
			assert.Nil(t, panicked,
				"registered route %s %s reached its handler without consulting the project guard", route.Method, path)
			assert.Equal(t, A2AContentType, rec.Header().Get(echo.HeaderContentType))

			switch {
			case path == "/.well-known/agent-card.json":
				// Tenant-blind and unauthenticated: it must not even ask.
				assert.Equal(t, http.StatusOK, rec.Code, "the global card must stay open")
			case strings.Contains(path, "pushNotificationConfigs"):
				// Declared unsupported for every caller and never project-addressed;
				// asserting the answer is what keeps it from being an exemption.
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"push-notification config route %s %s must stay not-supported", route.Method, path)
				assert.Contains(t, rec.Body.String(), "PUSH_NOTIFICATION_NOT_SUPPORTED")
			default:
				assert.Equal(t, http.StatusForbidden, rec.Code,
					"registered route %s %s must consult the project guard", route.Method, path)
				guarded++
			}
		})
	}

	// Guard against the test silently passing because no routes were walked.
	// The enumerated operations map onto six registered routes: cancel and
	// subscribe share POST /tasks/*.
	require.GreaterOrEqual(t, guarded, 6,
		"the structural sweep must cover every registered project-addressed A2A route")
}

// TestA2ARoutes_MemberReachesHandler proves the guard is an authorization gate
// and not a blanket denial: with the guard satisfied the request proceeds into
// the handler on every guarded operation.
func TestA2ARoutes_MemberReachesHandler(t *testing.T) {
	for _, op := range a2aGuardedOperations {
		t.Run(op.name, func(t *testing.T) {
			var calls []string
			e, _ := newA2ATestRouterWithAuthz(t, func(_ context.Context, projectID string) error {
				calls = append(calls, projectID)
				return nil
			})

			var body io.Reader
			if op.body != "" {
				body = strings.NewReader(op.body)
			}
			req := httptest.NewRequest(op.method, op.path, body)
			if op.body != "" {
				req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			}
			addA2AStandaloneAuth(req)
			rec := httptest.NewRecorder()

			// The handler has no repository, so reaching it surfaces as the
			// recovered nil-repo panic (routing and authorization both succeeded).
			panicked := serveRecovering(e, rec, req)
			require.NotNil(t, panicked,
				"%s %s must reach its handler once the guard passes, got status %d", op.method, op.path, rec.Code)
			require.Equal(t, []string{a2aTestProjectID}, calls)
		})
	}
}

// TestA2ARoutes_GlobalCardStaysOpen is the other half of #660: discovery must
// remain unauthenticated and must not consult project membership.
func TestA2ARoutes_GlobalCardStaysOpen(t *testing.T) {
	e, _ := newA2ATestRouterWithAuthz(t, func(context.Context, string) error {
		t.Fatal("the global agent card must not consult project membership")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil) // no auth headers
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, A2AContentType, rec.Header().Get(echo.HeaderContentType))

	var card AgentCard
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &card))
	assert.Equal(t, A2APlatformName, card.Name)
}
