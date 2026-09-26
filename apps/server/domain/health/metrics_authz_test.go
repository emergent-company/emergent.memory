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
	metricsOrgA         = "00000000-0000-0000-0000-000000000600"
	metricsOrgB         = "00000000-0000-0000-0000-000000000700"
	metricsProjectA     = "00000000-0000-0000-0000-000000000610"
	metricsProjectB     = "00000000-0000-0000-0000-000000000710"
	metricsMemberAID    = "00000000-0000-0000-0000-000000000801"
	metricsMemberAToken = "emt_metrics_member_a"
	metricsMemberAName  = "metrics-member-a"
)

// newMetricsEcho wires a minimal Echo instance with only the health routes and
// the real auth middleware + handler, so the /api/metrics authorization posture
// can be exercised over HTTP without the full in-process server.
func newMetricsEcho(t *testing.T, testDB *testutil.TestDB) *testutil.HTTPClient {
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

// TestMetricsProjectAuthz proves GET /api/metrics/jobs resolves the effective
// project server-side and never trusts a client-supplied ?project_id: a member
// of project A cannot read project B's metrics by passing ?project_id=<B>, the
// instance-wide aggregate requires superadmin_full, and /api/metrics/scheduler
// (an instance-wide platform concern) is superadmin_full-gated.
func TestMetricsProjectAuthz(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "metrics_authz")
	defer testDB.Close()

	dbc := testDB.GetDB()
	require.NoError(t, testutil.SetupTestFixtures(ctx, dbc))

	// Two organizations, each owning one project.
	require.NoError(t, testutil.CreateTestOrganization(ctx, dbc, metricsOrgA, "Metrics Org A"))
	require.NoError(t, testutil.CreateTestOrganization(ctx, dbc, metricsOrgB, "Metrics Org B"))
	require.NoError(t, testutil.CreateTestProject(ctx, dbc, testutil.TestProject{ID: metricsProjectA, Name: "Project A", OrgID: metricsOrgA}, ""))
	require.NoError(t, testutil.CreateTestProject(ctx, dbc, testutil.TestProject{ID: metricsProjectB, Name: "Project B", OrgID: metricsOrgB}, ""))

	// memberA belongs to org A (owner of project A) and NOT to org B.
	require.NoError(t, testutil.CreateTestUser(ctx, dbc, testutil.TestUser{
		ID:            metricsMemberAID,
		ZitadelUserID: metricsMemberAName,
		Email:         "metrics-member-a@test.local",
	}))
	require.NoError(t, testutil.CreateTestOrgMembership(ctx, dbc, metricsOrgA, metricsMemberAID, "member"))
	require.NoError(t, testutil.CreateTestAccountAPIToken(ctx, dbc, metricsMemberAID, metricsMemberAToken, []string{}))

	// superadmin_full principal: AdminUser holds an active superadmin_full grant.
	_, err := dbc.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, testutil.AdminUser.ID).Exec(ctx)
	require.NoError(t, err)

	// Seed one parsing job in project B so a cross-tenant read is observable.
	_, err = dbc.NewRaw(`INSERT INTO kb.document_parsing_jobs (project_id, status, source_type) VALUES (?, 'pending', 'upload')`, metricsProjectB).Exec(ctx)
	require.NoError(t, err)

	client := newMetricsEcho(t, testDB)

	t.Run("unauthenticated -> 401", func(t *testing.T) {
		resp := client.GET("/api/metrics/jobs")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
			"unauthenticated /api/metrics/jobs must be 401, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("member A reading foreign project B via ?project_id -> 403", func(t *testing.T) {
		resp := client.GET("/api/metrics/jobs?project_id="+metricsProjectB, testutil.WithAuth(metricsMemberAToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"member of A must NOT read B's metrics via ?project_id (got %d: %s)", resp.StatusCode, resp.String())
	})

	t.Run("member A reading foreign project B via X-Project-ID -> 403", func(t *testing.T) {
		resp := client.GET("/api/metrics/jobs", testutil.WithAuth(metricsMemberAToken), testutil.WithProjectID(metricsProjectB))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"member of A must NOT read B's metrics via X-Project-ID (got %d: %s)", resp.StatusCode, resp.String())
	})

	t.Run("member A reading own project A -> 200 project-scoped", func(t *testing.T) {
		resp := client.GET("/api/metrics/jobs", testutil.WithAuth(metricsMemberAToken), testutil.WithProjectID(metricsProjectA))
		require.Equal(t, http.StatusOK, resp.StatusCode,
			"member of A must read A's metrics (got %d: %s)", resp.StatusCode, resp.String())
		require.Contains(t, resp.String(), `"scope":"project"`, "own-project response must be project-scoped")
		require.Contains(t, resp.String(), metricsProjectA, "own-project response must carry the caller's project id")
	})

	t.Run("member A filter mismatch (X-Project-ID A + ?project_id B) -> 403", func(t *testing.T) {
		resp := client.GET("/api/metrics/jobs?project_id="+metricsProjectB, testutil.WithAuth(metricsMemberAToken), testutil.WithProjectID(metricsProjectA))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"a project_id filter for a foreign project must be refused (got %d: %s)", resp.StatusCode, resp.String())
	})

	t.Run("member A aggregate (no project context) -> 403", func(t *testing.T) {
		resp := client.GET("/api/metrics/jobs", testutil.WithAuth(metricsMemberAToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"instance-wide aggregate must NOT be reachable by a non-superadmin (got %d: %s)", resp.StatusCode, resp.String())
	})

	t.Run("superadmin_full aggregate -> 200 account-scoped", func(t *testing.T) {
		resp := client.GET("/api/metrics/jobs", testutil.WithAuth(superadminToken))
		require.Equal(t, http.StatusOK, resp.StatusCode,
			"superadmin_full aggregate must be 200 (got %d: %s)", resp.StatusCode, resp.String())
		require.Contains(t, resp.String(), `"scope":"account"`, "aggregate response must be account-scoped")
	})

	t.Run("scheduler -> superadmin_full only", func(t *testing.T) {
		resp := client.GET("/api/metrics/scheduler")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
			"unauthenticated scheduler must be 401, got %d: %s", resp.StatusCode, resp.String())

		resp = client.GET("/api/metrics/scheduler", testutil.WithAuth(metricsMemberAToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"project member must NOT reach scheduler metrics (got %d: %s)", resp.StatusCode, resp.String())

		resp = client.GET("/api/metrics/scheduler", testutil.WithAuth(superadminToken))
		require.Equal(t, http.StatusOK, resp.StatusCode,
			"superadmin_full scheduler must be 200, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("scheduler does not accept project context", func(t *testing.T) {
		// A project member must not smuggle a project context onto the
		// instance-wide scheduler surface.
		resp := client.GET("/api/metrics/scheduler?project_id="+metricsProjectA, testutil.WithAuth(metricsMemberAToken), testutil.WithProjectID(metricsProjectA))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"scheduler with project context must still be 403 for a member (got %d: %s)", resp.StatusCode, resp.String())
	})
}
