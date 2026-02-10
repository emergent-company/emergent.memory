# Phase 1: Document Management Implementation Guide

## Overview

This document provides complete implementation guidance for Phase 1 of the document management feature.

## Architecture Summary

```
CLI (File Upload) ──REST API──▶ Go Server ──▶ MinIO/S3
                                     │
                                     ├──▶ Document DB Record
                                     └──▶ Optional: Extraction Job

MCP (Management) ──JSON-RPC──▶ Go Server ──▶ List/Get/Delete Docs
                                             Trigger Extraction
```

---

## Implementation Tasks

### 1. Backend Upload Endpoint ✅ READY TO IMPLEMENT

**File**: `/root/emergent/apps/server-go/domain/documents/handler.go`

**Add after line 389:**

```go
// Upload handles POST /api/v2/documents/upload
func (h *Handler) Upload(c echo.Context) error {
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
	file, err := c.FormFile("file")
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("file required in multipart form")
	}

	// Validate file size (100MB limit)
	maxSize := int64(100 * 1024 * 1024)
	if file.Size > maxSize {
		return apperror.New(413, "file_too_large", "File size exceeds 100MB limit")
	}

	// Open uploaded file
	src, err := file.Open()
	if err != nil {
		return apperror.ErrInternal.WithMessage("failed to read uploaded file")
	}
	defer src.Close()

	// Upload to storage
	uploadResult, err := h.storage.UploadDocument(
		c.Request().Context(),
		src,
		file.Size,
		storage.DocumentUploadOptions{
			ProjectID: user.ProjectID,
			OrgID:     user.OrganizationID,
			Filename:  file.Filename,
			UploadOptions: storage.UploadOptions{
				ContentType: file.Header.Get("Content-Type"),
			},
		},
	)
	if err != nil {
		return apperror.ErrInternal.WithInternal(err).WithMessage("failed to upload file")
	}

	// Create document record
	sourceType := "upload"
	if st := c.FormValue("source_type"); st != "" {
		sourceType = st
	}

	doc, wasCreated, err := h.svc.Create(c.Request().Context(), CreateParams{
		ProjectID:     user.ProjectID,
		Filename:      file.Filename,
		StorageKey:    uploadResult.Key,
		SourceType:    sourceType,
		MimeType:      uploadResult.ContentType,
		FileSizeBytes: uploadResult.Size,
	})
	if err != nil {
		return apperror.ErrInternal.WithInternal(err).WithMessage("failed to create document record")
	}

	response := map[string]any{
		"document":    doc,
		"was_created": wasCreated,
	}

	// Optionally trigger extraction
	if c.FormValue("extract") == "true" {
		job, err := h.extractionService.CreateJob(c.Request().Context(), extraction.CreateJobOptions{
			ProjectID:      doc.ProjectID,
			SourceType:     "file_upload",
			SourceFilename: &doc.Filename,
			MimeType:       doc.MimeType,
			FileSizeBytes:  doc.FileSizeBytes,
			StorageKey:     doc.StorageKey,
			DocumentID:     &doc.ID,
		})
		if err != nil {
			// Log error but don't fail the upload
			h.log.Error("failed to create extraction job", "error", err)
		} else {
			response["extraction_job_id"] = job.ID
			response["message"] = "Document uploaded and extraction job created"
		}
	} else {
		response["message"] = "Document uploaded successfully"
	}

	// Return 201 for new document, 200 for deduplicated
	status := http.StatusCreated
	if !wasCreated {
		status = http.StatusOK
		response["message"] = "Document already exists (deduplicated by content hash)"
	}

	return c.JSON(status, response)
}
```

**Dependencies needed in Handler:**

```go
type Handler struct {
	svc                *Service
	storage            *storage.Service
	extractionService  *extraction.DocumentParsingJobsService // ADD THIS
	log                *slog.Logger                           // ADD THIS
}

func NewHandler(
	svc *Service,
	storageSvc *storage.Service,
	extractionSvc *extraction.DocumentParsingJobsService, // ADD THIS
	log *slog.Logger,                                      // ADD THIS
) *Handler {
	return &Handler{
		svc:               svc,
		storage:           storageSvc,
		extractionService: extractionSvc, // ADD THIS
		log:               log,           // ADD THIS
	}
}
```

---

### 2. Add Upload Route

**File**: `/root/emergent/apps/server-go/domain/documents/routes.go`

Check if file exists, otherwise create it. Add:

```go
package documents

import "github.com/labstack/echo/v4"

// RegisterRoutes registers document routes
func RegisterRoutes(e *echo.Group, h *Handler) {
	docs := e.Group("/documents")

	docs.GET("", h.List)                     // Existing
	docs.GET("/:id", h.GetByID)             // Existing
	docs.POST("", h.Create)                  // Existing
	docs.POST("/upload", h.Upload)           // NEW
	docs.DELETE("/:id", h.Delete)            // Existing
	docs.GET("/:id/download", h.Download)    // Existing
}
```

---

### 3. Update Service.Create to Accept Storage Fields

**File**: `/root/emergent/apps/server-go/domain/documents/service.go`

Update `CreateParams` struct (around line 54):

```go
type CreateParams struct {
	ProjectID     string
	Filename      string
	Content       string         // Optional (for text content)
	StorageKey    string         // NEW - for file uploads
	SourceType    string         // NEW
	MimeType      string         // NEW
	FileSizeBytes int64          // NEW
}
```

Update `Create` method (around line 62) to handle storage fields:

```go
func (s *Service) Create(ctx context.Context, params CreateParams) (*Document, bool, error) {
	// Apply defaults
	filename := strings.TrimSpace(params.Filename)
	if filename == "" {
		filename = "unnamed.txt"
	}

	content := params.Content
	sourceType := params.SourceType
	if sourceType == "" {
		sourceType = "text"
	}

	// Calculate content hash for deduplication
	contentHash := computeContentHash(content)

	// Check for existing document with same content hash (deduplication)
	existingDoc, err := s.repo.GetByContentHash(ctx, params.ProjectID, contentHash)
	if err != nil {
		return nil, false, err
	}
	if existingDoc != nil {
		s.log.Info("document deduplicated",
			slog.String("projectId", params.ProjectID),
			slog.String("existingId", existingDoc.ID),
			slog.String("contentHash", contentHash))
		return existingDoc, false, nil // Return existing doc, wasCreated=false
	}

	// Create new document
	now := time.Now().UTC()
	doc := &Document{
		ID:            uuid.New().String(),
		ProjectID:     params.ProjectID,
		Filename:      &filename,
		Content:       &content,
		ContentHash:   &contentHash,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// NEW: Set storage fields if provided
	if params.StorageKey != "" {
		doc.StorageKey = &params.StorageKey
	}
	if params.SourceType != "" {
		doc.SourceType = &params.SourceType
	}
	if params.MimeType != "" {
		doc.MimeType = &params.MimeType
	}
	if params.FileSizeBytes > 0 {
		doc.FileSizeBytes = &params.FileSizeBytes
	}

	err = s.repo.Create(ctx, doc)
	if err != nil {
		return nil, false, err
	}

	return doc, true, nil
}
```

---

### 4. Update Module Wiring

**File**: `/root/emergent/apps/server-go/domain/documents/module.go`

Update to inject extraction service:

```go
var Module = fx.Module("documents",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(
		fx.Annotate(
			NewHandler,
			fx.ParamTags(``, ``, ``, ``), // storage, extraction, log
		),
	),
	fx.Invoke(RegisterRoutes),
)
```

---

### 5. MCP Tools Implementation

**Status**: Deferred to focused session

**Reason**: Requires significant changes to MCP service struct and all test files

**Estimated Effort**: 2-3 hours

**Files to modify**:

- `/root/emergent/apps/server-go/domain/mcp/service.go` (add 6 tools)
- `/root/emergent/apps/server-go/domain/mcp/module.go` (dependency injection)
- `/root/emergent/apps/server-go/internal/testutil/server.go` (test setup)
- Any other files instantiating MCP service

**Tool Specifications**: See `/root/emergent/apps/server-go/domain/documents/UPLOAD_API.md` - "MCP Integration" section (to be added)

---

### 6. CLI Command Design

**Repository**: Likely `tools/emergent-cli/` or separate repo

**Command Structure**:

```bash
emergent upload <file> --project <uuid> [--extract]
```

**Implementation Sketch** (Go):

```go
package cmd

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var uploadCmd = &cobra.Command{
	Use:   "upload <file>",
	Short: "Upload a document to a project",
	Args:  cobra.ExactArgs(1),
	RunE:  runUpload,
}

var (
	uploadProjectID string
	uploadExtract   bool
)

func init() {
	uploadCmd.Flags().StringVar(&uploadProjectID, "project", "", "Target project ID (required)")
	uploadCmd.Flags().BoolVar(&uploadExtract, "extract", false, "Trigger extraction after upload")
	uploadCmd.MarkFlagRequired("project")

	rootCmd.AddCommand(uploadCmd)
}

func runUpload(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Get file info
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}

	_, err = io.Copy(part, file)
	if err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	// Add extract flag
	if uploadExtract {
		writer.WriteField("extract", "true")
	}

	writer.Close()

	// Create request
	apiURL := getAPIURL() + "/api/v2/documents/upload"
	req, err := http.NewRequest("POST", apiURL, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-API-Key", getAPIKey())
	req.Header.Set("X-Project-ID", uploadProjectID)

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("upload failed with status %d", resp.StatusCode)
	}

	// Parse response
	var result struct {
		Document struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
		} `json:"document"`
		ExtractionJobID string `json:"extraction_job_id"`
		Message         string `json:"message"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Print success
	fmt.Printf("✓ Uploaded %s (%.2f MB) → %s\n",
		filepath.Base(filePath),
		float64(stat.Size())/1024/1024,
		result.Document.ID,
	)

	if result.ExtractionJobID != "" {
		fmt.Printf("✓ Triggered extraction job → %s\n", result.ExtractionJobID)
		fmt.Printf("✓ View status: emergent status %s\n", result.ExtractionJobID)
	}

	return nil
}
```

---

## Testing

### Manual Testing

```bash
# 1. Upload without extraction
curl -X POST http://localhost:3002/api/v2/documents/upload \
  -H "X-API-Key: test-key" \
  -H "X-Project-ID: test-project-uuid" \
  -F "file=@test.pdf"

# 2. Upload with extraction
curl -X POST http://localhost:3002/api/v2/documents/upload \
  -H "X-API-Key: test-key" \
  -H "X-Project-ID: test-project-uuid" \
  -F "file=@test.pdf" \
  -F "extract=true"

# 3. Test deduplication (upload same file twice)
curl -X POST http://localhost:3002/api/v2/documents/upload \
  -H "X-API-Key: test-key" \
  -H "X-Project-ID: test-project-uuid" \
  -F "file=@test.pdf"
# Should return 200 OK with was_created=false

# 4. Test file too large (>100MB)
dd if=/dev/zero of=large.bin bs=1M count=101
curl -X POST http://localhost:3002/api/v2/documents/upload \
  -H "X-API-Key: test-key" \
  -H "X-Project-ID: test-project-uuid" \
  -F "file=@large.bin"
# Should return 413 Payload Too Large
```

### Unit Tests

Create `/root/emergent/apps/server-go/domain/documents/handler_test.go`:

```go
package documents_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"

	"github.com/emergent/emergent-core/domain/documents"
)

func TestUploadHandler(t *testing.T) {
	// TODO: Implement upload handler tests
	// - Test successful upload
	// - Test upload with extraction
	// - Test file too large
	// - Test missing file
	// - Test deduplication
}
```

---

## Next Steps

**Priority Order**:

1. ✅ Implement backend upload endpoint (handler.go)
2. ✅ Update Service.Create to handle storage fields
3. ✅ Add upload route
4. ✅ Update module wiring
5. 🔄 Test manually with cURL
6. ⏳ Implement MCP tools (separate session)
7. ⏳ Implement CLI command (separate session/repo)

**Estimated Time**:

- Backend implementation: 1 hour
- Testing: 30 minutes
- MCP tools: 2-3 hours (separate session)
- CLI: 1-2 hours (separate session)

---

## Related Documentation

- **API Spec**: `/root/emergent/apps/server-go/domain/documents/UPLOAD_API.md`
- **Storage Service**: `/root/emergent/apps/server-go/internal/storage/storage.go`
- **Extraction Jobs**: `/root/emergent/apps/server-go/domain/extraction/document_parsing_jobs.go`
- **Document Entity**: `/root/emergent/apps/server-go/domain/documents/entity.go`

---

## Status

- [x] API specification documented
- [ ] Backend endpoint implemented
- [ ] Routes registered
- [ ] Service updated
- [ ] Module wired
- [ ] Manual testing
- [ ] MCP tools
- [ ] CLI command
