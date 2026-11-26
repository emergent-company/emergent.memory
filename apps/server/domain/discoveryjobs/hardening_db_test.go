package discoveryjobs

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestListJobsExposesAutoAppliedReport pins that the auto-apply decision (and
// its decline reason) round-trip through the list DTO, so a caller can surface
// what the pipeline decided rather than a bare "completed".
func TestListJobsExposesAutoAppliedReport(t *testing.T) {
	ctx := context.Background()
	testDB := testdb.SetupTestDBOrFail(t, ctx, "discovery_hardening")
	defer testDB.Close()
	db := testDB.DB

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, 'T', now(), now())`, orgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at) VALUES (?, 'T', ?, now(), now())`, projectID, orgID).Exec(ctx)
	require.NoError(t, err)
	projectUUID := uuid.MustParse(projectID)

	repo := NewRepository(db, quietLogger())

	job := &DiscoveryJob{ID: uuid.New(), OrganizationID: uuid.MustParse(orgID), ProjectID: projectUUID, Status: StatusCompleted}
	require.NoError(t, repo.Create(ctx, job))

	report := JSONMap{"applied": false, "declined": "too many new types in one run"}
	applied, err := repo.MarkAutoApplied(ctx, job.ID, report)
	require.NoError(t, err)
	require.True(t, applied)

	svc := &Service{repo: repo, log: quietLogger()}
	items, err := svc.ListJobsForProject(ctx, projectUUID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.True(t, items[0].AutoApplied, "auto_applied must be exposed in the list DTO")
	require.Equal(t, "too many new types in one run", items[0].AppliedReport["declined"])
}
