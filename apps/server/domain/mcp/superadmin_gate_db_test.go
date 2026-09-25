package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// fakeEmbeddingCtl is a minimal EmbeddingControlHandler that records PauseAll /
// ResumeAll so the operator tool can be executed without the extraction worker
// graph.
type fakeEmbeddingCtl struct {
	paused bool
	status EmbeddingStatusSnapshot
}

func (f *fakeEmbeddingCtl) CurrentStatus() EmbeddingStatusSnapshot { return f.status }
func (f *fakeEmbeddingCtl) PauseAll()                              { f.paused = true }
func (f *fakeEmbeddingCtl) ResumeAll()                             { f.paused = false }
func (f *fakeEmbeddingCtl) ApplyConfig(EmbeddingConfigUpdate)      {}

// newGateHandler builds a Handler whose Service carries a real DB (for the
// superadmin role lookup) and the fake embedding controller, and pre-populates
// the package tool index so GetToolByName resolves.
func newGateHandler(db bun.IDB, embeddingCtl EmbeddingControlHandler) *Handler {
	svc := &Service{db: db, embeddingCtl: embeddingCtl}
	_ = svc.GetToolDefinitions() // populates toolIndex
	return NewHandler(svc, slog.New(slog.NewTextHandler(os.Stderr, nil)), nil)
}

// gateContext builds an echo context for a tools/call request carrying the given
// bearer token and injects the auth user into the request context so
// IsSuperadminCaller can resolve it.
func gateContext(user *auth.AuthUser, token string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-Key", token)
	req = req.WithContext(auth.InjectAuthContext(req.Context(), user))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), user)
	return c, rec
}

// setupGateDB seeds a throwaway database with a project-member user (no
// superadmin grant) and a superadmin_full principal, returning the DB and their
// user IDs.
func setupGateDB(t *testing.T) (*bun.DB, string, string) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mcp_superadmin_gate")
	t.Cleanup(tdb.Close)
	dbc := tdb.DB

	memberID := uuid.New().String()
	_, err := dbc.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id) VALUES (?, ?)`,
		memberID, "gate-member-user").Exec(ctx)
	require.NoError(t, err)

	superID := uuid.New().String()
	_, err = dbc.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id) VALUES (?, ?)`,
		superID, "gate-super-user").Exec(ctx)
	require.NoError(t, err)
	_, err = dbc.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, superID).Exec(ctx)
	require.NoError(t, err)

	return dbc, memberID, superID
}

// TestSuperadminGateOnOperatorTools proves that deployment-wide operator tools
// (embedding-pause) and org-level provider tools (provider-configure-org) are
// gated on the superadmin_full authority (core.superadmins), not on a bare
// `admin` token scope. A project member carrying a bare admin token is refused;
// a superadmin_full principal succeeds even though its token also carries only
// the bare admin scope (proving the scope is not the authority).
func TestSuperadminGateOnOperatorTools(t *testing.T) {
	dbc, memberID, superID := setupGateDB(t)

	h := newGateHandler(dbc, &fakeEmbeddingCtl{status: EmbeddingStatusSnapshot{}})

	memberUser := &auth.AuthUser{ID: memberID, Scopes: []string{"admin"}, ProjectID: uuid.New().String()}
	superUser := &auth.AuthUser{ID: superID, Scopes: []string{"admin"}, ProjectID: uuid.New().String()}

	call := func(t *testing.T, user *auth.AuthUser, tool string) *Response {
		t.Helper()
		token := "tok-" + user.ID
		h.sessionsMu.Lock()
		h.sessions[token] = &Session{Initialized: true, ProjectID: user.ProjectID}
		h.sessionsMu.Unlock()

		c, _ := gateContext(user, token)
		params, err := json.Marshal(ToolsCallParams{Name: tool, Arguments: map[string]any{}})
		require.NoError(t, err)
		req := &Request{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}
		return h.handleToolsCall(c, req, user)
	}

	t.Run("project member bare-admin is refused on embedding-pause", func(t *testing.T) {
		resp := call(t, memberUser, "embedding-pause")
		require.NotNil(t, resp.Error, "project member with bare admin scope must be refused, got result %v", resp.Result)
		require.Equal(t, ErrCodeMethodNotFound, resp.Error.Code,
			"refusal must not reveal the tool's existence (method-not-found), got code %d: %s", resp.Error.Code, resp.Error.Message)
	})

	t.Run("project member bare-admin is refused on provider-configure-org", func(t *testing.T) {
		resp := call(t, memberUser, "provider-configure-org")
		require.NotNil(t, resp.Error, "project member with bare admin scope must be refused, got result %v", resp.Result)
		require.Equal(t, ErrCodeMethodNotFound, resp.Error.Code,
			"refusal must not reveal the tool's existence (method-not-found), got code %d: %s", resp.Error.Code, resp.Error.Message)
	})

	t.Run("superadmin_full succeeds on embedding-pause", func(t *testing.T) {
		resp := call(t, superUser, "embedding-pause")
		require.Nil(t, resp.Error, "superadmin_full must execute embedding-pause, got error %+v", resp.Error)
	})

	t.Run("superadmin_full succeeds on provider-usage-get", func(t *testing.T) {
		resp := call(t, superUser, "provider-usage-get")
		require.Nil(t, resp.Error, "superadmin_full must execute provider-usage-get, got error %+v", resp.Error)
	})
}

// TestExecuteToolSuperadminGate proves the in-process dispatch path — the one
// the ADK ToolPool reaches during an agent run — enforces the superadmin_full
// authority directly in Service.ExecuteTool. This is the bypass the HTTP-only
// gate missed: ExecuteTool is the single dispatch point for every transport AND
// the in-process agent ToolPool, so the check must live here (issue #948).
func TestExecuteToolSuperadminGate(t *testing.T) {
	dbc, memberID, superID := setupGateDB(t)

	ctl := &fakeEmbeddingCtl{status: EmbeddingStatusSnapshot{}}
	svc := &Service{db: dbc, embeddingCtl: ctl}

	memberCtx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: memberID, Scopes: []string{"admin"}})
	superCtx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: superID, Scopes: []string{"admin"}})
	projectID := uuid.New().String()

	t.Run("project member bare-admin is refused on embedding-pause (in-process)", func(t *testing.T) {
		_, err := svc.ExecuteTool(memberCtx, projectID, "embedding-pause", map[string]any{})
		require.Error(t, err, "in-process embedding-pause by a project member must be refused")
		require.False(t, ctl.paused, "pause must not execute for a non-superadmin principal")
	})

	t.Run("project member bare-admin is refused on provider-configure-org (in-process)", func(t *testing.T) {
		_, err := svc.ExecuteTool(memberCtx, projectID, "provider-configure-org", map[string]any{})
		require.Error(t, err, "in-process provider-configure-org by a project member must be refused")
	})

	t.Run("no principal fails closed (in-process)", func(t *testing.T) {
		_, err := svc.ExecuteTool(context.Background(), projectID, "embedding-pause", map[string]any{})
		require.Error(t, err, "a principal-less in-process dispatch must fail closed")
	})

	t.Run("superadmin_full succeeds on embedding-pause (in-process)", func(t *testing.T) {
		_, err := svc.ExecuteTool(superCtx, projectID, "embedding-pause", map[string]any{})
		require.NoError(t, err, "superadmin_full must execute embedding-pause in-process")
		require.True(t, ctl.paused, "pause must execute for a superadmin_full principal")
	})

	t.Run("superadmin_full succeeds on provider-usage-get (in-process)", func(t *testing.T) {
		_, err := svc.ExecuteTool(superCtx, projectID, "provider-usage-get", map[string]any{})
		require.NoError(t, err, "superadmin_full must execute provider-usage-get in-process")
	})
}
