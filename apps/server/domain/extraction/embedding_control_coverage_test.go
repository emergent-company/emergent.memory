package extraction_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// vec768Coverage returns a 768-dimension pgvector literal.
func vec768Coverage() string {
	parts := make([]string, 768)
	for i := range parts {
		parts[i] = "0.0"
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// seedCoverageObjects inserts live graph objects owned by projectID: the first
// embeddedCount carry an embedding_v2 vector and the next awaitingCount do not.
func seedCoverageObjects(t *testing.T, ctx context.Context, db bun.IDB, projectID string, embeddedCount, awaitingCount int) {
	t.Helper()
	for range embeddedCount {
		id := uuid.NewString()
		_, err := db.NewRaw(`INSERT INTO kb.graph_objects
			(id, project_id, canonical_id, version, type, status, properties, labels, created_at, updated_at, embedding_v2)
			VALUES (?, ?, ?, 1, 'test', 'active', '{}'::jsonb, '{}'::text[], now(), now(), ?::vector)`,
			id, projectID, id, vec768Coverage()).Exec(ctx)
		require.NoError(t, err)
	}
	for range awaitingCount {
		id := uuid.NewString()
		_, err := db.NewRaw(`INSERT INTO kb.graph_objects
			(id, project_id, canonical_id, version, type, status, properties, labels, created_at, updated_at)
			VALUES (?, ?, ?, 1, 'test', 'active', '{}'::jsonb, '{}'::text[], now(), now())`,
			id, projectID, id).Exec(ctx)
		require.NoError(t, err)
	}
}

// TestEmbeddingCoverageAuthz proves GET /api/embeddings/coverage mirrors the
// Progress authorization posture (issue #940): project-scoped counts for a
// project member, deployment-wide only for an active superadmin_full grant, and
// a 403 for a caller with no project context and no superadmin grant.
func TestEmbeddingCoverageAuthz(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "embedding_coverage_authz")
	defer testDB.Close()

	dbc := testDB.GetDB()
	require.NoError(t, testutil.SetupTestFixtures(ctx, dbc))

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	require.NoError(t, testutil.CreateTestOrganization(ctx, dbc, orgID, "Coverage Org"))
	require.NoError(t, testutil.CreateTestProject(ctx, dbc, testutil.TestProject{ID: projectID, OrgID: orgID, Name: "Coverage Project"}, testutil.AdminUser.ID))
	require.NoError(t, testutil.CreateTestOrgMembership(ctx, dbc, orgID, testutil.AdminUser.ID, "org_admin"))
	require.NoError(t, testutil.CreateTestProjectMembership(ctx, dbc, projectID, testutil.AdminUser.ID, "project_admin"))

	// superadmin_full principal: AdminUser holds an active superadmin_full grant.
	_, err := dbc.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, testutil.AdminUser.ID).Exec(ctx)
	require.NoError(t, err)

	// org_admin escalation principal (the exact path the earlier scope gate admitted).
	orgAdminUserID := uuid.New().String()
	require.NoError(t, testutil.CreateTestUser(ctx, dbc, testutil.TestUser{
		ID:            orgAdminUserID,
		ZitadelUserID: "coverage-org-admin-escalation-user",
		Email:         "coverage-orgadmin@test.local",
	}))
	require.NoError(t, testutil.CreateTestOrgMembership(ctx, dbc, orgID, orgAdminUserID, "org_admin"))
	require.NoError(t, testutil.CreateTestAccountAPIToken(ctx, dbc, orgAdminUserID, orgAdminToken, []string{"admin:all"}))

	// Seed coverage: 2 embedded + 1 awaiting object for projectID.
	seedCoverageObjects(t, ctx, dbc, projectID, 2, 1)

	client := newEmbeddingControlEcho(t, testDB)

	t.Run("coverage without project: superadmin_full only", func(t *testing.T) {
		resp := client.GET("/api/embeddings/coverage", testutil.WithAuth(orgAdminToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "project-less coverage by org_admin admin:all token must be 403, got %d: %s", resp.StatusCode, resp.String())

		resp = client.GET("/api/embeddings/coverage", testutil.WithAuth(noScopeToken))
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "project-less coverage by non-admin must be 403, got %d: %s", resp.StatusCode, resp.String())

		resp = client.GET("/api/embeddings/coverage", testutil.WithAuth(superadminToken))
		require.Equal(t, http.StatusOK, resp.StatusCode, "project-less coverage by superadmin_full must be 200, got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("coverage is project-scoped for members", func(t *testing.T) {
		resp := client.GET("/api/embeddings/coverage",
			testutil.WithAuth(superadminToken), testutil.WithProjectID(projectID))
		require.Equal(t, http.StatusOK, resp.StatusCode, "member coverage must be 200, got %d: %s", resp.StatusCode, resp.String())

		var coverage struct {
			Objects struct {
				Embedded int64 `json:"embedded"`
				Awaiting int64 `json:"awaiting"`
				Total    int64 `json:"total"`
			} `json:"objects"`
			Relationships struct {
				Embedded int64 `json:"embedded"`
				Awaiting int64 `json:"awaiting"`
				Total    int64 `json:"total"`
			} `json:"relationships"`
		}
		require.NoError(t, json.Unmarshal(resp.Body, &coverage))
		require.Equal(t, int64(2), coverage.Objects.Embedded)
		require.Equal(t, int64(1), coverage.Objects.Awaiting)
		require.Equal(t, int64(3), coverage.Objects.Total)
		require.Equal(t, int64(0), coverage.Relationships.Embedded)
		require.Equal(t, int64(0), coverage.Relationships.Awaiting)
		require.Equal(t, int64(0), coverage.Relationships.Total)
	})
}
