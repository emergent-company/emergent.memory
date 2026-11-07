package extraction

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// TestRunRecorderRecordVariance_PersistsSummaryAndTranscript is the persistence
// half of the variance signal: the warning lands on the run's summary jsonb
// (the field the run API/chat workspace reads) and on the transcript as a
// warning-labelled message, naming both counts.
func TestRunRecorderRecordVariance_PersistsSummaryAndTranscript(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	var jobID string
	require.NoError(t, db.NewRaw(`INSERT INTO kb.object_extraction_jobs
		(id, project_id, status, created_at, updated_at)
		VALUES (gen_random_uuid(), ?, 'pending', now(), now())
		RETURNING id::text`, projectID).Scan(ctx, &jobID))

	rec := NewRunRecorder(db, quietLogger())
	job := &ObjectExtractionJob{ID: jobID, ProjectID: projectID}
	runID, err := rec.StartRun(ctx, job)
	require.NoError(t, err)

	warning := planRunVariance(&RunCountSnapshot{JobID: "previous-job", ObjectsCreated: 213}, 71)
	require.NotNil(t, warning)
	require.NoError(t, rec.RecordVariance(ctx, runID, warning))

	var previousObjects, objects, direction, previousJobID string
	require.NoError(t, db.NewRaw(`SELECT
		summary->'variance_warning'->>'previous_objects',
		summary->'variance_warning'->>'objects',
		summary->'variance_warning'->>'direction',
		summary->'variance_warning'->>'previous_job_id'
		FROM kb.agent_runs WHERE id = ?`, runID).Scan(ctx, &previousObjects, &objects, &direction, &previousJobID))
	require.Equal(t, "213", previousObjects)
	require.Equal(t, "71", objects)
	require.Equal(t, "smaller", direction)
	require.Equal(t, "previous-job", previousJobID)

	// The original summary keys survive the merge.
	var trigger string
	require.NoError(t, db.NewRaw(`SELECT summary->>'trigger' FROM kb.agent_runs WHERE id = ?`, runID).Scan(ctx, &trigger))
	require.Equal(t, "extraction", trigger)

	var stageKind, text string
	require.NoError(t, db.NewRaw(`SELECT content->>'stage_kind', content->>'text'
		FROM kb.agent_run_messages
		WHERE run_id = ? AND content->>'stage_kind' = ?`, runID, stageKindWarning).Scan(ctx, &stageKind, &text))
	require.Equal(t, "warning", stageKind)
	require.Contains(t, text, "71 objects")
	require.Contains(t, text, "213 objects")
}

// seedVarianceDocument inserts a minimal document row for the extraction-job
// FK and returns its id.
func seedVarianceDocument(t *testing.T, db bun.IDB, projectID string) string {
	t.Helper()
	documentID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.documents (id, project_id, filename, created_at, updated_at)
		VALUES (?, ?, 'variance.docx', now(), now())`, documentID, projectID).Exec(context.Background())
	require.NoError(t, err)
	return documentID
}

// TestPreviousCompletedRunForDocument_ComparableRun checks the lookup the
// variance decision is made on: only completed runs of the same document and
// schema configuration count, and the newest one wins.
func TestPreviousCompletedRunForDocument_ComparableRun(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	documentID := seedVarianceDocument(t, db, projectID)
	otherDocumentID := seedVarianceDocument(t, db, projectID)

	insert := func(docID, status string, objects int, enabledTypes []string, completedAt string) string {
		if enabledTypes == nil {
			// enabled_types is NOT NULL; pq.Array(nil) encodes SQL NULL.
			enabledTypes = []string{}
		}
		var id string
		require.NoError(t, db.NewRaw(`INSERT INTO kb.object_extraction_jobs
			(id, project_id, document_id, status, objects_created, enabled_types, created_at, updated_at, completed_at)
			VALUES (gen_random_uuid(), ?, ?, ?, ?, ?, now(), now(), ?::timestamptz)
			RETURNING id::text`,
			projectID, docID, status, objects, pq.Array(enabledTypes), completedAt).Scan(ctx, &id))
		return id
	}

	// Not comparable: different document.
	insert(otherDocumentID, string(JobStatusCompleted), 999, nil, "2026-11-01T00:00:00Z")
	// Not comparable: did not complete.
	insert(documentID, string(JobStatusFailed), 500, nil, "2026-11-02T00:00:00Z")
	// Comparable, older.
	insert(documentID, string(JobStatusCompleted), 140, nil, "2026-11-03T00:00:00Z")
	// Comparable, newest — this is the one the next run must be compared with.
	wantID := insert(documentID, string(JobStatusCompleted), 213, nil, "2026-11-04T00:00:00Z")

	current := &ObjectExtractionJob{
		ID:         uuid.NewString(),
		ProjectID:  projectID,
		DocumentID: &documentID,
		Status:     JobStatusProcessing,
	}

	svc := NewObjectExtractionJobsService(db, quietLogger(), nil)
	previous, err := svc.PreviousCompletedRunForDocument(ctx, current)
	require.NoError(t, err)
	require.NotNil(t, previous)
	require.Equal(t, wantID, previous.JobID)
	require.Equal(t, 213, previous.ObjectsCreated)
}

// TestPreviousCompletedRunForDocument_NoComparableRun: the first run, a job
// without a document, and a run whose schema config changed all yield no
// previous snapshot — which is what keeps the first run from warning.
func TestPreviousCompletedRunForDocument_NoComparableRun(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	documentID := seedVarianceDocument(t, db, projectID)

	svc := NewObjectExtractionJobsService(db, quietLogger(), nil)

	// No document on the job.
	previous, err := svc.PreviousCompletedRunForDocument(ctx, &ObjectExtractionJob{ID: uuid.NewString(), ProjectID: projectID})
	require.NoError(t, err)
	require.Nil(t, previous)

	// A completed run exists but was created with pinned types.
	var pinnedID string
	require.NoError(t, db.NewRaw(`INSERT INTO kb.object_extraction_jobs
		(id, project_id, document_id, status, objects_created, enabled_types, created_at, updated_at, completed_at)
		VALUES (gen_random_uuid(), ?, ?, 'completed', 90, ?, now(), now(), now())
		RETURNING id::text`, projectID, documentID, pq.Array([]string{"Person"})).Scan(ctx, &pinnedID))

	// The current run has no pinned types, so its schema fingerprint differs.
	previous, err = svc.PreviousCompletedRunForDocument(ctx, &ObjectExtractionJob{
		ID: uuid.NewString(), ProjectID: projectID, DocumentID: &documentID,
	})
	require.NoError(t, err)
	require.Nil(t, previous, "a run with a different explicit schema config is not comparable")
}
