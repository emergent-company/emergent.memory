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

// TestRepositoryFindUploadByFileHash is the SQL-level regression test for the
// upload dedupe: a retried MCP upload must find the document its bytes already
// produced, including while that document's parse is still in flight (the window
// a retry actually lands in), and must not find a document whose parse reached a
// terminal failure — returning that one would make the retry inherit an empty
// document instead of retrying the pipeline.
func TestRepositoryFindUploadByFileHash(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "documents_find_upload_by_file_hash")
	defer tdb.Close()
	db := tdb.GetDB()

	projectID := seedUploaderProject(t, ctx, db)

	insertDoc := func(project string, fileHash string, content *string, conversionStatus string, sourceStatus string, createdAt time.Time) string {
		t.Helper()
		id := uuid.NewString()
		_, err := db.NewRaw(
			`INSERT INTO kb.documents
			   (id, project_id, filename, mime_type, content, source_type, conversion_status, source_status, file_hash, created_at)
			 VALUES (?, ?, 'upload.md', 'text/markdown', ?, 'upload', ?, ?, ?, ?)`,
			id, project, content, conversionStatus, sourceStatus, fileHash, createdAt).Exec(ctx)
		require.NoError(t, err)
		return id
	}
	insertJob := func(documentID string, status string) {
		t.Helper()
		_, err := db.NewRaw(
			`INSERT INTO kb.document_parsing_jobs (project_id, status, source_type, document_id)
			 VALUES (?, ?, 'file_upload', ?)`, projectID, status, documentID).Exec(ctx)
		require.NoError(t, err)
	}

	now := time.Now()
	text := "parsed text"
	repo := NewRepository(db, slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("a parse still in flight is the retry target", func(t *testing.T) {
		hash := computeFileHash([]byte("in flight"))
		pending := insertDoc(projectID, hash, nil, "pending", "active", now.Add(-time.Minute))
		insertJob(pending, "pending")

		found, err := repo.FindUploadByFileHash(ctx, projectID, hash)
		require.NoError(t, err)
		require.NotNil(t, found, "a retry must match a document whose parse has not finished yet")
		assert.Equal(t, pending, found.ID)
	})

	t.Run("a parsed document is the retry target", func(t *testing.T) {
		hash := computeFileHash([]byte("parsed"))
		parsed := insertDoc(projectID, hash, &text, "completed", "active", now.Add(-time.Hour))

		found, err := repo.FindUploadByFileHash(ctx, projectID, hash)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, parsed, found.ID)
	})

	t.Run("a terminally failed parse is not", func(t *testing.T) {
		hash := computeFileHash([]byte("failed"))
		failed := insertDoc(projectID, hash, nil, "failed", "active", now.Add(-time.Hour))
		insertJob(failed, "failed")

		found, err := repo.FindUploadByFileHash(ctx, projectID, hash)
		require.NoError(t, err)
		assert.Nil(t, found, "an empty document must not be returned as if the retry had succeeded")
	})

	t.Run("neither is a document with no job and no text", func(t *testing.T) {
		hash := computeFileHash([]byte("orphan"))
		insertDoc(projectID, hash, nil, "not_required", "active", now.Add(-time.Hour))

		found, err := repo.FindUploadByFileHash(ctx, projectID, hash)
		require.NoError(t, err)
		assert.Nil(t, found, "an upload that never enqueued a parse is not a dedupe target")
	})

	t.Run("a document removed at its source is not", func(t *testing.T) {
		hash := computeFileHash([]byte("deleted"))
		insertDoc(projectID, hash, &text, "completed", SourceStatusDeletedFromSource, now.Add(-time.Hour))

		found, err := repo.FindUploadByFileHash(ctx, projectID, hash)
		require.NoError(t, err)
		assert.Nil(t, found)
	})

	t.Run("dedupe is scoped to the project", func(t *testing.T) {
		otherOrg := uuid.NewString()
		otherProject := uuid.NewString()
		_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at)
			VALUES (?, 'Other Org', now(), now())`, otherOrg).Exec(ctx)
		require.NoError(t, err)
		_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
			VALUES (?, 'Other Project', ?, now(), now())`, otherProject, otherOrg).Exec(ctx)
		require.NoError(t, err)

		hash := computeFileHash([]byte("elsewhere"))
		insertDoc(otherProject, hash, &text, "completed", "active", now.Add(-time.Hour))

		found, err := repo.FindUploadByFileHash(ctx, projectID, hash)
		require.NoError(t, err)
		assert.Nil(t, found, "an upload is deduplicated within its own project only")
	})

	t.Run("the newest match wins", func(t *testing.T) {
		hash := computeFileHash([]byte("twice"))
		insertDoc(projectID, hash, &text, "completed", "active", now.Add(-2*time.Hour))
		newest := insertDoc(projectID, hash, &text, "completed", "active", now.Add(-time.Hour))

		found, err := repo.FindUploadByFileHash(ctx, projectID, hash)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, newest, found.ID)
	})

	t.Run("an empty hash never matches", func(t *testing.T) {
		found, err := repo.FindUploadByFileHash(ctx, projectID, "")
		require.NoError(t, err)
		assert.Nil(t, found)
	})
}

// TestServiceFindExistingUploadByHashReturnsExplicitDuplicate pins the response
// the MCP tool relies on: a duplicate is reported as one, with the existing
// document named, never as a silent success on a new id.
func TestServiceFindExistingUploadByHashReturnsExplicitDuplicate(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "documents_find_existing_upload_response")
	defer tdb.Close()
	db := tdb.GetDB()

	projectID := seedUploaderProject(t, ctx, db)
	repo := NewRepository(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc := NewService(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	hash := computeFileHash([]byte("same bytes"))
	text := "the text"
	docID := uuid.NewString()
	_, err := db.NewRaw(
		`INSERT INTO kb.documents
		   (id, project_id, filename, mime_type, content, source_type, conversion_status, source_status, file_hash, created_at)
		 VALUES (?, ?, 'notes.md', 'text/markdown', ?, 'upload', 'completed', 'active', ?, now())`,
		docID, projectID, text, hash).Exec(ctx)
	require.NoError(t, err)

	resp, err := svc.findExistingUploadByHash(ctx, projectID, hash)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.IsDuplicate)
	require.NotNil(t, resp.ExistingDocumentID)
	assert.Equal(t, docID, *resp.ExistingDocumentID)
	require.NotNil(t, resp.Document)
	assert.Equal(t, docID, resp.Document.ID)

	missing, err := svc.findExistingUploadByHash(ctx, projectID, computeFileHash([]byte("other bytes")))
	require.NoError(t, err)
	assert.Nil(t, missing)
}
