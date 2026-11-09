package extraction_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// autoExtractTestStorage is a minimal uploadStorage for the extraction adapter test.
type autoExtractTestStorage struct{}

func (autoExtractTestStorage) Enabled() bool { return true }

func (autoExtractTestStorage) UploadDocument(_ context.Context, _ io.Reader, _ int64, _ storage.DocumentUploadOptions) (*storage.UploadResult, error) {
	return &storage.UploadResult{Key: "uploads/adapter-test.txt", StorageURL: "http://fake/uploads/adapter-test.txt"}, nil
}

func (autoExtractTestStorage) Delete(context.Context, string) error { return nil }

// autoExtractTestJobs records ParsingJobOptions passed by the upload handler.
type autoExtractTestJobs struct {
	got []documents.ParsingJobOptions
}

func (j *autoExtractTestJobs) CreateJob(_ context.Context, opts documents.ParsingJobOptions) error {
	j.got = append(j.got, opts)
	return nil
}

func autoExtractTestMultipart(t *testing.T, filename string, data []byte, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	for name, value := range fields {
		require.NoError(t, w.WriteField(name, value))
	}
	part, err := w.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf, w.FormDataContentType()
}

func seedAutoExtractTestProject(t *testing.T, ctx context.Context, testDB *testutil.TestDB) string {
	t.Helper()
	orgID := uuid.NewString()
	projectID := uuid.NewString()
	db := testDB.GetDB()
	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'AutoExtract Org', now(), now())`, orgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, 'AutoExtract Project', ?, now(), now())`, projectID, orgID).Exec(ctx)
	require.NoError(t, err)
	return projectID
}

// TestProjectAutoExtractReaderAdapterWiredIntoUpload exercises the real
// projectAutoExtractReaderAdapter provided to documents.NewUploadHandler: a
// project with auto_extract_objects=true makes an upload that omits autoExtract
// enqueue a parsing job with AutoExtract=true, while an explicit false overrides.
func TestProjectAutoExtractReaderAdapterWiredIntoUpload(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "documents_project_autoextract_reader")
	defer testDB.Close()

	projectID := seedAutoExtractTestProject(t, ctx, testDB)
	_, err := testDB.GetDB().NewRaw(`UPDATE kb.projects SET auto_extract_objects = true WHERE id = ?`, projectID).Exec(ctx)
	require.NoError(t, err)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	docSvc := documents.NewService(documents.NewRepository(testDB.GetDB(), log), log)

	newHandler := func(jobs *autoExtractTestJobs) *documents.UploadHandler {
		return documents.NewUploadHandler(
			docSvc,
			autoExtractTestStorage{},
			jobs,
			nil,
			extraction.NewProjectAutoExtractReader(projects.NewRepository(testDB.GetDB(), log)),
			&config.Config{},
			log,
		)
	}

	upload := func(t *testing.T, h *documents.UploadHandler, filename string, body []byte, fields map[string]string) {
		t.Helper()
		buf, contentType := autoExtractTestMultipart(t, filename, body, fields)
		req := httptest.NewRequest(http.MethodPost, "/api/documents/upload", buf)
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		e := echo.New()
		c := e.NewContext(req, rec)
		c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "", OrgID: "org-" + projectID, ProjectID: projectID})
		require.NoError(t, h.Upload(c))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}

	t.Run("no field inherits project true", func(t *testing.T) {
		jobs := &autoExtractTestJobs{}
		upload(t, newHandler(jobs), "adapter-default.txt", []byte("adapter default body"), nil)
		require.Len(t, jobs.got, 1)
		require.True(t, jobs.got[0].AutoExtract, "project auto_extract_objects=true must default autoExtract on")
	})

	t.Run("explicit false overrides project true", func(t *testing.T) {
		jobs := &autoExtractTestJobs{}
		upload(t, newHandler(jobs), "adapter-explicit-false.txt", []byte("adapter explicit false body"), map[string]string{"autoExtract": "false"})
		require.Len(t, jobs.got, 1)
		require.False(t, jobs.got[0].AutoExtract, "explicit autoExtract=false must override the project flag")
	})
}
