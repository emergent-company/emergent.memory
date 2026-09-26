package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestCreateWebhookHook_RefusesInternalVisibility is the fail-first regression
// for issue #1004: nothing prevented binding a public webhook hook to an
// internal-visibility agent. The webhook receiver (POST /api/webhooks/agents/
// :hookId) has no RequireAuth — only a per-hook bearer token shared with third
// parties — so such a binding lets a holder of that secret invoke an agent meant
// to be reachable only from within the platform. Binding is now fail-closed:
// an internal target is refused by default, an explicit opt-in is honoured, and
// a normal (project) visibility target still binds.
func TestCreateWebhookHook_RefusesInternalVisibility(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_hook_visibility")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	insertAgentDefinition(t, tdb.DB, ctx, projectID, "internal-hook", string(VisibilityInternal))
	internalAgentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "internal-hook")

	insertAgentDefinition(t, tdb.DB, ctx, projectID, "project-hook", string(VisibilityProject))
	projectAgentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "project-hook")

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo}

	// 1. Internal-visibility target: refused by default (403).
	resp := callCreateWebhookHook(t, h, projectID, internalAgentID, `{"label":"internal"}`)
	require.Equal(t, http.StatusForbidden, resp.Code,
		"binding a hook to an internal agent must be refused by default")

	// 2. Explicit opt-in: allowed.
	resp = callCreateWebhookHook(t, h, projectID, internalAgentID, `{"label":"internal-optin","allowInternal":true}`)
	require.Equal(t, http.StatusCreated, resp.Code,
		"binding a hook to an internal agent with allowInternal must succeed")

	// 3. Normal (project) visibility target: allowed.
	resp = callCreateWebhookHook(t, h, projectID, projectAgentID, `{"label":"project"}`)
	require.Equal(t, http.StatusCreated, resp.Code,
		"binding a hook to a project-visibility agent must succeed")
}

// callCreateWebhookHook drives h.CreateWebhookHook directly with an
// authenticated user scoped to projectID, and returns the recorder. A returned
// *apperror.Error is mapped onto the recorder's status so callers can assert on
// the HTTP code the Echo error handler would render.
func callCreateWebhookHook(t *testing.T, h *Handler, projectID, agentID, body string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/agents/"+agentID+"/hooks", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/projects/:projectId/agents/:id/hooks")
	c.SetParamNames("projectId", "id")
	c.SetParamValues(projectID, agentID)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "user-test-id", ProjectID: projectID})

	if err := h.CreateWebhookHook(c); err != nil {
		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr)
		rec.Code = appErr.HTTPStatus
	}
	return rec
}
