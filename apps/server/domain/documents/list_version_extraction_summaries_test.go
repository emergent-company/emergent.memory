package documents

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// seedSummariesFixture inserts an org, project and document, returning the
// project and document IDs.
func seedSummariesFixture(t *testing.T, ctx context.Context, db bun.IDB) (projectID, documentID string) {
	t.Helper()
	orgID := uuid.NewString()
	projectID = uuid.NewString()
	documentID = uuid.NewString()

	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'Summaries Org', now(), now())`, orgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, 'Summaries Project', ?, now(), now())`, projectID, orgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.documents (id, project_id, created_at, updated_at)
		VALUES (?, ?, now(), now())`, documentID, projectID).Exec(ctx)
	require.NoError(t, err)

	return projectID, documentID
}

// TestListVersionExtractionSummariesLastExtractionAtUsesCreatedAt pins the
// #2041 review fix: the per-version "last extraction" timestamp must be the most
// recent job *creation* time, not completion time. A failed/cancelled job never
// sets completed_at, so MAX(completed_at) ignores it (and yields NULL when every
// job for a version failed); MAX(created_at) always reflects the latest attempt.
func TestListVersionExtractionSummariesLastExtractionAtUsesCreatedAt(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "doc_version_extraction_summaries")
	defer tdb.Close()
	db := tdb.GetDB()

	projectID, documentID := seedSummariesFixture(t, ctx, db)

	createdA := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	createdB := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	completedA := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

	// Completed job: created_at earlier than its completed_at.
	_, err := db.NewRaw(`INSERT INTO kb.object_extraction_jobs
		(id, project_id, document_id, status, objects_created, created_at, completed_at)
		VALUES (?, ?, ?, 'completed', 2, ?, ?)`,
		uuid.NewString(), projectID, documentID, createdA, completedA).Exec(ctx)
	require.NoError(t, err)

	// Failed job: later created_at, but completed_at is NULL (never finished).
	_, err = db.NewRaw(`INSERT INTO kb.object_extraction_jobs
		(id, project_id, document_id, status, objects_created, created_at, completed_at)
		VALUES (?, ?, ?, 'failed', 3, ?, NULL)`,
		uuid.NewString(), projectID, documentID, createdB).Exec(ctx)
	require.NoError(t, err)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := NewRepository(db, log)

	summaries, err := repo.ListVersionExtractionSummaries(ctx, projectID, []string{documentID})
	require.NoError(t, err)
	require.Contains(t, summaries, documentID)

	got := summaries[documentID]
	require.Equal(t, 2, got.ExtractionJobs)
	require.Equal(t, 5, got.ObjectsCreated)
	require.NotNil(t, got.LastExtractionAt)
	require.Equal(t, createdB, got.LastExtractionAt.UTC(),
		"last_extraction_at must be MAX(created_at) (the failed job's creation), not MAX(completed_at)")
}

// TestListVersionExtractionSummariesAllFailedStillTimestamped proves a version
// whose jobs all failed (completed_at IS NULL) still reports a non-nil
// last_extraction_at — impossible under MAX(completed_at), which would collapse
// to NULL.
func TestListVersionExtractionSummariesAllFailedStillTimestamped(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "doc_version_extraction_all_failed")
	defer tdb.Close()
	db := tdb.GetDB()

	projectID, documentID := seedSummariesFixture(t, ctx, db)

	createdAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	_, err := db.NewRaw(`INSERT INTO kb.object_extraction_jobs
		(id, project_id, document_id, status, objects_created, created_at, completed_at)
		VALUES (?, ?, ?, 'failed', 1, ?, NULL)`,
		uuid.NewString(), projectID, documentID, createdAt).Exec(ctx)
	require.NoError(t, err)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := NewRepository(db, log)

	summaries, err := repo.ListVersionExtractionSummaries(ctx, projectID, []string{documentID})
	require.NoError(t, err)
	require.Contains(t, summaries, documentID)

	got := summaries[documentID]
	require.NotNil(t, got.LastExtractionAt)
	require.Equal(t, createdAt, got.LastExtractionAt.UTC())
}
