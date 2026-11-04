package extraction

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/jobs"
)

// TestObjectExtractionStatsCountsDeadLetterAsFailed pins the status truth used
// by the Documents page: a dead-lettered job is a failure, not a status the
// statistics silently drop. It remains separately countable as the
// "failed permanently" subset.
func TestObjectExtractionStatsCountsDeadLetterAsFailed(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	seed := func(status, errMsg string) {
		t.Helper()
		_, err := db.NewRaw(`INSERT INTO kb.object_extraction_jobs
			(id, project_id, status, retry_count, max_retries, error_message, created_at, updated_at)
			VALUES (gen_random_uuid(), ?, ?, 0, 3, ?, now(), now())`, projectID, status, errMsg).Exec(ctx)
		require.NoError(t, err)
	}

	seed("completed", "")
	seed("failed", "provider 500")
	seed("failed", jobs.StaleJobMessage)
	seed("dead_letter", "model returned malformed JSON")

	svc := NewObjectExtractionJobsService(db, quietLogger(), DefaultObjectExtractionConfig())

	stats, err := svc.Stats(ctx, &projectID)
	require.NoError(t, err)
	require.Equal(t, int64(4), stats.Total)
	require.Equal(t, int64(1), stats.Completed)
	require.Equal(t, int64(1), stats.DeadLettered)
	// One genuine failure + one dead letter; the stale-sweep row is split out.
	require.Equal(t, int64(2), stats.Failed)
	require.Equal(t, int64(1), stats.StaleFailed)

	extended, err := svc.GetStatistics(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, int64(2), extended.JobsByStatus["failed"])
	require.Equal(t, int64(1), extended.JobsByStatus["dead_letter"])
}
