package scheduler

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// documentRow is one kb.documents row the sweep test seeds. Every field the
// sweep inspects is explicit, so a case differs from its neighbours in exactly
// the dimension it tests.
type documentRow struct {
	filename         string
	content          *string
	sourceType       string
	conversionStatus string
	sourceStatus     string
	createdAt        time.Time
}

// TestDocumentParsingSweepTask_Run is the regression test for a document that
// exists with no parsing job: an upload enqueues its job outside the document
// insert's transaction, so a crash or a cancelled request in that window leaves
// a document that never parses. The sweep must give exactly that document a job
// and leave every neighbouring state alone.
func TestDocumentParsingSweepTask_Run(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "scheduler_document_parsing_sweep")
	defer tdb.Close()
	// The task guards concurrent sweepers with a transaction-scoped advisory
	// lock, so it needs the pool rather than the test's transaction handle.
	db, ok := tdb.GetDB().(*bun.DB)
	require.True(t, ok, "test database handle must be a *bun.DB")

	// kb.documents.project_id is a real foreign key, so seed the org and the
	// project the documents belong to.
	orgID := uuid.New().String()
	projectID := uuid.New().String()
	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'Sweep Org', now(), now())`, orgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, 'Sweep Project', ?, now(), now())`, projectID, orgID).Exec(ctx)
	require.NoError(t, err)

	old := time.Now().Add(-2 * time.Hour)

	seed := func(row documentRow) string {
		t.Helper()
		id := uuid.New().String()
		_, err := db.NewRaw(
			`INSERT INTO kb.documents
			   (id, project_id, filename, mime_type, content, source_type, conversion_status, source_status, storage_key, created_at)
			 VALUES (?, ?, ?, 'text/markdown', ?, ?, ?, ?, ?, ?)`,
			id, projectID, row.filename, row.content, row.sourceType,
			row.conversionStatus, row.sourceStatus, "uploads/"+row.filename, row.createdAt).Exec(ctx)
		require.NoError(t, err)
		return id
	}

	// The state the sweep exists for: an upload committed with no job and no text.
	orphan := seed(documentRow{filename: "orphan.md", sourceType: "upload",
		conversionStatus: "not_required", sourceStatus: "active", createdAt: old})

	// An old upload with no project at all. kb.documents.project_id is nullable
	// while kb.document_parsing_jobs.project_id is NOT NULL, so no job can name
	// it; the sweep must skip it and still enqueue the valid orphan beside it.
	// With a LEFT JOIN this single row failed the whole multi-row INSERT and the
	// sweep healed nothing.
	unowned := uuid.New().String()
	_, err = db.NewRaw(
		`INSERT INTO kb.documents
		   (id, project_id, filename, mime_type, content, source_type, conversion_status, source_status, storage_key, created_at)
		 VALUES (?, NULL, 'unowned.md', 'text/markdown', NULL, 'upload', 'not_required', 'active', 'uploads/unowned.md', ?)`,
		unowned, old).Exec(ctx)
	require.NoError(t, err)

	// Neighbours that must be left alone.
	tooRecent := seed(documentRow{filename: "fresh.md", sourceType: "upload",
		conversionStatus: "not_required", sourceStatus: "active", createdAt: time.Now().Add(-time.Minute)})

	parsed := "already parsed"
	hasContent := seed(documentRow{filename: "parsed.md", content: &parsed, sourceType: "upload",
		conversionStatus: "completed", sourceStatus: "active", createdAt: old})

	failedConversion := seed(documentRow{filename: "broken.pdf", sourceType: "upload",
		conversionStatus: "failed", sourceStatus: "active", createdAt: old})

	deletedFromSource := seed(documentRow{filename: "gone.md", sourceType: "upload",
		conversionStatus: "not_required", sourceStatus: "deleted_from_source", createdAt: old})

	sourceOwned := seed(documentRow{filename: "synced.md", sourceType: "github",
		conversionStatus: "not_required", sourceStatus: "active", createdAt: old})

	withJob := seed(documentRow{filename: "queued.md", sourceType: "upload",
		conversionStatus: "not_required", sourceStatus: "active", createdAt: old})
	_, err = db.NewRaw(
		`INSERT INTO kb.document_parsing_jobs (project_id, status, source_type, document_id)
		 VALUES (?, 'pending', 'file_upload', ?)`, projectID, withJob).Exec(ctx)
	require.NoError(t, err)

	task := NewDocumentParsingSweepTask(db, slog.New(slog.NewTextHandler(io.Discard, nil)), 15*time.Minute, 100)
	require.NoError(t, task.Run(ctx))

	jobCount := func(documentID string) int {
		t.Helper()
		var n int
		require.NoError(t, db.NewRaw(
			`SELECT COUNT(*) FROM kb.document_parsing_jobs WHERE document_id = ?`, documentID,
		).Scan(ctx, &n))
		return n
	}

	assert.Equal(t, 1, jobCount(orphan), "the orphaned upload must get exactly one parsing job")
	assert.Equal(t, 0, jobCount(unowned), "a document with no project cannot name one on a job, so it is skipped")
	assert.Equal(t, 0, jobCount(tooRecent), "an upload that is still in flight must not be swept")
	assert.Equal(t, 0, jobCount(hasContent), "a document with content is already parsed")
	assert.Equal(t, 0, jobCount(failedConversion), "a permanently failed parse must not be retried by the sweep")
	assert.Equal(t, 0, jobCount(deletedFromSource), "a document removed at its source must not be resurrected")
	assert.Equal(t, 0, jobCount(sourceOwned), "a data-source-owned document is that source's sync's job")
	assert.Equal(t, 1, jobCount(withJob), "a document that already has a job must not get a second one")

	var job struct {
		Status     string  `bun:"status"`
		SourceType string  `bun:"source_type"`
		Filename   *string `bun:"source_filename"`
		StorageKey *string `bun:"storage_key"`
		ProjectID  string  `bun:"project_id"`
		OrgID      *string `bun:"organization_id"`
		DocumentID *string `bun:"document_id"`
		Metadata   string  `bun:"metadata"`
	}
	require.NoError(t, db.NewRaw(
		`SELECT project_id, organization_id, document_id, status, source_type, source_filename, storage_key, metadata::text AS metadata
		 FROM kb.document_parsing_jobs WHERE document_id = ?`, orphan,
	).Scan(ctx, &job))
	assert.Equal(t, "pending", job.Status)
	assert.Equal(t, "file_upload", job.SourceType)
	require.NotNil(t, job.DocumentID)
	assert.Equal(t, orphan, *job.DocumentID)
	assert.Equal(t, projectID, job.ProjectID)
	require.NotNil(t, job.OrgID, "the job must carry the project's organization, as an upload-enqueued job does")
	assert.Equal(t, orgID, *job.OrgID)
	require.NotNil(t, job.Filename)
	assert.Equal(t, "orphan.md", *job.Filename)
	require.NotNil(t, job.StorageKey)
	assert.Equal(t, "uploads/orphan.md", *job.StorageKey, "the job must carry what the parser needs to read the file")
	// The sweep restores parsing, never extraction spend: no auto_extract flag.
	assert.NotContains(t, job.Metadata, "auto_extract")
	assert.Contains(t, job.Metadata, "swept")

	// Idempotent: a second run must not enqueue a second job for the same document.
	require.NoError(t, task.Run(ctx))
	assert.Equal(t, 1, jobCount(orphan), "a second sweep must not enqueue a duplicate job")
}

// TestNewDocumentParsingSweepTask_Defaults keeps the constructor's contract: a
// non-positive age or batch size falls back to the defaults rather than
// sweeping every document at once.
func TestNewDocumentParsingSweepTask_Defaults(t *testing.T) {
	task := NewDocumentParsingSweepTask(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), 0, -1)
	assert.Equal(t, defaultDocumentParsingSweepMinAge, task.minAge)
	assert.Equal(t, defaultDocumentParsingSweepBatchSize, task.batchSize)
}
