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
	"github.com/uptrace/bun"

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
		nil, // project auto-extract reader
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
		nil, // project auto-extract reader
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
		nil, // project auto-extract reader
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
	handler := NewHandler(NewService(NewRepository(db, log), log), nil, nil, nil, nil, nil, log)

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

// dbAutoExtractReader resolves the project flag straight from the test DB, so
// these tests exercise the handler's default-resolution path without importing
// the extraction package (which would be an import cycle from documents tests).
type dbAutoExtractReader struct {
	db bun.IDB
}

func (r dbAutoExtractReader) AutoExtractObjectsForProject(ctx context.Context, projectID string) (bool, error) {
	var enabled bool
	err := r.db.NewSelect().
		TableExpr("kb.projects").
		Column("auto_extract_objects").
		Where("id = ?", projectID).
		Scan(ctx, &enabled)
	return enabled, err
}

// TestUploadHandlerProjectAutoExtractDefault pins #498 for the single upload
// path: when the request does not specify autoExtract, the project setting
// decides; an explicit autoExtract=false must still override a project that has
// auto-extraction enabled.
func TestUploadHandlerProjectAutoExtractDefault(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "documents_upload_autoextract")
	defer tdb.Close()
	db := tdb.GetDB()

	projectID := seedUploaderProject(t, ctx, db)
	_, err := db.NewRaw(`UPDATE kb.projects SET auto_extract_objects = true WHERE id = ?`, projectID).Exec(ctx)
	require.NoError(t, err)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	newHandler := func(jobs *fakeParsingJobs) *UploadHandler {
		return NewUploadHandler(
			NewService(NewRepository(db, log), log),
			&fakeStorage{enabled: true, uploadKey: "uploads/autoextract.txt"},
			jobs,
			nil,
			dbAutoExtractReader{db: db},
			&config.Config{},
			log,
		)
	}

	upload := func(t *testing.T, h *UploadHandler, filename string, body []byte, formFields map[string]string) {
		t.Helper()
		buf, contentType := multipartBodyWithFields(t, "file", filename, body, formFields)
		c, rec := echoCtxWithUser(t, http.MethodPost, "/api/documents/upload", buf, contentType)
		c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "", OrgID: "org-" + projectID, ProjectID: projectID})
		require.NoError(t, h.Upload(c))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}

	t.Run("no field uses project true", func(t *testing.T) {
		jobs := &fakeParsingJobs{}
		upload(t, newHandler(jobs), "project-default.txt", []byte("project default body"), nil)
		require.Len(t, jobs.got, 1)
		require.True(t, jobs.got[0].AutoExtract, "project auto_extract_objects=true must be the default")
	})

	t.Run("explicit false overrides project true", func(t *testing.T) {
		jobs := &fakeParsingJobs{}
		upload(t, newHandler(jobs), "explicit-false.txt", []byte("explicit false body"), map[string]string{"autoExtract": "false"})
		require.Len(t, jobs.got, 1)
		require.False(t, jobs.got[0].AutoExtract, "explicit autoExtract=false must override the project default")
	})

	t.Run("explicit true with project false", func(t *testing.T) {
		offProject := seedUploaderProject(t, ctx, db)
		jobs := &fakeParsingJobs{}
		h := NewUploadHandler(
			NewService(NewRepository(db, log), log),
			&fakeStorage{enabled: true, uploadKey: "uploads/autoextract-off.txt"},
			jobs,
			nil,
			dbAutoExtractReader{db: db},
			&config.Config{},
			log,
		)
		buf, contentType := multipartBodyWithFields(t, "file", "explicit-true.txt", []byte("explicit true body"), map[string]string{"autoExtract": "true"})
		c, rec := echoCtxWithUser(t, http.MethodPost, "/api/documents/upload", buf, contentType)
		c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "", OrgID: "org-" + offProject, ProjectID: offProject})
		require.NoError(t, h.Upload(c))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		require.Len(t, jobs.got, 1)
		require.True(t, jobs.got[0].AutoExtract)
	})

	t.Run("project false and no field stays false", func(t *testing.T) {
		offProject := seedUploaderProject(t, ctx, db)
		jobs := &fakeParsingJobs{}
		h := NewUploadHandler(
			NewService(NewRepository(db, log), log),
			&fakeStorage{enabled: true, uploadKey: "uploads/autoextract-off2.txt"},
			jobs,
			nil,
			dbAutoExtractReader{db: db},
			&config.Config{},
			log,
		)
		buf, contentType := multipartBodyWithFields(t, "file", "project-off.txt", []byte("project off body"), nil)
		c, rec := echoCtxWithUser(t, http.MethodPost, "/api/documents/upload", buf, contentType)
		c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "", OrgID: "org-" + offProject, ProjectID: offProject})
		require.NoError(t, h.Upload(c))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		require.Len(t, jobs.got, 1)
		require.False(t, jobs.got[0].AutoExtract)
	})
}
