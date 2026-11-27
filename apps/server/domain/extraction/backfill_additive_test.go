package extraction

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestBackfillEnqueuesAdditiveJobs pins that backfill-triggered re-extraction
// is marked additive, so the worker skips supersede-by-lineage and does not
// drop entities the re-run failed to reproduce (recall wobble).
func TestBackfillEnqueuesAdditiveJobs(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	docID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.documents (id, project_id, filename, created_at, updated_at)
		VALUES (?, ?, 'doc.md', now(), now())`, docID, projectID).Exec(ctx)
	require.NoError(t, err)

	svc := NewObjectExtractionJobsService(db, quietLogger(), DefaultObjectExtractionConfig())
	res, err := svc.Backfill(ctx, projectID, BackfillNever, 10, false)
	require.NoError(t, err)
	require.Equal(t, 1, res.Enqueued)

	job := new(ObjectExtractionJob)
	err = db.NewSelect().Model(job).Where("document_id = ?", docID).Scan(ctx)
	require.NoError(t, err)
	require.Equal(t, true, job.SourceMetadata["additive"])
}

// TestTriggerForDocumentNotAdditive pins that the normal (upload/reprocess)
// path is NOT additive: it must keep superseding the document's prior output.
func TestTriggerForDocumentNotAdditive(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	docID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.documents (id, project_id, filename, created_at, updated_at)
		VALUES (?, ?, 'doc.md', now(), now())`, docID, projectID).Exec(ctx)
	require.NoError(t, err)

	svc := NewObjectExtractionJobsService(db, quietLogger(), DefaultObjectExtractionConfig())
	require.NoError(t, svc.TriggerForDocument(ctx, projectID, docID))

	job := new(ObjectExtractionJob)
	err = db.NewSelect().Model(job).Where("document_id = ?", docID).Scan(ctx)
	require.NoError(t, err)
	_, additive := job.SourceMetadata["additive"]
	require.False(t, additive, "non-backfill extraction must not be additive")
}
