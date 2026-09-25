package health_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/health"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
	"github.com/emergent-company/emergent.memory/pkg/kreuzberg"
	"github.com/emergent-company/emergent.memory/pkg/whisper"
)

const (
	noScopeToken     = "no-scope"            // NoScopeUser: empty scopes, no membership
	superadminToken  = "e2e-test-user"       // AdminUser: all scopes + core.superadmins superadmin_full
	orgAdminToken    = "emt_diag_org_admin"  // admin:all account token minted for an org_admin (the escalation path)
	orgAdminUserName = "diag-org-admin-user" // dedicated org_admin profile
	orgAdminUserID   = "00000000-0000-0000-0000-000000000999"
	orgID            = "00000000-0000-0000-0000-000000000888"
)

// newDiagnosticsEcho wires a minimal Echo instance with only the health routes
// and the real auth middleware + handler, so the /api/diagnostics authorization
// posture can be exercised over HTTP without the full in-process server.
func newDiagnosticsEcho(t *testing.T, testDB *testutil.TestDB) *testutil.HTTPClient {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	dbc := testDB.GetDB()

	userSvc := auth.NewUserProfileService(dbc, log)
	am := auth.NewMiddleware(auth.MiddlewareParams{DB: dbc, Cfg: testDB.Config, Log: log, UserSvc: userSvc})

	h := health.NewHandler(
		testDB.Pool,
		testDB.Config,
		&storage.Service{},
		&kreuzberg.Client{},
		&whisper.Client{},
		&embeddings.Service{},
	)
	m := health.NewMetricsHandler(testDB.DB)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	health.RegisterRoutes(e, h, m, am)

	return testutil.NewHTTPClient(e)
}

// TestDiagnosticsAuthz proves GET /api/diagnostics is gated on the role-derived
// superadmin_full seam: unauthenticated callers get 401, authenticated
// non-superadmins get 403, and only an active superadmin_full principal reaches
// the handler (200). It specifically proves that an org_admin-minted admin:all
// token does NOT satisfy the gate — the platform-admin authority is the
// core.superadmins role, not a scope (issue #940 class; #947/#958 convention).
func TestDiagnosticsAuthz(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "diagnostics_authz")
	defer testDB.Close()

	dbc := testDB.GetDB()
	require.NoError(t, testutil.SetupTestFixtures(ctx, dbc))

	// superadmin_full principal: AdminUser holds an active superadmin_full grant.
	_, err := dbc.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, testutil.AdminUser.ID).Exec(ctx)
	require.NoError(t, err)

	// org_admin escalation principal: a user who is an org_admin and carries an
	// admin:all account token — the exact path a scope gate would admit.
	require.NoError(t, testutil.CreateTestUser(ctx, dbc, testutil.TestUser{
		ID:            orgAdminUserID,
		ZitadelUserID: orgAdminUserName,
		Email:         "diag-orgadmin@test.local",
	}))
	require.NoError(t, testutil.CreateTestOrganization(ctx, dbc, orgID, "Diag Org"))
	require.NoError(t, testutil.CreateTestOrgMembership(ctx, dbc, orgID, orgAdminUserID, "org_admin"))
	require.NoError(t, testutil.CreateTestAccountAPIToken(ctx, dbc, orgAdminUserID, orgAdminToken, []string{"admin:all"}))

	client := newDiagnosticsEcho(t, testDB)

	t.Run("unauthenticated -> 401", func(t *testing.T) {
		resp := client.GET("/api/diagnostics")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
			"unauthenticated /api/diagnostics must be 401, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("authenticated non-superadmin -> 403", func(t *testing.T) {
		resp := client.GET("/api/diagnostics", testutil.WithAuth(noScopeToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"non-superadmin /api/diagnostics must be 403, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("org_admin admin:all token -> 403", func(t *testing.T) {
		resp := client.GET("/api/diagnostics", testutil.WithAuth(orgAdminToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"org_admin admin:all token must NOT satisfy the gate (403), got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("superadmin_full -> 200", func(t *testing.T) {
		resp := client.GET("/api/diagnostics", testutil.WithAuth(superadminToken))
		require.Equal(t, http.StatusOK, resp.StatusCode,
			"superadmin_full /api/diagnostics must be 200, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("sibling /debug gated identically", func(t *testing.T) {
		resp := client.GET("/debug")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
			"unauthenticated /debug must be 401, got %d: %s", resp.StatusCode, resp.String())

		resp = client.GET("/debug", testutil.WithAuth(noScopeToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"non-superadmin /debug must be 403, got %d: %s", resp.StatusCode, resp.String())
	})
}

// TestPlatformGateDoesNotLeakToUnrelatedPaths is the regression guard for the
// Echo v4.15 group-middleware catch-all: registering the platform gate under an
// EMPTY-prefix group (e.Group("")) auto-registers a global "/*" RouteNotFound
// catch-all, so RequireAuth + RequireSuperadminFull would run on every
// otherwise-unmatched request and leak 401/403 onto unrelated paths (including
// the moved /api/admin/agents cancel route, which then stopped returning 404).
// With the gate registered under its own non-empty prefix, unrelated paths must
// fall through to the normal 404.
func TestPlatformGateDoesNotLeakToUnrelatedPaths(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "diagnostics_gate_no_leak")
	defer testDB.Close()

	dbc := testDB.GetDB()
	require.NoError(t, testutil.SetupTestFixtures(ctx, dbc))
	_, err := dbc.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, testutil.AdminUser.ID).Exec(ctx)
	require.NoError(t, err)

	client := newDiagnosticsEcho(t, testDB)

	// Unrelated unknown path must 404 — for both an unauthenticated caller AND
	// an active superadmin_full caller (the catch-all must not swallow it).
	t.Run("unrelated unknown path -> 404", func(t *testing.T) {
		resp := client.GET("/api/this-does-not-exist")
		require.Equal(t, http.StatusNotFound, resp.StatusCode,
			"unauthenticated unrelated path must be 404, got %d: %s", resp.StatusCode, resp.String())

		resp = client.GET("/api/this-does-not-exist", testutil.WithAuth(superadminToken))
		require.Equal(t, http.StatusNotFound, resp.StatusCode,
			"superadmin unrelated path must still be 404, got %d: %s", resp.StatusCode, resp.String())
	})

	// The moved /api/admin/agents cancel route (now /api/projects/:projectId/
	// agents/...) must keep its normal 404, not 401/403 from the platform gate.
	t.Run("moved admin agents cancel route -> 404", func(t *testing.T) {
		fakeAgentID := "00000000-0000-0000-0000-00000000aaaa"
		fakeRunID := "00000000-0000-0000-0000-00000000bbbb"
		path := "/api/admin/agents/" + fakeAgentID + "/runs/" + fakeRunID + "/cancel"

		resp := client.POST(path)
		require.Equal(t, http.StatusNotFound, resp.StatusCode,
			"unauthenticated moved cancel route must be 404, got %d: %s", resp.StatusCode, resp.String())

		resp = client.POST(path, testutil.WithAuth(superadminToken))
		require.Equal(t, http.StatusNotFound, resp.StatusCode,
			"superadmin moved cancel route must still be 404, got %d: %s", resp.StatusCode, resp.String())
	})

	// A public probe must remain public (not intercepted by the gate).
	t.Run("public probe unaffected -> 200", func(t *testing.T) {
		resp := client.GET("/healthz")
		require.Equal(t, http.StatusOK, resp.StatusCode,
			"public /healthz must remain 200, got %d: %s", resp.StatusCode, resp.String())
	})
}
