package mcp

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func registerTestRoutes(t *testing.T) *echo.Echo {
	t.Helper()
	e := echo.New()
	svc := &Service{}
	h := NewHandler(svc, slog.Default(), nil)
	sse := NewSSEHandler(svc, h, slog.Default())
	stream := NewStreamableHTTPHandler(svc, slog.Default())
	ep := NewAgentEndpointHandler(svc, slog.Default())
	mw := auth.NewMiddleware(auth.MiddlewareParams{Cfg: &config.Config{}, Log: slog.Default()})
	RegisterRoutes(e, h, sse, stream, ep, mw)
	return e
}

func serveRoute(e *echo.Echo, method, path string) int {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Code
}

// TestAgentEndpointKeyRoutesRegistered resolves every route in the endpoint/key
// CRUD surface. An unauthenticated request reaches the auth middleware (401)
// rather than the router (404/405), proving the path+method is registered.
func TestAgentEndpointKeyRoutesRegistered(t *testing.T) {
	e := registerTestRoutes(t)
	routes := routeSet(e)
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/projects/proj-1/agents/" + agentA + "/mcp-endpoint"},
		{http.MethodGet, "/api/projects/proj-1/agents/" + agentA + "/mcp-endpoint"},
		{http.MethodDelete, "/api/projects/proj-1/agent-mcp-endpoints/ep-1"},
		{http.MethodPost, "/api/projects/proj-1/agent-mcp-endpoints/ep-1/keys"},
		{http.MethodGet, "/api/projects/proj-1/agent-mcp-endpoints/ep-1/keys"},
		{http.MethodDelete, "/api/projects/proj-1/agent-mcp-keys/key-1"},
		{http.MethodPost, "/api/projects/proj-1/agent-mcp-keys/key-1/rotate"},
	}
	for _, tc := range cases {
		code := serveRoute(e, tc.method, tc.path)
		assert.Equal(t, http.StatusUnauthorized, code, "%s %s must sit behind auth", tc.method, tc.path)
	}

	want := []string{
		"POST /api/projects/:projectId/agents/:agentId/mcp-endpoint",
		"GET /api/projects/:projectId/agents/:agentId/mcp-endpoint",
		"DELETE /api/projects/:projectId/agent-mcp-endpoints/:id",
		"POST /api/projects/:projectId/agent-mcp-endpoints/:id/keys",
		"GET /api/projects/:projectId/agent-mcp-endpoints/:id/keys",
		"DELETE /api/projects/:projectId/agent-mcp-keys/:id",
		"POST /api/projects/:projectId/agent-mcp-keys/:id/rotate",
	}
	for _, route := range want {
		assert.True(t, routes[route], "route %q must be registered", route)
	}
}

// TestRetiredAgentShareRoutesGone pins the retirement of the single-share
// credential surface against the route table.
func TestRetiredAgentShareRoutesGone(t *testing.T) {
	routes := routeSet(registerTestRoutes(t))
	retired := []string{
		"POST /api/projects/:projectId/agents/:id/mcp-share",
		"GET /api/projects/:projectId/agents/:id/mcp-shares",
		"GET /api/projects/:projectId/agent-mcp-shares",
		"DELETE /api/projects/:projectId/agent-mcp-shares/:id",
		"POST /api/projects/:projectId/agent-mcp-shares/:id/rotate",
	}
	for _, route := range retired {
		assert.False(t, routes[route], "retired route %q must not be registered", route)
	}
}

func routeSet(e *echo.Echo) map[string]bool {
	out := map[string]bool{}
	for _, r := range e.Routes() {
		out[r.Method+" "+r.Path] = true
	}
	return out
}
