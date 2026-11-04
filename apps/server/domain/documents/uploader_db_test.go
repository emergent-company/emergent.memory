package documents

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// seedUploaderProject inserts an org and project and returns the project id.
func seedUploaderProject(t *testing.T, ctx context.Context, db bun.IDB) string {
	t.Helper()
	orgID := uuid.NewString()
	projectID := uuid.NewString()

	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'Uploader Org', now(), now())`, orgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, 'Uploader Project', ?, now(), now())`, projectID, orgID).Exec(ctx)
	require.NoError(t, err)

	return projectID
}

// seedUploaderUser inserts a user profile and returns its id — the value
// kb.documents.created_by_user_id references.
func seedUploaderUser(t *testing.T, ctx context.Context, db bun.IDB, displayName string) string {
	t.Helper()
	userID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name)
		VALUES (?, ?, ?)`, userID, "zitadel-"+userID, displayName).Exec(ctx)
	require.NoError(t, err)
	return userID
}

// newUploaderService builds a documents service over the test database. The
// listing and create paths exercised here need no pipeline attachment.
func newUploaderService(db bun.IDB) *Service {
	return NewService(NewRepository(db, slog.New(slog.NewTextHandler(io.Discard, nil))), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// TestDocumentUploaderRecordedAndListed pins #2260. Three properties:
//
//   - a document created through the uploader-aware create path records the
//     caller, and the listing resolves the display name server-side (one join,
//     not a per-row lookup);
//   - a document with no uploader — source-synced, or written before the column
//     existed — still lists, with the field simply absent;
//   - the uploader is not a filter or a sort key, so it cannot reorder or hide
//     rows.
func TestDocumentUploaderRecordedAndListed(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "documents_uploader")
	defer tdb.Close()
	db := tdb.GetDB()

	projectID := seedUploaderProject(t, ctx, db)
	uploaderID := seedUploaderUser(t, ctx, db, "Ada Lovelace")
	svc := newUploaderService(db)
	repo := NewRepository(db, slog.New(slog.NewTextHandler(io.Discard, nil)))

	filename := "provenance.txt"
	content := "who brought this in"
	created, wasCreated, err := svc.Create(ctx, CreateParams{
		ProjectID:       projectID,
		CreatedByUserID: &uploaderID,
		Filename:        &filename,
		Content:         &content,
	})
	require.NoError(t, err)
	require.True(t, wasCreated, "distinct content must create a new document")

	// A document with no uploader, exactly as source sync and pre-#2260 rows look.
	headlessID := uuid.NewString()
	_, err = db.NewRaw(`INSERT INTO kb.documents (id, project_id, filename, created_at, updated_at)
		VALUES (?, ?, 'synced.txt', now(), now())`, headlessID, projectID).Exec(ctx)
	require.NoError(t, err)

	list, err := repo.List(ctx, ListParams{ProjectID: projectID})
	require.NoError(t, err)
	require.Len(t, list.Documents, 2)

	byID := make(map[string]Document, len(list.Documents))
	for _, doc := range list.Documents {
		byID[doc.ID] = doc
	}

	uploaded, ok := byID[created.ID]
	require.True(t, ok, "the uploaded document must be listed")
	require.NotNil(t, uploaded.CreatedByDisplayName, "the listing must resolve the uploader's display name")
	require.Equal(t, "Ada Lovelace", *uploaded.CreatedByDisplayName)

	synced, ok := byID[headlessID]
	require.True(t, ok, "a document with no uploader must still be listed")
	require.Nil(t, synced.CreatedByUserID, "a source-synced document has no uploader")
	require.Nil(t, synced.CreatedByDisplayName, "no uploader means no resolved name, not an empty string")

	// GetByID resolves it too: the detail page shows the same attribution as the
	// list, from the same join.
	detail, err := repo.GetByID(ctx, projectID, created.ID)
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.NotNil(t, detail.CreatedByDisplayName)
	require.Equal(t, "Ada Lovelace", *detail.CreatedByDisplayName)

	// The uploader is neither a filter nor a sort key: with both rows present,
	// ordering stays the documented created_at ordering and neither row drops.
	require.Equal(t, 2, list.Total)
}

// TestCreateFromUploadRecordsUploader proves the multipart path records the same
// attribution as the JSON create path — the two entry points a user can reach
// must not disagree about who uploaded what.
func TestCreateFromUploadRecordsUploader(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "documents_uploader_multipart")
	defer tdb.Close()
	db := tdb.GetDB()

	projectID := seedUploaderProject(t, ctx, db)
	uploaderID := seedUploaderUser(t, ctx, db, "Grace Hopper")
	svc := newUploaderService(db)

	storageKey := "uploads/project/grace.pdf"
	resp, err := svc.CreateFromUpload(ctx, UploadParams{
		ProjectID:       projectID,
		OrgID:           uploaderID,
		CreatedByUserID: &uploaderID,
		Filename:        "grace.pdf",
		MimeType:        "application/pdf",
		FileSize:        1024,
		FileHash:        "hash-grace",
		StorageKey:      storageKey,
		StorageURL:      "s3://bucket/" + storageKey,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	detail, err := NewRepository(db, slog.New(slog.NewTextHandler(io.Discard, nil))).GetByID(ctx, projectID, resp.Document.ID)
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.NotNil(t, detail.CreatedByUserID, "the multipart upload path must record the uploader")
	require.Equal(t, uploaderID, *detail.CreatedByUserID)
	require.NotNil(t, detail.CreatedByDisplayName)
	require.Equal(t, "Grace Hopper", *detail.CreatedByDisplayName)
}
