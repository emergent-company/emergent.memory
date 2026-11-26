package extraction

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestBackfillStaleSchemaSelection pins that stale_schema selects documents
// whose latest completed extraction job predates the project's current schema
// generation (or that have no job), and ignores documents already extracted
// under the current generation.
func TestBackfillStaleSchemaSelection(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	if _, err := db.NewRaw(`UPDATE kb.projects SET schema_generation = 3 WHERE id = ?`, projectID).Exec(ctx); err != nil {
		t.Fatalf("set generation: %v", err)
	}

	doc := func() string {
		id := uuid.NewString()
		_, err := db.NewRaw(`INSERT INTO kb.documents (id, project_id, filename, created_at, updated_at)
			VALUES (?, ?, 'doc.md', now(), now())`, id, projectID).Exec(ctx)
		require.NoError(t, err)
		return id
	}
	job := func(documentID string, generation *int) {
		id := uuid.NewString()
		_, err := db.NewRaw(`INSERT INTO kb.object_extraction_jobs
			(id, project_id, document_id, status, schema_generation, completed_at, created_at, updated_at)
			VALUES (?, ?, ?, 'completed', ?, now(), now(), now())`, id, projectID, documentID, generation).Exec(ctx)
		require.NoError(t, err)
	}

	currentGen := 3
	staleGen := 2

	currentDoc := doc()
	job(currentDoc, &currentGen) // already extracted under the current schema

	staleDoc := doc()
	job(staleDoc, &staleGen) // extracted before the schema changed

	neverDoc := doc() // no job at all

	svc := NewObjectExtractionJobsService(db, quietLogger(), DefaultObjectExtractionConfig())

	res, err := svc.Backfill(ctx, projectID, BackfillStaleSchema, 10, true)
	require.NoError(t, err)
	require.Equal(t, 3, res.CurrentGeneration)
	require.Equal(t, 2, res.Selected)

	selected := map[string]bool{}
	for _, id := range res.DocumentIDs {
		selected[id] = true
	}
	require.True(t, selected[staleDoc], "stale document must be selected")
	require.True(t, selected[neverDoc], "never-extracted document must be selected")
	require.False(t, selected[currentDoc], "current document must not be selected")
}

// TestBackfillAllSelection pins that the "all" filter selects every document in
// the project regardless of current extraction state — the "full" re-extraction
// mode — while respecting the LIMIT.
func TestBackfillAllSelection(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	doc := func() string {
		id := uuid.NewString()
		_, err := db.NewRaw(`INSERT INTO kb.documents (id, project_id, filename, created_at, updated_at)
			VALUES (?, ?, 'doc.md', now(), now())`, id, projectID).Exec(ctx)
		require.NoError(t, err)
		return id
	}

	// One already extracted (completed under current generation), one never
	// extracted, one failed — "all" must still select every one of them.
	currentGen := 1
	completed := doc()
	_, err := db.NewRaw(`INSERT INTO kb.object_extraction_jobs
		(id, project_id, document_id, status, schema_generation, completed_at, created_at, updated_at)
		VALUES (?, ?, ?, 'completed', ?, now(), now(), now())`, uuid.NewString(), projectID, completed, currentGen).Exec(ctx)
	require.NoError(t, err)

	never := doc()

	failed := doc()
	_, err = db.NewRaw(`INSERT INTO kb.object_extraction_jobs
		(id, project_id, document_id, status, completed_at, created_at, updated_at)
		VALUES (?, ?, ?, 'failed', now(), now(), now())`, uuid.NewString(), projectID, failed).Exec(ctx)
	require.NoError(t, err)

	svc := NewObjectExtractionJobsService(db, quietLogger(), DefaultObjectExtractionConfig())
	res, err := svc.Backfill(ctx, projectID, BackfillAll, 10, true)
	require.NoError(t, err)
	require.Equal(t, 3, res.Selected)

	selected := map[string]bool{}
	for _, id := range res.DocumentIDs {
		selected[id] = true
	}
	require.True(t, selected[completed], "completed document must be selected by all")
	require.True(t, selected[never], "never-extracted document must be selected by all")
	require.True(t, selected[failed], "failed document must be selected by all")
}

// TestBackfillFailedIsLatestTerminal pins that a document whose latest job
// succeeded is not re-selected by the failed filter even though it has an
// earlier failed job.
func TestBackfillFailedIsLatestTerminal(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	docID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.documents (id, project_id, filename, created_at, updated_at)
		VALUES (?, ?, 'doc.md', now(), now())`, docID, projectID).Exec(ctx)
	require.NoError(t, err)

	// A failed job, then a later completed job for the same document.
	_, err = db.NewRaw(`INSERT INTO kb.object_extraction_jobs
		(id, project_id, document_id, status, completed_at, created_at, updated_at)
		VALUES (?, ?, ?, 'failed', now() - interval '1 hour', now() - interval '1 hour', now() - interval '1 hour')`,
		uuid.NewString(), projectID, docID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.object_extraction_jobs
		(id, project_id, document_id, status, completed_at, created_at, updated_at)
		VALUES (?, ?, ?, 'completed', now(), now(), now())`,
		uuid.NewString(), projectID, docID).Exec(ctx)
	require.NoError(t, err)

	svc := NewObjectExtractionJobsService(db, quietLogger(), DefaultObjectExtractionConfig())
	res, err := svc.Backfill(ctx, projectID, BackfillFailed, 10, true)
	require.NoError(t, err)
	require.Equal(t, 0, res.Selected, "a later success must clear the failed filter")
}
