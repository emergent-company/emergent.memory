package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// --- Fake storage ---

type fakeStorage struct {
	enabled     bool
	uploadKey   string
	uploadErr   error
	deleteErr   error
	deletedKeys []string
}

func (f *fakeStorage) Enabled() bool { return f.enabled }

func (f *fakeStorage) UploadDocument(_ context.Context, _ io.Reader, _ int64, _ storage.DocumentUploadOptions) (*storage.UploadResult, error) {
	if f.uploadErr != nil {
		return nil, f.uploadErr
	}
	return &storage.UploadResult{Key: f.uploadKey, StorageURL: "http://fake/" + f.uploadKey}, nil
}

func (f *fakeStorage) Delete(_ context.Context, key string) error {
	f.deletedKeys = append(f.deletedKeys, key)
	return f.deleteErr
}

// --- Fake parsing jobs ---

type fakeParsingJobs struct {
	called bool
	err    error
}

func (f *fakeParsingJobs) CreateJob(_ context.Context, _ ParsingJobOptions) error {
	f.called = true
	return f.err
}

// --- Fake upload service ---

type fakeUploadService struct {
	result *UploadDocumentResponse
	err    error
}

// We need to satisfy the *Service call in UploadHandler. Since svc is a *Service
// (concrete type), we can't easily mock it without a real DB. Instead, for
// handler tests that reach CreateFromUpload, we test them at a higher level
// (e2e) or by calling the helper functions directly. For handler-level HTTP
// tests we cover only the cases that short-circuit before the service call:
// storage disabled, bad request, file too large, MIME rejected, too many files.

// --- Helpers ---

func newTestUploadHandler(t *testing.T, stor uploadStorage, allowedMIME string) *UploadHandler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{}
	cfg.Storage.AllowedMIMETypes = allowedMIME
	// svc = nil because tests below don't reach CreateFromUpload
	h := &UploadHandler{
		svc:     nil,
		storage: stor,
		log:     log,
	}
	return h.WithAllowedMIMETypes(cfg.Storage.AllowedMIMETypes)
}

func echoCtxWithUser(t *testing.T, method, path string, body io.Reader, contentType string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "user-1", OrgID: "org-1", ProjectID: "proj-1"})
	return c, rec
}

func multipartBody(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	part, err := w.CreateFormFile(field, filename)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf, w.FormDataContentType()
}

func batchMultipartBody(t *testing.T, files map[string][]byte) (*bytes.Buffer, string) {
	t.Helper()
	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	for name, data := range files {
		part, err := w.CreateFormFile("files", name)
		require.NoError(t, err)
		_, err = part.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf, w.FormDataContentType()
}

// ============================================================================
// Unit tests — pure helper functions (no echo, no DB)
// ============================================================================

func TestDetectMIMEType_FallsBackToSniff(t *testing.T) {
	got := detectMIMEType(http.Header{}, []byte("%PDF-1.4"), "file.pdf")
	assert.Equal(t, "application/pdf", got)
}

func TestDetectMIMEType_PreservesClientType(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "text/plain")
	got := detectMIMEType(h, []byte("hello"), "note.txt")
	assert.Equal(t, "text/plain", got)
}

func TestDetectMIMEType_OctetStreamFallsToSniff(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "application/octet-stream")
	got := detectMIMEType(h, []byte("%PDF-1.4"), "file.bin")
	assert.Equal(t, "application/pdf", got)
}

func TestDetectMIMEType_RefinesZipToDocx(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "application/zip")
	got := detectMIMEType(h, []byte("PK\x03\x04"), "report.docx")
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", got)
}

func TestDetectMIMEType_RefinesZipToXlsx(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "application/zip")
	got := detectMIMEType(h, []byte("PK\x03\x04"), "data.xlsx")
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", got)
}

func TestDetectMIMEType_RefinesZipToPptx(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "application/zip")
	got := detectMIMEType(h, []byte("PK\x03\x04"), "slides.pptx")
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.presentationml.presentation", got)
}

func TestReadFileBytes_ReadsAll(t *testing.T) {
	want := []byte("hello world")
	got, n, err := readFileBytes(bytes.NewReader(want))
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, int64(len(want)), n)
}

func TestReadFileBytes_Empty(t *testing.T) {
	got, n, err := readFileBytes(bytes.NewReader(nil))
	require.NoError(t, err)
	assert.Empty(t, got) // nil and []byte{} are both empty
	assert.Equal(t, int64(0), n)
}

func TestComputeFileHash_KnownSHA256(t *testing.T) {
	// SHA-256("hello world") = b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9
	h := computeFileHash([]byte("hello world"))
	assert.Equal(t, "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9", h)
}

func TestComputeFileHash_DifferentInputs(t *testing.T) {
	a := computeFileHash([]byte("foo"))
	b := computeFileHash([]byte("bar"))
	assert.NotEqual(t, a, b)
}

func TestRefineMimeTypeByExtension_AllOfficeFormats(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"doc.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"sheet.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"slides.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		{"archive.zip", "application/zip"}, // unchanged — not an Office format
		{"REPORT.DOCX", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"}, // case-insensitive
	}
	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := refineMimeTypeByExtension("application/zip", tt.filename)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ============================================================================
// Unit tests — MIME allowlist (WithAllowedMIMETypes / validateMIMEType)
// ============================================================================

func TestWithAllowedMIMETypes_Empty_NoRestriction(t *testing.T) {
	h := &UploadHandler{}
	h.WithAllowedMIMETypes("")
	assert.Nil(t, h.allowedMIMETypes)
}

func TestWithAllowedMIMETypes_ParsesCSV(t *testing.T) {
	h := &UploadHandler{}
	h.WithAllowedMIMETypes("application/pdf, image/jpeg, text/plain")
	assert.NotNil(t, h.allowedMIMETypes)
	assert.Contains(t, h.allowedMIMETypes, "application/pdf")
	assert.Contains(t, h.allowedMIMETypes, "image/jpeg")
	assert.Contains(t, h.allowedMIMETypes, "text/plain")
	assert.NotContains(t, h.allowedMIMETypes, "application/x-executable")
}

func TestValidateMIMEType_Allowed(t *testing.T) {
	h := &UploadHandler{allowedMIMETypes: map[string]struct{}{"application/pdf": {}}}
	assert.NoError(t, h.validateMIMEType("application/pdf"))
}

func TestValidateMIMEType_Rejected(t *testing.T) {
	h := &UploadHandler{allowedMIMETypes: map[string]struct{}{"application/pdf": {}}}
	err := h.validateMIMEType("application/x-executable")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
}

func TestValidateMIMEType_NoAllowlist_AcceptsAnything(t *testing.T) {
	h := &UploadHandler{allowedMIMETypes: nil}
	assert.NoError(t, h.validateMIMEType("application/x-executable"))
}

// ============================================================================
// Unit tests — NewUploadHandler wires AllowedMIMETypes from config
// ============================================================================

func TestNewUploadHandler_AllowedMIMETypesFromConfig(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{}
	cfg.Storage.AllowedMIMETypes = "application/pdf,image/png"
	h := NewUploadHandler(nil, nil, nil, nil, cfg, log)
	require.NotNil(t, h.allowedMIMETypes)
	assert.Contains(t, h.allowedMIMETypes, "application/pdf")
	assert.Contains(t, h.allowedMIMETypes, "image/png")
}

func TestNewUploadHandler_EmptyAllowedMIMETypes_NoRestriction(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{} // AllowedMIMETypes = ""
	h := NewUploadHandler(nil, nil, nil, nil, cfg, log)
	assert.Nil(t, h.allowedMIMETypes)
}

// ============================================================================
// Unit tests — deleteStorageObject / createParsingJob
// ============================================================================

func TestDeleteStorageObject_ErrorIsLoggedNotPropagated(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	stor := &fakeStorage{enabled: true, deleteErr: fmt.Errorf("network error")}
	h := &UploadHandler{storage: stor, log: log}
	assert.NotPanics(t, func() {
		h.deleteStorageObject(context.Background(), "some-key")
	})
	assert.Equal(t, []string{"some-key"}, stor.deletedKeys)
}

func TestCreateParsingJob_NilService_NoPanic(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	h := &UploadHandler{parsingJobsService: nil, log: log}
	assert.NotPanics(t, func() {
		h.createParsingJob(context.Background(), "org", "proj", "doc", "file.pdf", "application/pdf", 100, "key", false)
	})
}

func TestCreateParsingJob_ErrorIsLoggedNotReturned(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	parsing := &fakeParsingJobs{err: fmt.Errorf("queue full")}
	h := &UploadHandler{parsingJobsService: parsing, log: log}
	assert.NotPanics(t, func() {
		h.createParsingJob(context.Background(), "org", "proj", "doc", "file.pdf", "application/pdf", 100, "key", false)
	})
	assert.True(t, parsing.called)
}

// ============================================================================
// HTTP handler tests — Upload (cases that short-circuit before storage/DB)
// ============================================================================

// assertHandlerStatus handles Echo's apperror pattern: handlers return *apperror.Error
// (not nil) which Echo's error handler converts to an HTTP response. In unit tests
// without a full Echo server, we inspect the error's HTTPStatus field directly.
func assertHandlerStatus(t *testing.T, handlerErr error, rec *httptest.ResponseRecorder, wantCode int) {
	t.Helper()
	if handlerErr != nil {
		type hasHTTPStatus interface{ GetHTTPStatus() int }
		// apperror.Error exposes HTTPStatus field directly
		if appErr, ok := handlerErr.(*apperror.Error); ok {
			assert.Equal(t, wantCode, appErr.HTTPStatus)
			return
		}
		// Echo HTTPError
		if echoErr, ok := handlerErr.(*echo.HTTPError); ok {
			assert.Equal(t, wantCode, echoErr.Code)
			return
		}
		t.Fatalf("unexpected error type %T: %v", handlerErr, handlerErr)
	}
	assert.Equal(t, wantCode, rec.Code)
}

func TestUpload_StorageDisabled_Returns503(t *testing.T) {
	stor := &fakeStorage{enabled: false}
	h := newTestUploadHandler(t, stor, "")

	body, ct := multipartBody(t, "file", "test.pdf", []byte("%PDF-1.4"))
	c, rec := echoCtxWithUser(t, http.MethodPost, "/upload", body, ct)

	err := h.Upload(c)
	assertHandlerStatus(t, err, rec, http.StatusServiceUnavailable)
}

func TestUpload_MissingFile_Returns400(t *testing.T) {
	stor := &fakeStorage{enabled: true}
	h := newTestUploadHandler(t, stor, "")

	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	_ = w.WriteField("autoExtract", "false")
	_ = w.Close()

	c, rec := echoCtxWithUser(t, http.MethodPost, "/upload", buf, w.FormDataContentType())
	err := h.Upload(c)
	assertHandlerStatus(t, err, rec, http.StatusBadRequest)
}

func TestUpload_MIMETypeRejected_Returns415(t *testing.T) {
	stor := &fakeStorage{enabled: true}
	h := newTestUploadHandler(t, stor, "application/pdf")

	body, ct := multipartBody(t, "file", "note.txt", []byte("hello world"))
	c, rec := echoCtxWithUser(t, http.MethodPost, "/upload", body, ct)

	err := h.Upload(c)
	assertHandlerStatus(t, err, rec, http.StatusUnsupportedMediaType)
}

func TestUpload_StorageUploadError_Returns500(t *testing.T) {
	stor := &fakeStorage{enabled: true, uploadErr: fmt.Errorf("s3 unavailable")}
	h := newTestUploadHandler(t, stor, "")

	body, ct := multipartBody(t, "file", "test.pdf", []byte("%PDF-1.4"))
	c, rec := echoCtxWithUser(t, http.MethodPost, "/upload", body, ct)

	err := h.Upload(c)
	assertHandlerStatus(t, err, rec, http.StatusInternalServerError)
}

// ============================================================================
// HTTP handler tests — UploadBatch (cases that short-circuit before storage/DB)
// ============================================================================

func TestUploadBatch_StorageDisabled_Returns503(t *testing.T) {
	stor := &fakeStorage{enabled: false}
	h := newTestUploadHandler(t, stor, "")

	body, ct := batchMultipartBody(t, map[string][]byte{"a.txt": []byte("hello")})
	req := httptest.NewRequest(http.MethodPost, "/upload/batch", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "u", OrgID: "o", ProjectID: "p"})

	err := h.UploadBatch(c)
	assertHandlerStatus(t, err, rec, http.StatusServiceUnavailable)
}

func TestUploadBatch_NoFiles_Returns400(t *testing.T) {
	stor := &fakeStorage{enabled: true}
	h := newTestUploadHandler(t, stor, "")

	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload/batch", buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "u", OrgID: "o", ProjectID: "p"})

	err := h.UploadBatch(c)
	assertHandlerStatus(t, err, rec, http.StatusBadRequest)
}

func TestUploadBatch_TooManyFiles_Returns400(t *testing.T) {
	stor := &fakeStorage{enabled: true}
	h := newTestUploadHandler(t, stor, "")

	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	for i := 0; i <= MaxBatchFiles; i++ {
		part, err := w.CreateFormFile("files", fmt.Sprintf("file%d.txt", i))
		require.NoError(t, err)
		_, err = part.Write([]byte("data"))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/upload/batch", buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "u", OrgID: "o", ProjectID: "p"})

	err := h.UploadBatch(c)
	assertHandlerStatus(t, err, rec, http.StatusBadRequest)
}

func TestUploadBatch_FileTooLarge_PerFileError(t *testing.T) {
	stor := &fakeStorage{enabled: true, uploadErr: fmt.Errorf("s3 error")}
	h := newTestUploadHandler(t, stor, "")

	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)

	// Small file
	part, err := w.CreateFormFile("files", "small.txt")
	require.NoError(t, err)
	_, err = part.Write(bytes.Repeat([]byte("x"), 100))
	require.NoError(t, err)

	// File just over batch limit
	part2, err := w.CreateFormFile("files", "large.txt")
	require.NoError(t, err)
	_, err = part2.Write(bytes.Repeat([]byte("x"), MaxBatchUploadSize+1))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/upload/batch", buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "u", OrgID: "o", ProjectID: "p"})

	err = h.UploadBatch(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code) // batch always 200

	var result BatchUploadResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	assert.Equal(t, 2, result.Summary.Total)

	// Verify large.txt reports size error
	var foundSizeError bool
	for _, r := range result.Results {
		if r.Filename == "large.txt" && r.Status == "failed" && r.Error != nil {
			if strings.Contains(*r.Error, "10 MB") {
				foundSizeError = true
			}
		}
	}
	assert.True(t, foundSizeError, "expected size error for large.txt, got: %+v", result.Results)
}

func TestUploadBatch_MIMETypeRejected_PerFileError(t *testing.T) {
	stor := &fakeStorage{enabled: true}
	h := newTestUploadHandler(t, stor, "application/pdf") // only PDF allowed

	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	part, err := w.CreateFormFile("files", "note.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("hello world"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/upload/batch", buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "u", OrgID: "o", ProjectID: "p"})

	err = h.UploadBatch(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var result BatchUploadResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	assert.Equal(t, 1, result.Summary.Total)
	assert.Equal(t, 1, result.Summary.Failed)
	require.Len(t, result.Results, 1)
	assert.Equal(t, "failed", result.Results[0].Status)
	require.NotNil(t, result.Results[0].Error)
	assert.Contains(t, *result.Results[0].Error, "not allowed")
}

// ============================================================================
// HTTP handler tests — UploadForRemember (helper-level, no HTTP)
// ============================================================================

func TestUploadForRemember_StorageDisabled_ReturnsError(t *testing.T) {
	stor := &fakeStorage{enabled: false}
	h := newTestUploadHandler(t, stor, "")

	// Build a fake multipart.FileHeader
	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	part, err := w.CreateFormFile("file", "test.pdf")
	require.NoError(t, err)
	_, err = part.Write([]byte("%PDF-1.4"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/remember/file", buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	mr, err := req.MultipartReader()
	require.NoError(t, err)
	form, err := mr.ReadForm(1 << 20)
	require.NoError(t, err)
	fh := form.File["file"][0]

	_, uploadErr := h.UploadForRemember(context.Background(), "org", "proj", fh, nil)
	require.Error(t, uploadErr)
	assert.Contains(t, uploadErr.Error(), "storage")
}

func TestUploadForRemember_MIMETypeRejected_ReturnsError(t *testing.T) {
	stor := &fakeStorage{enabled: true}
	h := newTestUploadHandler(t, stor, "application/pdf") // only PDF

	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	part, err := w.CreateFormFile("file", "note.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("plain text"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/remember/file", buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	mr2, err := req.MultipartReader()
	require.NoError(t, err)
	form, err := mr2.ReadForm(1 << 20)
	require.NoError(t, err)
	fh := form.File["file"][0]

	_, uploadErr := h.UploadForRemember(context.Background(), "org", "proj", fh, nil)
	require.Error(t, uploadErr)
	assert.Contains(t, uploadErr.Error(), "not allowed")
}
