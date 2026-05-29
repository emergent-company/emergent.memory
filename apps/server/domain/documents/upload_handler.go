package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// MaxUploadSize is the maximum file size for single uploads (500 MB).
	MaxUploadSize = 500 * 1024 * 1024
	// MaxBatchUploadSize is the maximum file size per file in batch uploads (10 MB).
	MaxBatchUploadSize = 10 * 1024 * 1024
	// MaxBatchFiles is the maximum number of files in a batch upload.
	MaxBatchFiles = 100
	// BatchConcurrency is the number of concurrent file uploads in a batch.
	BatchConcurrency = 3
)

// uploadStorage is the subset of storage.Service used by UploadHandler.
// Using an interface allows test doubles without importing the storage package.
type uploadStorage interface {
	Enabled() bool
	UploadDocument(ctx context.Context, r io.Reader, size int64, opts storage.DocumentUploadOptions) (*storage.UploadResult, error)
	Delete(ctx context.Context, key string) error
}

// UploadHandler handles document upload HTTP requests.
type UploadHandler struct {
	svc                   *Service
	storage               uploadStorage
	parsingJobsService    ParsingJobCreator
	extractionJobsService ExtractionJobCreator
	allowedMIMETypes      map[string]struct{} // nil = unrestricted
	log                   *slog.Logger
}

// ParsingJobCreator is an interface for creating document parsing jobs.
type ParsingJobCreator interface {
	CreateJob(ctx context.Context, opts ParsingJobOptions) error
}

// ExtractionJobCreator is a narrow interface for triggering object extraction on a document.
// Defined here (in the documents package) to avoid a circular import with the extraction package.
type ExtractionJobCreator interface {
	TriggerForDocument(ctx context.Context, projectID, documentID string) error
}

// ParsingJobOptions contains options for creating a document parsing job.
type ParsingJobOptions struct {
	OrganizationID string
	ProjectID      string
	DocumentID     string
	SourceType     string
	SourceFilename *string
	MimeType       *string
	FileSizeBytes  *int64
	StorageKey     *string
	// AutoExtract triggers object extraction after parsing completes successfully.
	// The extraction job is created by the parsing worker (not the upload handler)
	// to avoid a race condition where extraction starts before content is written.
	AutoExtract bool
}

// NewUploadHandler creates a new upload handler.
// cfg.Storage.AllowedMIMETypes (ALLOWED_MIME_TYPES env) controls which MIME types are accepted.
// An empty value disables type restrictions.
func NewUploadHandler(
	svc *Service,
	storageSvc uploadStorage,
	parsingJobsService ParsingJobCreator,
	extractionJobsService ExtractionJobCreator,
	cfg *config.Config,
	log *slog.Logger,
) *UploadHandler {
	h := &UploadHandler{
		svc:                   svc,
		storage:               storageSvc,
		parsingJobsService:    parsingJobsService,
		extractionJobsService: extractionJobsService,
		log:                   log.With(logger.Scope("upload")),
	}
	return h.WithAllowedMIMETypes(cfg.Storage.AllowedMIMETypes)
}

// WithAllowedMIMETypes configures a MIME type allowlist. Pass a comma-separated
// string from config (ALLOWED_MIME_TYPES). Empty string = no restriction.
func (h *UploadHandler) WithAllowedMIMETypes(csv string) *UploadHandler {
	if csv == "" {
		h.allowedMIMETypes = nil
		return h
	}
	set := make(map[string]struct{})
	for _, t := range strings.Split(csv, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			set[t] = struct{}{}
		}
	}
	h.allowedMIMETypes = set
	return h
}

// Upload handles POST /api/documents/upload (multipart file upload).
func (h *UploadHandler) Upload(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}
	if !h.storage.Enabled() {
		return apperror.New(503, "storage_unavailable", "Storage service is not configured")
	}

	// Enforce server-side byte limit before Echo reads multipart form.
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, MaxUploadSize+1024)

	file, err := c.FormFile("file")
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("file is required")
	}

	// file.Size comes from the multipart header (client-controlled). The real
	// size limit is enforced by MaxBytesReader above; this check is a fast-path.
	if file.Size > MaxUploadSize {
		return apperror.New(http.StatusRequestEntityTooLarge, "file_too_large", "file size exceeds maximum of 500 MB")
	}

	src, err := file.Open()
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("failed to read file")
	}
	defer src.Close()

	fileBytes, n, err := readFileBytes(src)
	if err != nil {
		return apperror.ErrInternal.WithInternal(err)
	}

	mimeType := detectMIMEType(file.Header, fileBytes, file.Filename)

	if err := h.validateMIMEType(mimeType); err != nil {
		return err
	}

	fileHash := computeFileHash(fileBytes)
	autoExtract := c.FormValue("autoExtract") == "true"

	uploadResult, err := h.storage.UploadDocument(
		c.Request().Context(),
		bytes.NewReader(fileBytes),
		n,
		storage.DocumentUploadOptions{
			OrgID:     user.OrgID,
			ProjectID: user.ProjectID,
			Filename:  file.Filename,
			UploadOptions: storage.UploadOptions{
				ContentType: mimeType,
			},
		},
	)
	if err != nil {
		return apperror.ErrInternal.WithInternal(err)
	}

	response, err := h.svc.CreateFromUpload(c.Request().Context(), UploadParams{
		ProjectID:   user.ProjectID,
		OrgID:       user.OrgID,
		Filename:    file.Filename,
		MimeType:    mimeType,
		FileSize:    n,
		FileHash:    fileHash,
		StorageKey:  uploadResult.Key,
		StorageURL:  uploadResult.StorageURL,
		AutoExtract: autoExtract,
	})
	if err != nil {
		h.deleteStorageObject(c.Request().Context(), uploadResult.Key)
		return err
	}

	status := http.StatusCreated
	if response.IsDuplicate {
		status = http.StatusOK
		h.deleteStorageObject(c.Request().Context(), uploadResult.Key)
	} else {
		h.createParsingJob(c.Request().Context(), user.OrgID, user.ProjectID, response.Document.ID, file.Filename, mimeType, n, uploadResult.Key, autoExtract)
	}

	return c.JSON(status, response)
}

// UploadBatch handles POST /api/documents/upload/batch (batch multipart file upload).
// Max 100 files per batch, each max 10 MB. Files are processed concurrently via a worker pool.
func (h *UploadHandler) UploadBatch(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}
	if !h.storage.Enabled() {
		return apperror.New(503, "storage_unavailable", "Storage service is not configured")
	}

	// Enforce a hard body limit: MaxBatchFiles * MaxBatchUploadSize + overhead.
	const batchBodyLimit = (MaxBatchFiles * MaxBatchUploadSize) + (1 * 1024 * 1024)
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, batchBodyLimit)

	form, err := c.MultipartForm()
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid multipart form")
	}

	files := form.File["files"]
	if len(files) == 0 {
		return apperror.ErrBadRequest.WithMessage("at least one file is required")
	}
	if len(files) > MaxBatchFiles {
		return apperror.ErrBadRequest.WithMessage("maximum 100 files allowed per batch")
	}

	autoExtract := false
	if values := form.Value["autoExtract"]; len(values) > 0 && values[0] == "true" {
		autoExtract = true
	}

	ctx := c.Request().Context()
	results := make([]BatchUploadFileResult, len(files))

	// Worker pool: exactly BatchConcurrency goroutines, driven by a job channel.
	type job struct {
		idx int
		fh  *multipart.FileHeader
	}
	jobCh := make(chan job, len(files))
	for i, f := range files {
		jobCh <- job{idx: i, fh: f}
	}
	close(jobCh)

	done := make(chan struct{})
	for w := 0; w < BatchConcurrency; w++ {
		go func() {
			for j := range jobCh {
				results[j.idx] = h.processFileUpload(ctx, user, j.fh, autoExtract)
			}
			done <- struct{}{}
		}()
	}
	for w := 0; w < BatchConcurrency; w++ {
		<-done
	}

	summary := BatchUploadSummary{Total: len(files)}
	for _, r := range results {
		switch r.Status {
		case "success":
			summary.Successful++
		case "duplicate":
			summary.Duplicates++
		case "failed":
			summary.Failed++
		}
	}

	return c.JSON(http.StatusOK, BatchUploadResult{
		Summary: summary,
		Results: results,
	})
}

// processFileUpload processes a single file in a batch upload.
func (h *UploadHandler) processFileUpload(ctx context.Context, user *auth.AuthUser, fh *multipart.FileHeader, autoExtract bool) BatchUploadFileResult {
	filename := fh.Filename
	if filename == "" {
		filename = "upload"
	}

	if fh.Size > MaxBatchUploadSize {
		errMsg := "file size exceeds maximum of 10 MB for batch uploads"
		return BatchUploadFileResult{Filename: filename, Status: "failed", Error: &errMsg}
	}

	src, err := fh.Open()
	if err != nil {
		errMsg := "failed to read file"
		return BatchUploadFileResult{Filename: filename, Status: "failed", Error: &errMsg}
	}
	defer src.Close()

	fileBytes, n, err := readFileBytes(src)
	if err != nil {
		errMsg := "failed to read file content"
		return BatchUploadFileResult{Filename: filename, Status: "failed", Error: &errMsg}
	}

	mimeType := detectMIMEType(fh.Header, fileBytes, fh.Filename)

	if err := h.validateMIMEType(mimeType); err != nil {
		errMsg := "file type not allowed: " + mimeType
		return BatchUploadFileResult{Filename: filename, Status: "failed", Error: &errMsg}
	}

	fileHash := computeFileHash(fileBytes)

	uploadResult, err := h.storage.UploadDocument(
		ctx,
		bytes.NewReader(fileBytes),
		n,
		storage.DocumentUploadOptions{
			OrgID:     user.OrgID,
			ProjectID: user.ProjectID,
			Filename:  filename,
			UploadOptions: storage.UploadOptions{
				ContentType: mimeType,
			},
		},
	)
	if err != nil {
		errMsg := "failed to upload to storage: " + err.Error()
		return BatchUploadFileResult{Filename: filename, Status: "failed", Error: &errMsg}
	}

	response, err := h.svc.CreateFromUpload(ctx, UploadParams{
		ProjectID:   user.ProjectID,
		OrgID:       user.OrgID,
		Filename:    filename,
		MimeType:    mimeType,
		FileSize:    n,
		FileHash:    fileHash,
		StorageKey:  uploadResult.Key,
		StorageURL:  uploadResult.StorageURL,
		AutoExtract: autoExtract,
	})
	if err != nil {
		h.deleteStorageObject(ctx, uploadResult.Key)
		errMsg := "failed to create document record"
		return BatchUploadFileResult{Filename: filename, Status: "failed", Error: &errMsg}
	}

	if response.IsDuplicate {
		h.deleteStorageObject(ctx, uploadResult.Key)
		return BatchUploadFileResult{
			Filename:   filename,
			Status:     "duplicate",
			DocumentID: response.ExistingDocumentID,
		}
	}

	h.createParsingJob(ctx, user.OrgID, user.ProjectID, response.Document.ID, filename, mimeType, n, uploadResult.Key, autoExtract)

	docID := response.Document.ID
	return BatchUploadFileResult{Filename: filename, Status: "success", DocumentID: &docID}
}

// RememberUploadResult is returned by UploadForRemember.
type RememberUploadResult struct {
	DocumentID  string
	IsDuplicate bool
}

// UploadForRemember uploads a file, creates a document record, and queues a parsing job
// without auto-extraction. The caller is responsible for waiting for
// ConversionStatus=="completed" before reading content.
func (h *UploadHandler) UploadForRemember(ctx context.Context, orgID, projectID string, fh *multipart.FileHeader, metadata map[string]any) (*RememberUploadResult, error) {
	filename := fh.Filename
	if filename == "" {
		filename = "upload"
	}

	if fh.Size > MaxUploadSize {
		return nil, apperror.ErrBadRequest.WithMessage("file size exceeds maximum of 500 MB")
	}

	// Check storage before reading file bytes to avoid wasted memory.
	if !h.storage.Enabled() {
		return nil, apperror.New(http.StatusServiceUnavailable, "storage_unavailable", "Storage service is not configured")
	}

	src, err := fh.Open()
	if err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("failed to read file")
	}
	defer src.Close()

	fileBytes, n, err := readFileBytes(src)
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	mimeType := detectMIMEType(fh.Header, fileBytes, fh.Filename)

	if err := h.validateMIMEType(mimeType); err != nil {
		return nil, err
	}

	fileHash := computeFileHash(fileBytes)

	uploadResult, err := h.storage.UploadDocument(
		ctx,
		bytes.NewReader(fileBytes),
		n,
		storage.DocumentUploadOptions{
			OrgID:     orgID,
			ProjectID: projectID,
			Filename:  filename,
			UploadOptions: storage.UploadOptions{
				ContentType: mimeType,
			},
		},
	)
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	response, err := h.svc.CreateFromUpload(ctx, UploadParams{
		ProjectID:   projectID,
		OrgID:       orgID,
		Filename:    filename,
		MimeType:    mimeType,
		FileSize:    n,
		FileHash:    fileHash,
		StorageKey:  uploadResult.Key,
		StorageURL:  uploadResult.StorageURL,
		AutoExtract: false,
		Metadata:    metadata,
	})
	if err != nil {
		h.deleteStorageObject(ctx, uploadResult.Key)
		return nil, err
	}

	if response.IsDuplicate {
		h.deleteStorageObject(ctx, uploadResult.Key)
		var docID string
		if response.ExistingDocumentID != nil {
			docID = *response.ExistingDocumentID
		} else if response.Document != nil {
			docID = response.Document.ID
		}
		return &RememberUploadResult{DocumentID: docID, IsDuplicate: true}, nil
	}

	h.createParsingJob(ctx, orgID, projectID, response.Document.ID, filename, mimeType, n, uploadResult.Key, false)

	return &RememberUploadResult{DocumentID: response.Document.ID, IsDuplicate: false}, nil
}

// --- Shared helpers ---

// readFileBytes reads all bytes from r into memory and returns (bytes, n, err).
func readFileBytes(r io.Reader) ([]byte, int64, error) {
	buf := new(bytes.Buffer)
	n, err := io.Copy(buf, r)
	if err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), n, nil
}

// detectMIMEType resolves the MIME type for an upload. It prefers the client-supplied
// Content-Type header but falls back to content sniffing. Office Open XML formats
// (.docx/.xlsx/.pptx) are ZIP archives internally; extension is used as a tiebreaker.
// header accepts textproto.MIMEHeader (multipart.FileHeader.Header) or http.Header — both
// expose the same Get method via the interface below.
func detectMIMEType(header interface{ Get(string) string }, data []byte, filename string) string {
	mimeType := header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	if mimeType == "application/zip" {
		mimeType = refineMimeTypeByExtension(mimeType, filename)
	}
	return mimeType
}

// Ensure textproto.MIMEHeader satisfies the detectMIMEType interface at compile time.
var _ interface{ Get(string) string } = textproto.MIMEHeader{}

// validateMIMEType returns a 415 error if the MIME type is not in the allowlist.
// When allowedMIMETypes is nil (no allowlist configured) all types are permitted.
func (h *UploadHandler) validateMIMEType(mimeType string) error {
	if h.allowedMIMETypes == nil {
		return nil
	}
	if _, ok := h.allowedMIMETypes[mimeType]; !ok {
		return apperror.New(http.StatusUnsupportedMediaType, "unsupported_media_type",
			"file type not allowed: "+mimeType)
	}
	return nil
}

// createParsingJob queues a document parsing job and logs (but does not fail on) errors.
// A failed enqueue means the document stays unparsed — callers already returned success.
func (h *UploadHandler) createParsingJob(ctx context.Context, orgID, projectID, documentID, filename, mimeType string, sizeBytes int64, storageKey string, autoExtract bool) {
	if h.parsingJobsService == nil {
		return
	}
	if err := h.parsingJobsService.CreateJob(ctx, ParsingJobOptions{
		OrganizationID: orgID,
		ProjectID:      projectID,
		DocumentID:     documentID,
		SourceType:     "file_upload",
		SourceFilename: &filename,
		MimeType:       &mimeType,
		FileSizeBytes:  &sizeBytes,
		StorageKey:     &storageKey,
		AutoExtract:    autoExtract,
	}); err != nil {
		h.log.Error("failed to create parsing job",
			slog.String("document_id", documentID),
			logger.Error(err),
		)
	}
}

// deleteStorageObject deletes a storage object and logs any error.
// Errors are non-fatal: the upload already succeeded; orphaned objects are a
// storage-hygiene concern, not a user-visible failure.
func (h *UploadHandler) deleteStorageObject(ctx context.Context, key string) {
	if err := h.storage.Delete(ctx, key); err != nil {
		h.log.Error("failed to delete storage object",
			slog.String("key", key),
			logger.Error(err),
		)
	}
}

// computeFileHash computes SHA-256 hash of file bytes.
func computeFileHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// refineMimeTypeByExtension corrects MIME types that Go's http.DetectContentType
// gets wrong because it only inspects magic bytes. Office Open XML formats
// (.docx, .xlsx, .pptx) are ZIP archives internally and are detected as
// "application/zip". Use the file extension as a tiebreaker.
func refineMimeTypeByExtension(mimeType, filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	}
	return mimeType
}
