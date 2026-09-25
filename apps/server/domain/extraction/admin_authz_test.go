package extraction_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

const (
	// memberAdminToken is a bare `admin` account token minted by a project
	// member — the escalation path a scope-only gate admits (#948/#949).
	// superadminToken is shared with embedding_control_authz_test.go.
	memberAdminToken = "emt_test_959_member_admin"
)

// newAdminEcho wires a minimal Echo instance with only the /api/admin/extraction-jobs
// routes and the real auth middleware + admin handler, so the authorization
// posture of each endpoint can be exercised over HTTP.
func newAdminEcho(t *testing.T, testDB *testutil.TestDB) *testutil.HTTPClient {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	dbc := testDB.GetDB()

	userSvc := auth.NewUserProfileService(dbc, log)
	am := auth.NewMiddleware(auth.MiddlewareParams{DB: dbc, Cfg: testDB.Config, Log: log, UserSvc: userSvc})

	jobsSvc := extraction.NewObjectExtractionJobsService(dbc, log, extraction.DefaultObjectExtractionConfig())
	handler := extraction.NewAdminHandler(jobsSvc)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	extraction.RegisterAdminRoutes(e, handler, am)

	return testutil.NewHTTPClient(e)
}

// TestAdminExtractionJobsAuthz proves the /api/admin/extraction-jobs group is
// project-scoped (issue #959): a bare `admin` account token held by a project
// member must NOT reach another project's jobs, while a member of the addressed
// project's organization still succeeds.
func TestAdminExtractionJobsAuthz(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "admin_extraction_authz")
	defer testDB.Close()

	dbc := testDB.GetDB()
	require.NoError(t, testutil.SetupTestFixtures(ctx, dbc))

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	require.NoError(t, testutil.CreateTestOrganization(ctx, dbc, orgID, "Authz Org"))
	require.NoError(t, testutil.CreateTestProject(ctx, dbc, testutil.TestProject{ID: projectID, OrgID: orgID, Name: "Authz Project"}, testutil.AdminUser.ID))
	require.NoError(t, testutil.CreateTestOrgMembership(ctx, dbc, orgID, testutil.AdminUser.ID, "org_admin"))
	require.NoError(t, testutil.CreateTestProjectMembership(ctx, dbc, projectID, testutil.AdminUser.ID, "project_admin"))

	// superadmin_full principal (AdminUser).
	_, err := dbc.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, testutil.AdminUser.ID).Exec(ctx)
	require.NoError(t, err)

	// A project member of orgID who mints a bare `admin` account token.
	memberID := uuid.New().String()
	require.NoError(t, testutil.CreateTestUser(ctx, dbc, testutil.TestUser{
		ID:            memberID,
		ZitadelUserID: "member-admin-user",
		Email:         "memberadmin@test.local",
	}))
	require.NoError(t, testutil.CreateTestOrgMembership(ctx, dbc, orgID, memberID, "member"))
	require.NoError(t, testutil.CreateTestAccountAPIToken(ctx, dbc, memberID, memberAdminToken, []string{"admin"}))

	// A foreign project the member-admin token must NOT reach.
	foreignOrg := uuid.New().String()
	foreignProject := uuid.New().String()
	require.NoError(t, testutil.CreateTestOrganization(ctx, dbc, foreignOrg, "Foreign Org"))
	require.NoError(t, testutil.CreateTestProject(ctx, dbc, testutil.TestProject{ID: foreignProject, OrgID: foreignOrg, Name: "Foreign Project"}, testutil.AdminUser.ID))

	// Seed a foreign-project job so the job-scoped route has a target.
	jobsSvc := extraction.NewObjectExtractionJobsService(dbc, slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn})), extraction.DefaultObjectExtractionConfig())
	src := "document"
	foreignJob, err := jobsSvc.CreateJob(ctx, extraction.CreateObjectExtractionJobOptions{ProjectID: foreignProject, SourceType: &src})
	require.NoError(t, err)

	client := newAdminEcho(t, testDB)

	t.Run("project-scoped list: member admin token refused on foreign project", func(t *testing.T) {
		resp := client.GET("/api/admin/extraction-jobs/projects/"+foreignProject, testutil.WithAuth(memberAdminToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"foreign project list by bare admin token must be 403, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("project-scoped list: member admin token allowed on own project", func(t *testing.T) {
		resp := client.GET("/api/admin/extraction-jobs/projects/"+projectID, testutil.WithAuth(memberAdminToken))
		require.Equal(t, http.StatusOK, resp.StatusCode,
			"own project list by member admin token must be 200, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("project-scoped write: member admin token refused on foreign project", func(t *testing.T) {
		resp := client.POST("/api/admin/extraction-jobs/projects/"+foreignProject+"/bulk-cancel", testutil.WithAuth(memberAdminToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"foreign bulk-cancel by bare admin token must be 403, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("job-scoped read: member admin token refused on foreign job", func(t *testing.T) {
		resp := client.GET("/api/admin/extraction-jobs/"+foreignJob.ID, testutil.WithAuth(memberAdminToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"foreign job read by bare admin token must be 403, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("create: member admin token refused on foreign project body", func(t *testing.T) {
		resp := client.POST("/api/admin/extraction-jobs",
			testutil.WithAuth(memberAdminToken),
			testutil.WithJSONBody(map[string]any{"project_id": foreignProject, "source_type": "document"}))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"foreign create by bare admin token must be 403, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("member (superadmin fixture) succeeds on own project", func(t *testing.T) {
		resp := client.GET("/api/admin/extraction-jobs/projects/"+projectID,
			testutil.WithAuth(superadminToken), testutil.WithProjectID(projectID))
		require.Equal(t, http.StatusOK, resp.StatusCode,
			"member list own project must be 200, got %d: %s", resp.StatusCode, resp.String())
	})
}
