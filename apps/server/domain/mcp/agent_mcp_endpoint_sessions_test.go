package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addEndpointFixtureSession inserts a session on the fixture's endpoint ep-1 and
// returns it. lastActiveAt drives the list ordering.
func addEndpointFixtureSession(t *testing.T, f *endpointTestFixture, ref, keyID, status string, lastActiveAt time.Time) *AgentMCPSession {
	t.Helper()
	sess := &AgentMCPSession{
		ID:           uuid.NewString(),
		EndpointID:   "ep-1",
		KeyID:        keyID,
		SessionRef:   ref,
		Status:       status,
		TurnCount:    3,
		TotalSteps:   42,
		CreatedAt:    lastActiveAt.Add(-time.Hour),
		LastActiveAt: lastActiveAt,
	}
	require.NoError(t, f.sessions.CreateSession(context.Background(), sess))
	return sess
}

// ============================================================================
// Service
// ============================================================================

func TestListAgentSessionsHappyPath(t *testing.T) {
	f := newEndpointTestFixture()
	f.sessions.labels["key-1"] = "ci-runner"

	older := time.Now().UTC().Add(-2 * time.Hour)
	newer := time.Now().UTC().Add(-1 * time.Minute)
	addEndpointFixtureSession(t, f, "ref-old", "key-1", AgentMCPSessionStatusActive, older)
	addEndpointFixtureSession(t, f, "ref-new", "key-1", AgentMCPSessionStatusActive, newer)

	resp, err := f.svc.ListAgentSessions(context.Background(), "proj-1", "user-1", "ep-1", "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Sessions, 2)

	// Ordered by last_active_at DESC.
	assert.Equal(t, "ref-new", resp.Sessions[0].SessionID)
	assert.Equal(t, "ref-old", resp.Sessions[1].SessionID)

	first := resp.Sessions[0]
	assert.Equal(t, "key-1", first.KeyID)
	assert.Equal(t, "ci-runner", first.KeyLabel)
	assert.Equal(t, AgentMCPSessionStatusActive, first.Status)
	assert.Equal(t, 3, first.TurnCount)
	assert.Equal(t, 42, first.TotalSteps)
	assert.WithinDuration(t, newer, first.LastActiveAt, time.Second)
	assert.Nil(t, first.ExpiresAt)
}

func TestListAgentSessionsEmptyListIsNotNull(t *testing.T) {
	f := newEndpointTestFixture()
	resp, err := f.svc.ListAgentSessions(context.Background(), "proj-1", "user-1", "ep-1", "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.Sessions, "sessions must be an empty slice, never null")
	assert.Empty(t, resp.Sessions)

	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"sessions":[]`)
}

func TestListAgentSessionsCrossProjectNotFound(t *testing.T) {
	f := newEndpointTestFixture()
	addEndpointFixtureSession(t, f, "ref-1", "key-1", AgentMCPSessionStatusActive, time.Now().UTC())

	resp, err := f.svc.ListAgentSessions(context.Background(), "proj-2", "user-1", "ep-1", "")
	assertAppError(t, err, http.StatusNotFound)
	assert.Nil(t, resp)
}

func TestListAgentSessionsNonAdminForbidden(t *testing.T) {
	f := newEndpointTestFixture()
	f.tokenSvc.role = "project_user"
	resp, err := f.svc.ListAgentSessions(context.Background(), "proj-1", "user-1", "ep-1", "")
	assertAppError(t, err, http.StatusForbidden)
	assert.Nil(t, resp)
}

func TestListAgentSessionsStatusFilter(t *testing.T) {
	f := newEndpointTestFixture()
	now := time.Now().UTC()
	addEndpointFixtureSession(t, f, "ref-active", "key-1", AgentMCPSessionStatusActive, now)
	addEndpointFixtureSession(t, f, "ref-expired", "key-1", AgentMCPSessionStatusExpired, now.Add(-time.Minute))

	resp, err := f.svc.ListAgentSessions(context.Background(), "proj-1", "user-1", "ep-1", AgentMCPSessionStatusActive)
	require.NoError(t, err)
	require.Len(t, resp.Sessions, 1)
	assert.Equal(t, "ref-active", resp.Sessions[0].SessionID)

	resp, err = f.svc.ListAgentSessions(context.Background(), "proj-1", "user-1", "ep-1", AgentMCPSessionStatusExpired)
	require.NoError(t, err)
	require.Len(t, resp.Sessions, 1)
	assert.Equal(t, "ref-expired", resp.Sessions[0].SessionID)
}

func TestListAgentSessionsInvalidStatusBadRequest(t *testing.T) {
	f := newEndpointTestFixture()
	_, err := f.svc.ListAgentSessions(context.Background(), "proj-1", "user-1", "ep-1", "bogus")
	assertAppError(t, err, http.StatusBadRequest)
}

// TestListAgentSessionsEndpointDecoupledFromKeyScope proves the admin list is
// endpoint-scoped: sessions from a second key on the same endpoint are included,
// while another endpoint's sessions are not.
func TestListAgentSessionsEndpointScoped(t *testing.T) {
	f := newEndpointTestFixture()
	f.endpoints.byID["ep-2"] = &AgentMCPEndpoint{
		ID: "ep-2", ProjectID: "proj-1", AgentID: agentA, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	f.keys.byID["key-2"] = &AgentMCPKey{
		ID: "key-2", EndpointID: "ep-2", TokenID: "tok-2", Label: "other",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	now := time.Now().UTC()
	addEndpointFixtureSession(t, f, "ref-ep1-a", "key-1", AgentMCPSessionStatusActive, now)
	addEndpointFixtureSession(t, f, "ref-ep1-b", "key-2", AgentMCPSessionStatusActive, now.Add(-time.Minute))
	// Move ref-ep1-b onto ep-2 to prove endpoint scoping excludes it.
	f.sessions.byRef("ref-ep1-b").EndpointID = "ep-2"

	resp, err := f.svc.ListAgentSessions(context.Background(), "proj-1", "user-1", "ep-1", "")
	require.NoError(t, err)
	require.Len(t, resp.Sessions, 1)
	assert.Equal(t, "ref-ep1-a", resp.Sessions[0].SessionID)
}

// ============================================================================
// Handler
// ============================================================================

func TestHandleListAgentSessionsHappyPath(t *testing.T) {
	f := newEndpointTestFixture()
	f.sessions.labels["key-1"] = "ci-runner"
	expires := time.Now().UTC().Add(24 * time.Hour)
	sess := addEndpointFixtureSession(t, f, "ref-1", "key-1", AgentMCPSessionStatusActive, time.Now().UTC())
	sess.ExpiresAt = &expires

	h := NewHandler(f.svc, slog.Default(), nil)
	c, rec := newJSONContext(t, http.MethodGet, "", adminUser(), []string{"projectId", "id"}, []string{"proj-1", "ep-1"})
	require.NoError(t, h.HandleListAgentSessions(*c))
	require.Equal(t, http.StatusOK, rec.Code)

	// Wire contract: snake_case keys, key label, no message content.
	body := rec.Body.String()
	assert.Contains(t, body, `"sessions"`)
	assert.Contains(t, body, `"session_id"`)
	assert.Contains(t, body, `"turn_count"`)
	assert.Contains(t, body, `"last_active_at"`)
	assert.NotContains(t, body, "sessionId")
	assert.NotContains(t, body, "message")

	var resp AgentMCPSessionListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Sessions, 1)
	assert.Equal(t, "ref-1", resp.Sessions[0].SessionID)
	assert.Equal(t, "ci-runner", resp.Sessions[0].KeyLabel)
	require.NotNil(t, resp.Sessions[0].ExpiresAt)
}

func TestHandleListAgentSessionsEmpty(t *testing.T) {
	f := newEndpointTestFixture()
	h := NewHandler(f.svc, slog.Default(), nil)
	c, rec := newJSONContext(t, http.MethodGet, "", adminUser(), []string{"projectId", "id"}, []string{"proj-1", "ep-1"})
	require.NoError(t, h.HandleListAgentSessions(*c))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"sessions":[]`)
}

func TestHandleListAgentSessionsInvalidStatus(t *testing.T) {
	f := newEndpointTestFixture()
	h := NewHandler(f.svc, slog.Default(), nil)
	c, _ := newJSONContext(t, http.MethodGet, "", adminUser(), []string{"projectId", "id"}, []string{"proj-1", "ep-1"})
	(*c).Request().URL.RawQuery = "status=bogus"
	err := h.HandleListAgentSessions(*c)
	assertAppError(t, err, http.StatusBadRequest)
}

func TestHandleListAgentSessionsCrossProjectNotFound(t *testing.T) {
	f := newEndpointTestFixture()
	h := NewHandler(f.svc, slog.Default(), nil)
	c, _ := newJSONContext(t, http.MethodGet, "", adminUser(), []string{"projectId", "id"}, []string{"proj-2", "ep-1"})
	err := h.HandleListAgentSessions(*c)
	assertAppError(t, err, http.StatusNotFound)
}

func TestHandleListAgentSessionsNonAdminForbidden(t *testing.T) {
	f := newEndpointTestFixture()
	f.tokenSvc.role = "project_user"
	h := NewHandler(f.svc, slog.Default(), nil)
	c, _ := newJSONContext(t, http.MethodGet, "", adminUser(), []string{"projectId", "id"}, []string{"proj-1", "ep-1"})
	err := h.HandleListAgentSessions(*c)
	assertAppError(t, err, http.StatusForbidden)
}

// TestAgentMCPEndpointSessionsRouteRegistered pins the new route's registration.
func TestAgentMCPEndpointSessionsRouteRegistered(t *testing.T) {
	e := registerTestRoutes(t)
	assert.True(t, routeSet(e)["GET /api/projects/:projectId/agent-mcp-endpoints/:id/sessions"])

	// An unauthenticated request reaches the auth middleware (401) rather than
	// the router (404), proving the path+method is registered.
	code := serveRoute(e, http.MethodGet, "/api/projects/proj-1/agent-mcp-endpoints/ep-1/sessions")
	assert.Equal(t, http.StatusUnauthorized, code, "the sessions route must sit behind auth")
}
