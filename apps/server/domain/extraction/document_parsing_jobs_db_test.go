package extraction

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMarkFailedPermanentDeadLettersImmediately pins the difference between the
// retrying and the permanent failure paths: an empty document must not burn the
// retry budget, because no retry can add text to it (#243).
func TestMarkFailedPermanentDeadLettersImmediately(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	svc := NewDocumentParsingJobsService(db, quietLogger(), nil)

	insertJob := func() string {
		var id string
		require.NoError(t, db.NewRaw(`INSERT INTO kb.document_parsing_jobs
			(id, project_id, status, source_type, retry_count, max_retries, created_at, updated_at)
			VALUES (gen_random_uuid(), ?, 'processing', 'file_upload', 0, 3, now(), now())
			RETURNING id::text`, projectID).Scan(ctx, &id))
		return id
	}

	permanentID := insertJob()
	deadLettered, err := svc.MarkFailedPermanent(ctx, permanentID, errors.New("document has no extractable text content"))
	require.NoError(t, err)
	require.True(t, deadLettered, "a permanent failure is terminal on the first attempt")

	var status string
	var retryCount int
	var completed bool
	require.NoError(t, db.NewRaw(`SELECT status, retry_count, completed_at IS NOT NULL
		FROM kb.document_parsing_jobs WHERE id = ?`, permanentID).Scan(ctx, &status, &retryCount, &completed))
	require.Equal(t, string(JobStatusDeadLetter), status)
	require.Zero(t, retryCount, "a permanent failure must not consume the retry budget")
	require.True(t, completed, "a dead-lettered job records when it stopped")

	// The retrying path keeps its existing behaviour: a first failure schedules
	// a retry rather than ending the job.
	retryingID := insertJob()
	deadLettered, err = svc.MarkFailed(ctx, retryingID, errors.New("xberg unavailable"))
	require.NoError(t, err)
	require.False(t, deadLettered)
	require.NoError(t, db.NewRaw(`SELECT status, retry_count FROM kb.document_parsing_jobs WHERE id = ?`,
		retryingID).Scan(ctx, &status, &retryCount))
	require.Equal(t, "retry_pending", status)
	require.Equal(t, 1, retryCount)
}
