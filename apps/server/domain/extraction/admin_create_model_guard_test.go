package extraction_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/modelconfig"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// stubGenerativeResolver is a fixed-outcome GenerativeModelResolver used to
// exercise the job-creation model guard without a database-backed model config.
type stubGenerativeResolver struct {
	model string
	err   error
}

func (s stubGenerativeResolver) ResolveGenerativeModel(context.Context, uuid.UUID) (string, modelconfig.ModelSource, error) {
	return s.model, modelconfig.ModelSourceProject, s.err
}

// setupGuardTestProject provisions an org + project owned/administered by
// testutil.AdminUser (token "e2e-test-user" carries admin scopes and
// superadmin_full), returning the project ID.
func setupGuardTestProject(t *testing.T, ctx context.Context, testDB *testutil.TestDB) string {
	t.Helper()
	dbc := testDB.GetDB()
	require.NoError(t, testutil.SetupTestFixtures(ctx, dbc))

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	require.NoError(t, testutil.CreateTestOrganization(ctx, dbc, orgID, "Guard Org"))
	require.NoError(t, testutil.CreateTestProject(ctx, dbc, testutil.TestProject{ID: projectID, OrgID: orgID, Name: "Guard Project"}, testutil.AdminUser.ID))
	require.NoError(t, testutil.CreateTestOrgMembership(ctx, dbc, orgID, testutil.AdminUser.ID, "org_admin"))
	require.NoError(t, testutil.CreateTestProjectMembership(ctx, dbc, projectID, testutil.AdminUser.ID, "project_admin"))

	_, err := dbc.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, testutil.AdminUser.ID).Exec(ctx)
	require.NoError(t, err)

	return projectID
}

func countExtractionJobs(t *testing.T, ctx context.Context, testDB *testutil.TestDB, projectID string) int {
	t.Helper()
	var count int
	err := testDB.GetDB().NewRaw(
		`SELECT count(*) FROM kb.object_extraction_jobs WHERE project_id = ?`, projectID,
	).Scan(ctx, &count)
	require.NoError(t, err)
	return count
}

// TestCreateExtractionJobRejectedWithoutGenerativeModel is the core guard:
// when no effective generative model resolves, POST must fail 409 BEFORE a job
// row is inserted, instead of enqueueing a job that dies at run time (#1555).
func TestCreateExtractionJobRejectedWithoutGenerativeModel(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "create_guard_no_model")
	defer testDB.Close()

	projectID := setupGuardTestProject(t, ctx, testDB)
	client := newAdminEchoWithResolver(t, testDB, stubGenerativeResolver{})

	resp := client.POST("/api/admin/extraction-jobs",
		testutil.WithAuth(superadminToken),
		testutil.WithJSONBody(map[string]any{"project_id": projectID, "source_type": "document"}))

	require.Equal(t, http.StatusConflict, resp.StatusCode,
		"create with no generative model must be 409, got %d: %s", resp.StatusCode, resp.String())
	require.Contains(t, resp.String(), "No generative model is configured",
		"409 body must explain the missing model, got: %s", resp.String())
	require.Equal(t, 0, countExtractionJobs(t, ctx, testDB, projectID),
		"no job row may be inserted when the guard rejects")
}

// TestCreateExtractionJobSucceedsWithGenerativeModel proves the guard does not
// over-block: a project whose generative model resolves still creates a job.
func TestCreateExtractionJobSucceedsWithGenerativeModel(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "create_guard_with_model")
	defer testDB.Close()

	projectID := setupGuardTestProject(t, ctx, testDB)
	client := newAdminEchoWithResolver(t, testDB, stubGenerativeResolver{model: "deepseek/deepseek-v4-flash"})

	resp := client.POST("/api/admin/extraction-jobs",
		testutil.WithAuth(superadminToken),
		testutil.WithJSONBody(map[string]any{"project_id": projectID, "source_type": "document"}))

	require.Equal(t, http.StatusCreated, resp.StatusCode,
		"create with a resolvable generative model must be 201, got %d: %s", resp.StatusCode, resp.String())
	require.Equal(t, 1, countExtractionJobs(t, ctx, testDB, projectID),
		"job row must be inserted when a generative model resolves")
}

// TestCreateExtractionJobFailsOpenOnResolverError preserves the pre-existing
// behavior on a transient resolution error: extraction is not blocked (mirrors
// the gateway's fail-open extractionModelMissing convention).
func TestCreateExtractionJobFailsOpenOnResolverError(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "create_guard_resolver_err")
	defer testDB.Close()

	projectID := setupGuardTestProject(t, ctx, testDB)
	client := newAdminEchoWithResolver(t, testDB, stubGenerativeResolver{err: errors.New("model config store unavailable")})

	resp := client.POST("/api/admin/extraction-jobs",
		testutil.WithAuth(superadminToken),
		testutil.WithJSONBody(map[string]any{"project_id": projectID, "source_type": "document"}))

	require.Equal(t, http.StatusCreated, resp.StatusCode,
		"resolution error must fail open, got %d: %s", resp.StatusCode, resp.String())
	require.Equal(t, 1, countExtractionJobs(t, ctx, testDB, projectID),
		"job row must still be inserted when resolution errors")
}
