package documents

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestUploadHandlerRecordsUploader drives POST /api/documents/upload through the
// real handler, because that is the endpoint the web UI's Documents page uses
// (gateway `uiUploadDocument` → `Upload`). A service-level test cannot catch a
// handler that forgets to pass the uploader: `CreateFromUpload` records exactly
// what it is given, so the wiring is the thing under test here.
func TestUploadHandlerRecordsUploader(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "documents_upload_handler_uploader")
	defer tdb.Close()
	db := tdb.GetDB()

	projectID := seedUploaderProject(t, ctx, db)
	uploaderID := seedUploaderUser(t, ctx, db, "Ada Lovelace")

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewUploadHandler(
		NewService(NewRepository(db, log), log),
		&fakeStorage{enabled: true, uploadKey: "uploads/uploader.txt"},
		&fakeParsingJobs{},
		nil, // extraction jobs are only touched when autoExtract is set
		&config.Config{},
		log,
	)

	uploadAs := func(userID string) string {
		t.Helper()
		body, contentType := multipartBody(t, "file", "uploader.txt", []byte("who brought this in"))
		c, rec := echoCtxWithUser(t, http.MethodPost, "/api/documents/upload", body, contentType)
		c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: userID, OrgID: "org-" + projectID, ProjectID: projectID})

		require.NoError(t, handler.Upload(c))
		require.Equal(t, http.StatusCreated, rec.Code)

		var documentID string
		require.NoError(t, db.NewSelect().
			TableExpr("kb.documents").
			Column("id").
			Where("project_id = ?", projectID).
			Order("created_at DESC").
			Limit(1).
			Scan(ctx, &documentID))
		return documentID
	}

	attributed := uploadAs(uploaderID)
	var recorded *string
	require.NoError(t, db.NewSelect().
		TableExpr("kb.documents").
		Column("created_by_user_id").
		Where("id = ?", attributed).
		Scan(ctx, &recorded))
	require.NotNil(t, recorded, "POST /api/documents/upload must record the authenticated uploader")
	require.Equal(t, uploaderID, *recorded)

	// An ephemeral sandbox token has no owning user (auth builds it with an empty
	// id). The row must still be created, just unattributed.
	unattributed := uploadAs("")
	var sandboxed *string
	require.NoError(t, db.NewSelect().
		TableExpr("kb.documents").
		Column("created_by_user_id").
		Where("id = ?", unattributed).
		Scan(ctx, &sandboxed))
	require.Nil(t, sandboxed, "a caller with no user id must not be attributed")
}

// TestUploadForRememberRecordsUploader covers the chat attachment path, which is
// a user action even though its signature carries the id as a plain string.
func TestUploadForRememberRecordsUploader(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "documents_remember_uploader")
	defer tdb.Close()
	db := tdb.GetDB()

	projectID := seedUploaderProject(t, ctx, db)
	uploaderID := seedUploaderUser(t, ctx, db, "Grace Hopper")

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewUploadHandler(
		NewService(NewRepository(db, log), log),
		&fakeStorage{enabled: true, uploadKey: "uploads/remember.txt"},
		&fakeParsingJobs{},
		nil,
		&config.Config{},
		log,
	)

	body, contentType := multipartBody(t, "file", "remember.txt", []byte("attached in chat"))
	c, _ := echoCtxWithUser(t, http.MethodPost, "/api/documents/upload", body, contentType)
	fileHeader, err := c.FormFile("file")
	require.NoError(t, err)
	require.Contains(t, contentType, "multipart/form-data")

	result, err := handler.UploadForRemember(ctx, "org-"+projectID, projectID, uploaderID, fileHeader, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	var recorded *string
	require.NoError(t, db.NewSelect().
		TableExpr("kb.documents").
		Column("created_by_user_id").
		Where("id = ?", result.DocumentID).
		Scan(ctx, &recorded))
	require.NotNil(t, recorded, "a chat attachment is uploaded by a person and must be attributed")
	require.Equal(t, uploaderID, *recorded)
}

// TestUploadBatchRecordsUploader covers the batch worker path
// (`UploadBatch` → `processFileUpload`), which builds its own UploadParams. Every
// create path in this package must attribute independently: the round-one defect
// was exactly one handler that forgot, and it stayed invisible while its sibling
// was wired.
func TestUploadBatchRecordsUploader(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "documents_batch_uploader")
	defer tdb.Close()
	db := tdb.GetDB()

	projectID := seedUploaderProject(t, ctx, db)
	uploaderID := seedUploaderUser(t, ctx, db, "Ada Lovelace")

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewUploadHandler(
		NewService(NewRepository(db, log), log),
		&fakeStorage{enabled: true, uploadKey: "uploads/batch.txt"},
		&fakeParsingJobs{},
		nil,
		&config.Config{},
		log,
	)

	body, contentType := batchMultipartBody(t, map[string][]byte{
		"first.txt":  []byte("first batch file"),
		"second.txt": []byte("second batch file"),
	})
	c, rec := echoCtxWithUser(t, http.MethodPost, "/api/documents/upload/batch", body, contentType)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: uploaderID, OrgID: "org-" + projectID, ProjectID: projectID})

	require.NoError(t, handler.UploadBatch(c))
	require.Equal(t, http.StatusOK, rec.Code)

	names := make([]string, 0, 2)
	require.NoError(t, db.NewSelect().
		TableExpr("kb.documents").
		Column("filename").
		Where("project_id = ?", projectID).
		Order("filename ASC").
		Scan(ctx, &names))
	require.Equal(t, []string{"first.txt", "second.txt"}, names)

	for _, name := range names {
		var recorded *string
		require.NoError(t, db.NewSelect().
			TableExpr("kb.documents").
			Column("created_by_user_id").
			Where("project_id = ? AND filename = ?", projectID, name).
			Scan(ctx, &recorded))
		require.NotNil(t, recorded, "batch upload must record the uploader for %s", name)
		require.Equal(t, uploaderID, *recorded)
	}
}

// TestCreateHandlerRecordsUploader covers POST /api/documents — the JSON
// create path, which shares nothing with the multipart handlers but the same
// obligation to attribute what a person created.
func TestCreateHandlerRecordsUploader(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "documents_create_handler_uploader")
	defer tdb.Close()
	db := tdb.GetDB()

	projectID := seedUploaderProject(t, ctx, db)
	uploaderID := seedUploaderUser(t, ctx, db, "Ada Lovelace")

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// chunking and extraction are nil: a raw-content create returns before them
	// once conversion is marked complete, which is the path under test.
	handler := NewHandler(NewService(NewRepository(db, log), log), nil, nil, nil, nil, log)

	payload, err := json.Marshal(CreateDocumentRequest{Filename: "json-create.txt", Content: "created through the JSON endpoint"})
	require.NoError(t, err)
	c, rec := echoCtxWithUser(t, http.MethodPost, "/api/documents", strings.NewReader(string(payload)), "application/json")
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: uploaderID, OrgID: "org-" + projectID, ProjectID: projectID})

	require.NoError(t, handler.Create(c))
	require.Equal(t, http.StatusCreated, rec.Code)

	var recorded *string
	require.NoError(t, db.NewSelect().
		TableExpr("kb.documents").
		Column("created_by_user_id").
		Where("project_id = ? AND filename = ?", projectID, "json-create.txt").
		Scan(ctx, &recorded))
	require.NotNil(t, recorded, "POST /api/documents must record the uploader")
	require.Equal(t, uploaderID, *recorded)
}
