package extraction_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
)

const (
	noScopeToken     = "no-scope"                  // NoScopeUser: empty scopes, no membership
	superadminToken  = "e2e-test-user"             // AdminUser: all scopes + core.superadmins superadmin_full
	orgAdminToken    = "emt_test_940_org_admin"    // admin:all token minted by an org_admin (the escalation path)
	orgAdminUserName = "org-admin-escalation-user" // dedicated org_admin profile
)

// newEmbeddingControlEcho wires a minimal Echo instance with only the
// /api/embeddings control routes and the real auth middleware + control handler,
// so the authorization posture of each endpoint can be exercised over HTTP
// without the full in-process server (which does not register these routes).
func newEmbeddingControlEcho(t *testing.T, testDB *testutil.TestDB) *testutil.HTTPClient {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	dbc := testDB.GetDB()

	userSvc := auth.NewUserProfileService(dbc, log)
	am := auth.NewMiddleware(auth.MiddlewareParams{DB: dbc, Cfg: testDB.Config, Log: log, UserSvc: userSvc})

	embeds := embeddings.NewNoopService(log)
	cfg := extraction.DefaultGraphEmbeddingConfig()
	cfg.EnableAdaptiveScaling = false

	objectJobsSvc := extraction.NewGraphEmbeddingJobsService(dbc, log, cfg)
	relJobsSvc := extraction.NewGraphRelationshipEmbeddingJobsService(dbc, log, cfg)
	objectWorker := extraction.NewGraphEmbeddingWorker(objectJobsSvc, embeds, dbc, cfg, log, nil, nil, nil, false)
	relWorker := extraction.NewGraphRelationshipEmbeddingWorker(relJobsSvc, embeds, dbc, cfg, nil, log, nil, nil, false)
	sweepWorker := extraction.NewEmbeddingSweepWorker(objectJobsSvc, embeds, dbc, extraction.DefaultEmbeddingSweepConfig(), log, nil, nil, false)
	handler := extraction.NewEmbeddingControlHandler(objectWorker, relWorker, sweepWorker, nil, objectJobsSvc, relJobsSvc)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	extraction.RegisterEmbeddingControlRoutes(e, handler, am)

	return testutil.NewHTTPClient(e)
}

// seedObjectJobs inserts count pending embedding jobs owned by projectID so the
// project-scoped progress assertions can distinguish own-project from global
// counts.
func seedObjectJobs(t *testing.T, ctx context.Context, db bun.IDB, projectID string, count int) {
	t.Helper()
	for range count {
		objID := uuid.New().String()
		jobID := uuid.New().String()
		_, err := db.NewRaw(
			`INSERT INTO kb.graph_objects (id, project_id, type, canonical_id) VALUES (?, ?, 'test', ?)`,
			objID, projectID, objID).Exec(ctx)
		require.NoError(t, err)
		_, err = db.NewRaw(
			`INSERT INTO kb.graph_embedding_jobs (id, object_id, status) VALUES (?, ?, 'pending')`,
			jobID, objID).Exec(ctx)
		require.NoError(t, err)
	}
}

// TestEmbeddingControlAuthz proves the /api/embeddings control group is gated by
// superadmin_full for operator writes + diagnostics and by project membership
// (with project-scoped counts) for reads (issue #940). It specifically proves
// that an org_admin-minted admin:all token does NOT satisfy the deployment-wide
// controls (the proportionality failure of the earlier scope-based gate).
func TestEmbeddingControlAuthz(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "embedding_control_authz")
	defer testDB.Close()

	dbc := testDB.GetDB()
	require.NoError(t, testutil.SetupTestFixtures(ctx, dbc))

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	require.NoError(t, testutil.CreateTestOrganization(ctx, dbc, orgID, "Authz Org"))
	require.NoError(t, testutil.CreateTestProject(ctx, dbc, testutil.TestProject{ID: projectID, OrgID: orgID, Name: "Authz Project"}, testutil.AdminUser.ID))
	require.NoError(t, testutil.CreateTestOrgMembership(ctx, dbc, orgID, testutil.AdminUser.ID, "org_admin"))
	require.NoError(t, testutil.CreateTestProjectMembership(ctx, dbc, projectID, testutil.AdminUser.ID, "project_admin"))

	// superadmin_full principal: AdminUser holds an active superadmin_full grant.
	_, err := dbc.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, testutil.AdminUser.ID).Exec(ctx)
	require.NoError(t, err)

	// org_admin escalation principal: a dedicated user who is an org_admin and
	// mints an admin:all token — the exact path the earlier scope gate admitted.
	orgAdminUserID := uuid.New().String()
	require.NoError(t, testutil.CreateTestUser(ctx, dbc, testutil.TestUser{
		ID:            orgAdminUserID,
		ZitadelUserID: orgAdminUserName,
		Email:         "orgadmin@test.local",
	}))
	require.NoError(t, testutil.CreateTestOrgMembership(ctx, dbc, orgID, orgAdminUserID, "org_admin"))
	require.NoError(t, testutil.CreateTestAccountAPIToken(ctx, dbc, orgAdminUserID, orgAdminToken, []string{"admin:all"}))

	foreignOrg := uuid.New().String()
	foreignProject := uuid.New().String()
	require.NoError(t, testutil.CreateTestOrganization(ctx, dbc, foreignOrg, "Foreign Org"))
	require.NoError(t, testutil.CreateTestProject(ctx, dbc, testutil.TestProject{ID: foreignProject, OrgID: foreignOrg, Name: "Foreign Project"}, testutil.AdminUser.ID))

	seedObjectJobs(t, ctx, dbc, projectID, 2)
	seedObjectJobs(t, ctx, dbc, foreignProject, 5)

	client := newEmbeddingControlEcho(t, testDB)

	writeCases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/embeddings/pause"},
		{http.MethodPost, "/api/embeddings/resume"},
		{http.MethodPatch, "/api/embeddings/config"},
		{http.MethodDelete, "/api/embeddings/queue"},
		{http.MethodPost, "/api/embeddings/reset-schedule"},
	}

	t.Run("operator writes refused for org_admin admin:all token", func(t *testing.T) {
		for _, c := range writeCases {
			resp := client.Request(c.method, c.path, testutil.WithAuth(orgAdminToken))
			require.Equal(t, http.StatusForbidden, resp.StatusCode,
				"%s %s by org_admin admin:all token must be 403, got %d: %s", c.method, c.path, resp.StatusCode, resp.String())
		}
	})

	t.Run("operator writes refused for non-admin", func(t *testing.T) {
		for _, c := range writeCases {
			resp := client.Request(c.method, c.path, testutil.WithAuth(noScopeToken))
			require.Equal(t, http.StatusForbidden, resp.StatusCode,
				"%s %s by non-admin must be 403, got %d: %s", c.method, c.path, resp.StatusCode, resp.String())
		}
	})

	t.Run("operator writes allowed for superadmin_full", func(t *testing.T) {
		for _, c := range writeCases {
			resp := client.Request(c.method, c.path, testutil.WithAuth(superadminToken))
			require.Equal(t, http.StatusOK, resp.StatusCode,
				"%s %s by superadmin_full must be 200, got %d: %s", c.method, c.path, resp.StatusCode, resp.String())
		}
	})

	t.Run("diagnose refused for org_admin and non-admin, allowed for superadmin_full", func(t *testing.T) {
		resp := client.GET("/api/embeddings/diagnose", testutil.WithAuth(orgAdminToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "diagnose by org_admin admin:all token must be 403, got %d: %s", resp.StatusCode, resp.String())

		resp = client.GET("/api/embeddings/diagnose", testutil.WithAuth(noScopeToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "diagnose by non-admin must be 403, got %d: %s", resp.StatusCode, resp.String())

		resp = client.GET("/api/embeddings/diagnose", testutil.WithAuth(superadminToken))
		require.Equal(t, http.StatusOK, resp.StatusCode, "diagnose by superadmin_full must be 200, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("progress without project: superadmin_full only", func(t *testing.T) {
		resp := client.GET("/api/embeddings/progress", testutil.WithAuth(orgAdminToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "project-less progress by org_admin admin:all token must be 403, got %d: %s", resp.StatusCode, resp.String())

		resp = client.GET("/api/embeddings/progress", testutil.WithAuth(noScopeToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "project-less progress by non-admin must be 403, got %d: %s", resp.StatusCode, resp.String())

		resp = client.GET("/api/embeddings/progress", testutil.WithAuth(superadminToken))
		require.Equal(t, http.StatusOK, resp.StatusCode, "project-less progress by superadmin_full must be 200, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("progress is project-scoped for members", func(t *testing.T) {
		// Re-seed: the admin write subtest above clears the queue (DELETE /queue).
		seedObjectJobs(t, ctx, dbc, projectID, 2)
		seedObjectJobs(t, ctx, dbc, foreignProject, 5)

		resp := client.GET("/api/embeddings/progress",
			testutil.WithAuth(superadminToken), testutil.WithProjectID(projectID))
		require.Equal(t, http.StatusOK, resp.StatusCode, "member progress must be 200, got %d: %s", resp.StatusCode, resp.String())

		var progress struct {
			Objects struct {
				Pending int64 `json:"pending"`
			} `json:"objects"`
		}
		require.NoError(t, json.Unmarshal(resp.Body, &progress))
		require.Equal(t, int64(2), progress.Objects.Pending,
			"member progress must be scoped to their own project (2), got %d", progress.Objects.Pending)
	})

	t.Run("progress for foreign project denied", func(t *testing.T) {
		resp := client.GET("/api/embeddings/progress",
			testutil.WithAuth(noScopeToken), testutil.WithProjectID(foreignProject))
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "non-member foreign-project progress must be 403, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("status is a project-member read", func(t *testing.T) {
		resp := client.GET("/api/embeddings/status",
			testutil.WithAuth(superadminToken), testutil.WithProjectID(projectID))
		require.Equal(t, http.StatusOK, resp.StatusCode, "member status must be 200, got %d: %s", resp.StatusCode, resp.String())

		resp = client.GET("/api/embeddings/status",
			testutil.WithAuth(noScopeToken), testutil.WithProjectID(foreignProject))
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "non-member status must be 403, got %d: %s", resp.StatusCode, resp.String())
	})
}
