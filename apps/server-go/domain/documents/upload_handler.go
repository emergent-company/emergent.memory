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
	"sync"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/internal/storage"
	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
	"github.com/emergent-company/emergent/pkg/logger"
)

const (
	// MaxUploadSize is the maximum file size for uploads (500MB)
	MaxUploadSize = 500 * 1024 * 1024
	// MaxBatchUploadSize is the maximum file size for batch uploads (10MB per file)
	MaxBatchUploadSize = 10 * 1024 * 1024
	// MaxBatchFiles is the maximum number of files in a batch upload
	MaxBatchFiles = 100
	// BatchConcurrency is the number of concurrent file uploads in a batch
	BatchConcurrency = 3
)

// UploadHandler handles document upload HTTP requests
type UploadHandler struct {
	svc                *Service
	storage            *storage.Service
	parsingJobsService ParsingJobCreator
	log                *slog.Logger
}

// ParsingJobCreator is an interface for creating document parsing jobs
type ParsingJobCreator interface {
	CreateJob(ctx context.Context, opts ParsingJobOptions) error
}

// ParsingJobOptions contains options for creating a document parsing job
type ParsingJobOptions struct {
	OrganizationID string
	ProjectID      string
	DocumentID     string
	SourceType     string
	SourceFilename *string
	MimeType       *string
	FileSizeBytes  *int64
	StorageKey     *string
}

// NewUploadHandler creates a new upload handler
func NewUploadHandler(svc *Service, storageSvc *storage.Service, parsingJobsService ParsingJobCreator, log *slog.Logger) *UploadHandler {
	return &UploadHandler{
		svc:                svc,
		storage:            storageSvc,
		parsingJobsService: parsingJobsService,
		log:                log.With(logger.Scope("upload")),
	}
}

// Upload handles POST /api/documents/upload (multipart file upload)
func (h *UploadHandler) Upload(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	// Check if storage is enabled
	if !h.storage.Enabled() {
		return apperror.New(503, "storage_unavailable", "Storage service is not configured")
	}

	// Get file from multipart form
	file, err := c.FormFile("file")
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("file is required")
	}

	// Validate file size
	if file.Size > MaxUploadSize {
		return apperror.ErrBadRequest.WithMessage("file size exceeds maximum of 500MB")
	}

	// Open the file
	src, err := file.Open()
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("failed to read file")
	}
	defer src.Close()

	// Read file into buffer (needed for hashing and upload)
	buf := new(bytes.Buffer)
	n, err := io.Copy(buf, src)
	if err != nil {
		return apperror.ErrInternal.WithInternal(err)
	}
	fileBytes := buf.Bytes()

	// Compute file hash for deduplication
	fileHash := computeFileHash(fileBytes)

	// Detect MIME type
	mimeType := file.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		// Try to detect from content
		mimeType = http.DetectContentType(fileBytes)
	}

	// Parse optional form fields
	autoExtract := c.FormValue("autoExtract") == "true"

	// Upload to storage
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

	// Create document record
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
		// Try to clean up storage if document creation fails
		_ = h.storage.Delete(c.Request().Context(), uploadResult.Key)
		return err
	}

	// Return 201 for new document, 200 for deduplicated existing document
	status := http.StatusCreated
	if response.IsDuplicate {
		status = http.StatusOK
		// Clean up the uploaded file since we're using the existing one
		_ = h.storage.Delete(c.Request().Context(), uploadResult.Key)
	} else if h.parsingJobsService != nil {
		if err := h.parsingJobsService.CreateJob(c.Request().Context(), ParsingJobOptions{
			OrganizationID: user.OrgID,
			ProjectID:      user.ProjectID,
			DocumentID:     response.Document.ID,
			SourceType:     "file_upload",
			SourceFilename: &file.Filename,
			MimeType:       &mimeType,
			FileSizeBytes:  &n,
			StorageKey:     &uploadResult.Key,
		}); err != nil {
			h.log.Error("failed to create parsing job", slog.String("document_id", response.Document.ID), logger.Error(err))
		}
	}

	return c.JSON(status, response)
}

// UploadBatch handles POST /api/documents/upload/batch (batch multipart file upload)
// Max 100 files per batch, each max 10MB. Files are processed concurrently.
func (h *UploadHandler) UploadBatch(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	// Check if storage is enabled
	if !h.storage.Enabled() {
		return apperror.New(503, "storage_unavailable", "Storage service is not configured")
	}

	// Parse multipart form
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

	// Parse optional form fields
	autoExtract := false
	if values := form.Value["autoExtract"]; len(values) > 0 && values[0] == "true" {
		autoExtract = true
	}

	ctx := c.Request().Context()

	// Process files concurrently with limited concurrency
	results := make([]BatchUploadFileResult, len(files))
	var wg sync.WaitGroup
	sem := make(chan struct{}, BatchConcurrency)

	for i, file := range files {
		wg.Add(1)
		go func(idx int, fh *multipart.FileHeader) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			results[idx] = h.processFileUpload(ctx, user, fh, autoExtract)
		}(i, file)
	}

	wg.Wait()

	// Calculate summary
	summary := BatchUploadSummary{Total: len(files)}
	for _, result := range results {
		switch result.Status {
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

// processFileUpload processes a single file in a batch upload
func (h *UploadHandler) processFileUpload(ctx context.Context, user *auth.AuthUser, fh *multipart.FileHeader, autoExtract bool) BatchUploadFileResult {
	filename := fh.Filename
	if filename == "" {
		filename = "upload"
	}

	// Validate file size for batch uploads (10MB limit)
	if fh.Size > MaxBatchUploadSize {
		errMsg := "file size exceeds maximum of 10MB for batch uploads"
		return BatchUploadFileResult{
			Filename: filename,
			Status:   "failed",
			Error:    &errMsg,
		}
	}

	// Open the file
	src, err := fh.Open()
	if err != nil {
		errMsg := "failed to read file"
		return BatchUploadFileResult{
			Filename: filename,
			Status:   "failed",
			Error:    &errMsg,
		}
	}
	defer src.Close()

	// Read file into buffer
	buf := new(bytes.Buffer)
	n, err := io.Copy(buf, src)
	if err != nil {
		errMsg := "failed to read file content"
		return BatchUploadFileResult{
			Filename: filename,
			Status:   "failed",
			Error:    &errMsg,
		}
	}
	fileBytes := buf.Bytes()

	// Compute file hash for deduplication
	fileHash := computeFileHash(fileBytes)

	// Detect MIME type
	mimeType := fh.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(fileBytes)
	}

	// Upload to storage
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
		errMsg := "failed to upload to storage"
		return BatchUploadFileResult{
			Filename: filename,
			Status:   "failed",
			Error:    &errMsg,
		}
	}

	// Create document record
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
		// Clean up storage on failure
		_ = h.storage.Delete(ctx, uploadResult.Key)
		errMsg := "failed to create document record"
		return BatchUploadFileResult{
			Filename: filename,
			Status:   "failed",
			Error:    &errMsg,
		}
	}

	// Handle deduplication
	if response.IsDuplicate {
		// Clean up the uploaded file since we're using the existing one
		_ = h.storage.Delete(ctx, uploadResult.Key)
		return BatchUploadFileResult{
			Filename:   filename,
			Status:     "duplicate",
			DocumentID: response.ExistingDocumentID,
		}
	}

	if h.parsingJobsService != nil {
		if err := h.parsingJobsService.CreateJob(ctx, ParsingJobOptions{
			OrganizationID: user.OrgID,
			ProjectID:      user.ProjectID,
			DocumentID:     response.Document.ID,
			SourceType:     "file_upload",
			SourceFilename: &filename,
			MimeType:       &mimeType,
			FileSizeBytes:  &n,
			StorageKey:     &uploadResult.Key,
		}); err != nil {
			h.log.Error("failed to create parsing job", slog.String("document_id", response.Document.ID), logger.Error(err))
		}
	}

	docID := response.Document.ID
	return BatchUploadFileResult{
		Filename:   filename,
		Status:     "success",
		DocumentID: &docID,
	}
}

// computeFileHash computes SHA-256 hash of file bytes
func computeFileHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
