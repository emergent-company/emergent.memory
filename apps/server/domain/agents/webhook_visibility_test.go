package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

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

// TestReceiveWebhook_RefusesInternalVisibility is the fail-first regression for
// issue #1051(c): a webhook hook bound to an internal-visibility agent before
// the #1004 binding guard shipped is still in the database and still reachable
// on the public receiver (POST /api/webhooks/agents/:hookId). Invocation must
// now fail closed — the receiver refuses (403) and creates no run — while a hook
// bound to a normal (project) visibility agent is still invoked exactly as
// before (202, a run is created).
func TestReceiveWebhook_RefusesInternalVisibility(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_receivewebhook_visibility")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	// Internal-visibility target. The hook rows are inserted directly (creation
	// is now guarded by #1004), modelling hooks bound before the guard shipped
	// (allow_internal=false) and a deliberately opted-in hook (allow_internal=true).
	insertAgentDefinition(t, tdb.DB, ctx, projectID, "internal-target", string(VisibilityInternal))
	internalAgentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "internal-target")
	internalHookID := insertWebhookHook(t, tdb.DB, ctx, internalAgentID, projectID, "whk_vis_internal_token")
	internalOptInHookID := insertWebhookHookWithAllowInternal(t, tdb.DB, ctx, internalAgentID, projectID, "whk_vis_internal_optin_token", true)

	// Normal (project) visibility target: must remain invocable.
	insertAgentDefinition(t, tdb.DB, ctx, projectID, "project-target", string(VisibilityProject))
	projectAgentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "project-target")
	projectHookID := insertWebhookHook(t, tdb.DB, ctx, projectAgentID, projectID, "whk_vis_project_token")

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo, executor: trustTestExecutor(repo)}

	// 1. Internal target (not opted in): refused (403), no run created.
	rec := callReceiveWebhook(t, h, internalHookID, "whk_vis_internal_token")
	require.Equal(t, http.StatusForbidden, rec.Code,
		"invoking a hook bound to an internal-visibility agent must be refused")
	require.Equal(t, 0, countRunsByTriggerSource(t, tdb.DB, ctx, "webhook:"+internalHookID),
		"no run must be created for a refused internal-visibility hook")

	// 2. Internal target (explicitly opted in): the opt-in persists on the hook
	// row and invocation must agree with create-time — allowed (202), one run.
	rec = callReceiveWebhook(t, h, internalOptInHookID, "whk_vis_internal_optin_token")
	require.Equal(t, http.StatusAccepted, rec.Code,
		"invoking an opted-in hook bound to an internal-visibility agent must succeed")
	require.Equal(t, 1, countRunsByTriggerSource(t, tdb.DB, ctx, "webhook:"+internalOptInHookID),
		"a run must be created for an opted-in internal-visibility hook")

	// 3. Normal target: still invoked (202), a run is created.
	rec = callReceiveWebhook(t, h, projectHookID, "whk_vis_project_token")
	require.Equal(t, http.StatusAccepted, rec.Code,
		"invoking a hook bound to a project-visibility agent must still succeed")
	require.Equal(t, 1, countRunsByTriggerSource(t, tdb.DB, ctx, "webhook:"+projectHookID),
		"a run must be created for a project-visibility hook")
}

// callReceiveWebhook drives h.ReceiveWebhook directly with a Bearer token and
// returns the recorder. A returned *apperror.Error is mapped onto the recorder's
// status so callers can assert on the HTTP code the Echo error handler renders.
func callReceiveWebhook(t *testing.T, h *Handler, hookID, token string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/agents/"+hookID, strings.NewReader(`{"prompt":"hi"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/webhooks/agents/:hookId")
	c.SetParamNames("hookId")
	c.SetParamValues(hookID)

	if err := h.ReceiveWebhook(c); err != nil {
		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr)
		rec.Code = appErr.HTTPStatus
	}
	return rec
}

// countRunsByTriggerSource returns the number of persisted runs with the given
// trigger source, so tests can assert on whether a webhook invocation actually
// created a run (not merely on the error text).
func countRunsByTriggerSource(t *testing.T, db *bun.DB, ctx context.Context, triggerSource string) int {
	t.Helper()
	var count int
	err := db.NewRaw(`SELECT count(*) FROM kb.agent_runs WHERE trigger_source = ?`, triggerSource).Scan(ctx, &count)
	require.NoError(t, err)
	return count
}

// insertWebhookHookWithAllowInternal inserts an enabled webhook hook bound to
// agentID with the given plaintext token and an explicit allow_internal value,
// so tests can model both a stale pre-#1004 hook (false) and a deliberately
// opted-in hook (true).
func insertWebhookHookWithAllowInternal(t *testing.T, db *bun.DB, ctx context.Context, agentID, projectID, token string, allowInternal bool) string {
	t.Helper()
	hash, err := HashWebhookToken(token)
	require.NoError(t, err)
	id := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.agent_webhook_hooks (id, agent_id, project_id, label, token_hash, enabled, allow_internal) VALUES (?, ?, ?, ?, ?, true, ?)`,
		id, agentID, projectID, "test-hook", hash, allowInternal)
	require.NoError(t, err)
	return id
}

// TestReceiveWebhook_ResolutionError_FailsClosed is the fail-first regression
// for issue #1051(c) finding 2: ReceiveWebhook used to discard the error from
// ResolveDefinitionForAgent (`agentDef, _ = ...`), so a transient DB error left
// agentDef nil and the stale hook executed unchecked — the exact hole the
// visibility check is meant to close. Invocation must fail closed: a definition
// resolution failure refuses the invocation (500) and creates no run, matching
// the create-time binding guard, which already returns 500 on the same error.
func TestReceiveWebhook_ResolutionError_FailsClosed(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_receivewebhook_resolve_err")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "plain-agent")
	hookID := insertWebhookHook(t, tdb.DB, ctx, agentID, projectID, "whk_vis_resolve_err_token")

	// Fault injection: make the agent-definition resolution query fail with a
	// real error (relation does not exist) while the hook and runtime-agent
	// lookups still succeed. RENAME TO takes an unqualified name, so the table
	// stays in kb. and the old kb.agent_definitions name becomes unresolvable.
	_, err := tdb.DB.ExecContext(ctx, `ALTER TABLE kb.agent_definitions RENAME TO agent_definitions_hidden`)
	require.NoError(t, err)

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo, executor: trustTestExecutor(repo)}

	rec := callReceiveWebhook(t, h, hookID, "whk_vis_resolve_err_token")
	require.Equal(t, http.StatusInternalServerError, rec.Code,
		"a definition resolution failure must refuse the invocation (500), never execute")
	require.Equal(t, 0, countRunsByTriggerSource(t, tdb.DB, ctx, "webhook:"+hookID),
		"no run must be created when definition resolution fails")
}
