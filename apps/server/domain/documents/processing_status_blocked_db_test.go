package documents

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestProcessingStatusBlockedAndNotEnqueuedReasons pins the durable-state
// derivations behind the "blocked" processing status and the
// extraction-not-enqueued reasons. Both are computed in SQL from rows the
// workers already write — no dedicated state column exists — so this test
// exercises the real projection through GetByID and List.
func TestProcessingStatusBlockedAndNotEnqueuedReasons(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "documents_processing_status_blocked")
	defer tdb.Close()
	db := tdb.GetDB()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'Blocked Org', now(), now())`, orgID).Exec(ctx)
	require.NoError(t, err)

	// Project defaults: auto_extract_source_syncs=false and
	// auto_extract_objects=false. Documents land here precisely because both
	// are disabled.
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, 'Blocked Project', ?, now(), now())`, projectID, orgID).Exec(ctx)
	require.NoError(t, err)

	now := time.Now()
	insertDoc := func(sourceType string, sourceID *string) string {
		t.Helper()
		id := uuid.NewString()
		_, err := db.NewRaw(
			`INSERT INTO kb.documents
			   (id, project_id, filename, mime_type, source_type, source_id, conversion_status, created_at, updated_at)
			 VALUES (?, ?, 'doc.md', 'text/markdown', ?, ?, 'completed', ?, ?)`,
			id, projectID, sourceType, sourceID, now, now).Exec(ctx)
		require.NoError(t, err)
		return id
	}

	repo := NewRepository(db, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// documents.source_id is FK-constrained to kb.sources, so the source-synced
	// document needs a real source row before it can be inserted.
	repoSourceID := uuid.NewString()
	_, err = db.NewRaw(`INSERT INTO kb.sources (id, project_id, provider, created_at, updated_at)
		VALUES (?, ?, 'repo', now(), now())`, repoSourceID, projectID).Exec(ctx)
	require.NoError(t, err)

	docA := insertDoc("repo", &repoSourceID)
	docB := insertDoc("upload", nil)
	docC := insertDoc("upload", nil)

	// Document A: a pending embedding job whose last_error is the budget
	// signal, plus no extraction job at all.
	chunkID := uuid.NewString()
	_, err = db.NewRaw(
		`INSERT INTO kb.chunks (id, document_id, chunk_index, text, created_at, updated_at)
		 VALUES (?, ?, 0, 'hello world', now(), now())`, chunkID, docA).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(
		`INSERT INTO kb.chunk_embedding_jobs (chunk_id, status, last_error, scheduled_at)
		 VALUES (?, 'pending', 'budget_exceeded', now())`, chunkID).Exec(ctx)
	require.NoError(t, err)

	// Document C: extraction was enqueued (even though it is terminal), so the
	// not-enqueued reason must be absent regardless of the project settings.
	_, err = db.NewRaw(
		`INSERT INTO kb.object_extraction_jobs (project_id, document_id, status, created_at)
		 VALUES (?, ?, 'completed', now())`, projectID, docC).Exec(ctx)
	require.NoError(t, err)

	gotA, err := repo.GetByID(ctx, projectID, docA)
	require.NoError(t, err)
	require.NotNil(t, gotA)
	require.NotNil(t, gotA.ProcessingStatus)
	assert.Equal(t, ProcessingStatusBlocked, *gotA.ProcessingStatus)
	require.NotNil(t, gotA.ProcessingReason)
	assert.Equal(t, ProcessingReasonBudgetExceeded, *gotA.ProcessingReason)
	require.NotNil(t, gotA.ExtractionNotEnqueuedReason)
	assert.Equal(t, ExtractionNotEnqueuedSourceSyncsDisabled, *gotA.ExtractionNotEnqueuedReason)

	gotB, err := repo.GetByID(ctx, projectID, docB)
	require.NoError(t, err)
	require.NotNil(t, gotB)
	require.NotNil(t, gotB.ProcessingStatus)
	assert.Equal(t, "ready_for_extraction", *gotB.ProcessingStatus)
	assert.Nil(t, gotB.ProcessingReason)
	require.NotNil(t, gotB.ExtractionNotEnqueuedReason)
	assert.Equal(t, ExtractionNotEnqueuedProjectDisabled, *gotB.ExtractionNotEnqueuedReason)

	gotC, err := repo.GetByID(ctx, projectID, docC)
	require.NoError(t, err)
	require.NotNil(t, gotC)
	assert.Nil(t, gotC.ExtractionNotEnqueuedReason, "an existing extraction job means extraction was enqueued")

	// The listing must project the same derived columns as GetByID: a badge
	// that disagrees with the detail response would be indistinguishable from
	// a fixed bug that is not fixed.
	list, err := repo.List(ctx, ListParams{ProjectID: projectID, Limit: 10})
	require.NoError(t, err)
	require.NotNil(t, list)

	byID := map[string]Document{}
	for _, doc := range list.Documents {
		byID[doc.ID] = doc
	}
	require.Contains(t, byID, docA)
	require.Contains(t, byID, docB)

	listedA := byID[docA]
	require.NotNil(t, listedA.ProcessingStatus)
	assert.Equal(t, ProcessingStatusBlocked, *listedA.ProcessingStatus)
	require.NotNil(t, listedA.ProcessingReason)
	assert.Equal(t, ProcessingReasonBudgetExceeded, *listedA.ProcessingReason)
	require.NotNil(t, listedA.ExtractionNotEnqueuedReason)
	assert.Equal(t, ExtractionNotEnqueuedSourceSyncsDisabled, *listedA.ExtractionNotEnqueuedReason)

	listedB := byID[docB]
	require.NotNil(t, listedB.ProcessingStatus)
	assert.Equal(t, "ready_for_extraction", *listedB.ProcessingStatus)
	assert.Nil(t, listedB.ProcessingReason)
	require.NotNil(t, listedB.ExtractionNotEnqueuedReason)
	assert.Equal(t, ExtractionNotEnqueuedProjectDisabled, *listedB.ExtractionNotEnqueuedReason)

	assert.Equal(t, 1, list.StatusCounts.Blocked)
}
