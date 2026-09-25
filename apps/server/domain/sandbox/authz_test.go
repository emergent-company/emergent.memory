package sandbox_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

const (
	// memberAdminToken is a bare `admin` account token minted by a project
	// member — the escalation path a scope-only gate admits (#948/#949).
	memberAdminToken = "emt_test_959_sandbox_member_admin"
	// superadminToken maps to AdminUser, who holds an active superadmin_full grant.
	superadminToken = "e2e-test-user"
)

// newMCPHostingEcho wires a minimal Echo instance with only the /api/v1/mcp/hosted
// routes and the real auth middleware + MCP hosting handler.
func newMCPHostingEcho(t *testing.T, testDB *testutil.TestDB) *testutil.HTTPClient {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	dbc := testDB.GetDB()

	userSvc := auth.NewUserProfileService(dbc, log)
	am := auth.NewMiddleware(auth.MiddlewareParams{DB: dbc, Cfg: testDB.Config, Log: log, UserSvc: userSvc})

	store := sandbox.NewStore(dbc)
	orchestrator := sandbox.NewOrchestrator(log)
	svc := sandbox.NewService(store, orchestrator, log)
	hosting := sandbox.NewMCPHostingService(store, svc, orchestrator, log)
	handler := sandbox.NewMCPHostingHandler(hosting, log)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	sandbox.RegisterMCPHostingRoutes(e, handler, am, log)

	return testutil.NewHTTPClient(e)
}

// newAgentSandboxesEcho wires a minimal Echo instance with only the
// /api/v1/agent/sandboxes routes and the real auth middleware + handler.
func newAgentSandboxesEcho(t *testing.T, testDB *testutil.TestDB) *testutil.HTTPClient {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	dbc := testDB.GetDB()

	userSvc := auth.NewUserProfileService(dbc, log)
	am := auth.NewMiddleware(auth.MiddlewareParams{DB: dbc, Cfg: testDB.Config, Log: log, UserSvc: userSvc})

	store := sandbox.NewStore(dbc)
	orchestrator := sandbox.NewOrchestrator(log)
	svc := sandbox.NewService(store, orchestrator, log)
	handler := sandbox.NewHandler(svc, orchestrator, nil, nil, log)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	sandbox.RegisterRoutes(e, handler, am, log)

	return testutil.NewHTTPClient(e)
}

// TestMCPHostingAuthz proves the /api/v1/mcp/hosted group is platform-scoped
// (issue #959): it admits only an active superadmin_full principal, never a
// bare `admin` scope (mintable by any project member).
func TestMCPHostingAuthz(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "mcp_hosting_authz")
	defer testDB.Close()

	dbc := testDB.GetDB()
	require.NoError(t, testutil.SetupTestFixtures(ctx, dbc))

	// superadmin_full principal (AdminUser).
	_, err := dbc.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, testutil.AdminUser.ID).Exec(ctx)
	require.NoError(t, err)

	// A project member who mints a bare `admin` account token.
	memberID := uuid.New().String()
	require.NoError(t, testutil.CreateTestUser(ctx, dbc, testutil.TestUser{
		ID:            memberID,
		ZitadelUserID: "sandbox-member-admin-user",
		Email:         "sandboxmemberadmin@test.local",
	}))
	require.NoError(t, testutil.CreateTestAccountAPIToken(ctx, dbc, memberID, memberAdminToken, []string{"admin"}))

	client := newMCPHostingEcho(t, testDB)

	t.Run("list refused for member admin token", func(t *testing.T) {
		resp := client.GET("/api/v1/mcp/hosted", testutil.WithAuth(memberAdminToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"list by bare admin token must be 403, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("register refused for member admin token", func(t *testing.T) {
		resp := client.POST("/api/v1/mcp/hosted",
			testutil.WithAuth(memberAdminToken),
			testutil.WithJSONBody(map[string]any{"name": "x", "image": "x:latest"}))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"register by bare admin token must be 403, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("non-admin refused", func(t *testing.T) {
		resp := client.GET("/api/v1/mcp/hosted", testutil.WithAuth("no-scope"))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"list by non-admin must be 403, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("list allowed for superadmin_full", func(t *testing.T) {
		resp := client.GET("/api/v1/mcp/hosted", testutil.WithAuth(superadminToken))
		require.Equal(t, http.StatusOK, resp.StatusCode,
			"list by superadmin_full must be 200, got %d: %s", resp.StatusCode, resp.String())
	})
}

// TestAgentSandboxesAuthz proves the sibling /api/v1/agent/sandboxes group is
// likewise platform-scoped: bare `admin` is refused, superadmin_full is admitted.
func TestAgentSandboxesAuthz(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "agent_sandboxes_authz")
	defer testDB.Close()

	dbc := testDB.GetDB()
	require.NoError(t, testutil.SetupTestFixtures(ctx, dbc))

	// superadmin_full principal (AdminUser).
	_, err := dbc.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, testutil.AdminUser.ID).Exec(ctx)
	require.NoError(t, err)

	// A project member who mints a bare `admin` account token.
	memberID := uuid.New().String()
	require.NoError(t, testutil.CreateTestUser(ctx, dbc, testutil.TestUser{
		ID:            memberID,
		ZitadelUserID: "sandbox-member-admin-user",
		Email:         "sandboxmemberadmin@test.local",
	}))
	require.NoError(t, testutil.CreateTestAccountAPIToken(ctx, dbc, memberID, memberAdminToken, []string{"admin"}))

	client := newAgentSandboxesEcho(t, testDB)

	t.Run("list refused for member admin token", func(t *testing.T) {
		resp := client.GET("/api/v1/agent/sandboxes", testutil.WithAuth(memberAdminToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"list by bare admin token must be 403, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("list allowed for superadmin_full", func(t *testing.T) {
		resp := client.GET("/api/v1/agent/sandboxes", testutil.WithAuth(superadminToken))
		require.Equal(t, http.StatusOK, resp.StatusCode,
			"list by superadmin_full must be 200, got %d: %s", resp.StatusCode, resp.String())
	})
}
