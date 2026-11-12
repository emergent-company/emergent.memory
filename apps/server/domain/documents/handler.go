package documents

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// RetryableJob is a minimal extraction job view for retry purposes
type RetryableJob struct {
	ID        string
	ProjectID string
	Status    string
}

// ExtractionRetrier abstracts extraction job retry operations to avoid import cycles
type ExtractionRetrier interface {
	FindRetryableByDocument(ctx context.Context, documentID, projectID string) (*RetryableJob, error)
	RetryByJobID(ctx context.Context, jobID, projectID string) (*RetryableJob, error)
}

// ChunkingService is a narrow interface for rechunking a document.
// Defined here to avoid import cycles with the chunking package.
type ChunkingService interface {
	RecreateChunks(ctx context.Context, projectID, documentID string) error
}

// RetryConflictError reports that a retry was refused because the addressed run
// is still in flight. The extraction domain owns the sentinel
// (extraction.ErrJobStillRunning) and its threshold-aware message; this type
// carries that message across the narrow ExtractionRetrier seam without
// documents importing extraction, which would be an import cycle.
type RetryConflictError struct {
	Message string
}

func (e *RetryConflictError) Error() string { return e.Message }

// NewRetryConflict builds the error the documents handler renders as 409.
func NewRetryConflict(message string) error {
	return &RetryConflictError{Message: message}
}

// retryFailure renders a RetryByJobID failure as the response the client gets.
//
// A refusal the caller can act on keeps its own status and message: the
// in-flight conflict from the adapter, or an app error the extraction domain
// raised for a job that is gone or in a state the retry cannot resume. Anything
// else is a server-side failure — a driver error from the retry UPDATE, a
// decode, a context the adapter could not resolve — and its text goes to the
// log, not the body: err.Error() here used to become the 400 message, so a
// database error was reported to the caller as a bad request with the driver
// text in it.
func (h *Handler) retryFailure(err error) error {
	var conflict *RetryConflictError
	if errors.As(err, &conflict) {
		// A run that is still in flight is a conflict rather than a bad request:
		// retrying it would process the document twice.
		return apperror.NewConflict(conflict.Message)
	}
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	h.log.Error("retry extraction: retry failed", logger.Error(err))
	return apperror.ErrInternal.WithMessage("failed to retry extraction")
}

// Handler handles document HTTP requests
type Handler struct {
	svc                *Service
	storage            *storage.Service
	extraction         ExtractionRetrier
	chunking           ChunkingService
	extractionJobs     ExtractionJobCreator
	projectAutoExtract ProjectAutoExtractReader
	log                *slog.Logger
}

// NewHandler creates a new documents handler
func NewHandler(
	svc *Service,
	storageSvc *storage.Service,
	extraction ExtractionRetrier,
	chunkingSvc ChunkingService,
	extractionJobsSvc ExtractionJobCreator,
	projectAutoExtract ProjectAutoExtractReader,
	log *slog.Logger,
) *Handler {
	return &Handler{
		svc:                svc,
		storage:            storageSvc,
		extraction:         extraction,
		chunking:           chunkingSvc,
		extractionJobs:     extractionJobsSvc,
		projectAutoExtract: projectAutoExtract,
		log:                log.With(logger.Scope("documents.handler")),
	}
}

// List handles GET /api/documents
// @Summary      List documents
// @Description  List all documents for the project with optional filtering, search, sorting and pagination. total honours every filter including status; statusCounts ignores status but honours search and sourceType.
// @Tags         documents
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        limit query int false "Maximum number of results (1-500)" minimum(1) maximum(500)
// @Param        cursor query string false "Pagination cursor (opaque, from previous response; must match the requested sort)"
// @Param        sourceType query string false "Filter by source type"
// @Param        sourceId query string false "Filter by connected data source UUID, or 'none' for documents with no source"
// @Param        status query string false "Filter by derived status bucket" Enums(processing, ready, extracted, failed)
// @Param        q query string false "Case-insensitive substring match over filename and source URL"
// @Param        sort query string false "Sort key" Enums(created, updated) default(created)
// @Param        order query string false "Sort direction" Enums(asc, desc) default(desc)
// @Param        rootOnly query bool false "Filter to root documents only (no parent)"
// @Param        parentDocumentId query string false "Filter by parent document ID"
// @Success      200 {object} ListResult
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Router       /api/documents [get]
// @Security     bearerAuth
func (h *Handler) List(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	// Parse query parameters
	params := ListParams{
		ProjectID: user.ProjectID,
	}

	// Limit
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		limit, err := parsePositiveInt(limitStr, 1, 500)
		if err != nil {
			return apperror.ErrBadRequest.WithMessage("limit must be between 1 and 500")
		}
		params.Limit = limit
	}

	// Sort and direction. Both are validated vocabularies: an unrecognised value
	// is rejected rather than silently falling back, so a typo cannot look like a
	// working control. The normalizers return the client-facing *apperror.Error
	// themselves (BadRequest + the accepted vocabulary), so it is returned
	// unchanged rather than stringified into a new message.
	sortKey, err := NormalizeSort(c.QueryParam("sort"))
	if err != nil {
		return err
	}
	order, err := NormalizeOrder(c.QueryParam("order"))
	if err != nil {
		return err
	}
	params.Sort = sortKey
	params.Order = order

	// Status bucket
	status, err := NormalizeStatusBucket(c.QueryParam("status"))
	if err != nil {
		return err
	}
	params.Status = status

	// Search term (trimmed and length-capped)
	params.Query = NormalizeDocumentSearch(c.QueryParam("q"))

	// Cursor. A cursor minted for a different sort key would page with the wrong
	// comparison and silently skip or repeat rows, so the mismatch is refused.
	if cursorStr := c.QueryParam("cursor"); cursorStr != "" {
		cursor, err := ParseCursor(cursorStr)
		if err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid cursor")
		}
		if cursor.Sort != params.Sort {
			return apperror.ErrBadRequest.WithMessage("cursor does not match the requested sort")
		}
		params.Cursor = cursor
	}

	// Filters
	if sourceType := c.QueryParam("sourceType"); sourceType != "" {
		params.SourceType = &sourceType
	}
	if sourceID := strings.TrimSpace(c.QueryParam("sourceId")); sourceID != "" {
		if sourceID == sourceNoneFilter {
			params.SourceUnassigned = true
		} else {
			if _, err := uuid.Parse(sourceID); err != nil {
				return apperror.NewBadRequest("sourceId must be a source UUID or \"none\"")
			}
			params.SourceID = &sourceID
		}
	}
	if rootOnly := c.QueryParam("rootOnly"); rootOnly == "true" {
		params.RootOnly = true
	}
	if parentID := c.QueryParam("parentDocumentId"); parentID != "" {
		params.ParentDocumentID = &parentID
	}
	if c.QueryParam("includeDeletedFromSource") == "true" {
		params.IncludeDeletedFromSource = true
	}

	// Execute query
	result, err := h.svc.List(c.Request().Context(), params)
	if err != nil {
		return apperror.ErrInternal.WithInternal(err)
	}

	// Set cursor header if there are more results
	if result.NextCursor != nil {
		c.Response().Header().Set("x-next-cursor", *result.NextCursor)
	}

	return c.JSON(http.StatusOK, result)
}

// GetByID handles GET /api/documents/:id
// @Summary      Get document by ID
// @Description  Retrieve a single document by its ID
// @Tags         documents
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Document ID"
// @Success      200 {object} Document
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/documents/{id} [get]
// @Security     bearerAuth
func (h *Handler) GetByID(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	documentID := c.Param("id")
	if documentID == "" {
		return apperror.ErrBadRequest.WithMessage("document id required")
	}

	doc, err := h.svc.GetByID(c.Request().Context(), user.ProjectID, documentID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, doc)
}

// GetVersionHistory handles GET /api/documents/:id/versions
// @Summary      Get document version history
// @Description  Return every version of a document ordered by version number, with source revision and status
// @Tags         documents
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Document ID"
// @Success      200 {object} map[string][]DocumentVersion
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/documents/{id}/versions [get]
// @Security     bearerAuth
func (h *Handler) GetVersionHistory(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest
	}

	documentID := c.Param("id")
	if documentID == "" {
		return apperror.ErrBadRequest
	}

	versions, err := h.svc.GetVersionHistory(c.Request().Context(), user.ProjectID, documentID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]any{"versions": versions})
}

// Create handles POST /api/documents
// @Summary      Create document
// @Description  Create a new document or return existing if content hash matches (deduplication)
// @Tags         documents
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        request body CreateDocumentRequest true "Document data"
// @Success      201 {object} Document "Document created"
// @Success      200 {object} Document "Existing document returned (deduplicated)"
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Router       /api/documents [post]
// @Security     bearerAuth
func (h *Handler) Create(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	var req CreateDocumentRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("Invalid request body")
	}

	// Validate request
	if err := validateCreateRequest(&req); err != nil {
		return err
	}

	doc, wasCreated, err := h.svc.Create(c.Request().Context(), CreateParams{
		ProjectID:       user.ProjectID,
		CreatedByUserID: OptionalUserID(user.ID),
		Filename:        &req.Filename,
		Content:         &req.Content,
	})
	if err != nil {
		return err
	}

	// Trigger processing pipeline for newly created documents with content.
	// The upload path uses a parsing worker; raw-content documents skip parsing
	// because content is already stored inline. We still need to set conversion
	// status, chunk the text, and optionally trigger extraction.
	if wasCreated && req.Content != "" {
		requested := requestedAutoExtractFromQuery(c)
		autoExtract := h.effectiveAutoExtract(c.Request().Context(), user.ProjectID, requested)
		h.processNewDocument(c.Request().Context(), user.ProjectID, doc.ID, autoExtract)
	}

	// Return 201 for new document, 200 for deduplicated existing document
	status := http.StatusCreated
	if !wasCreated {
		status = http.StatusOK
	}

	return c.JSON(status, doc)
}

// processNewDocument triggers the post-create pipeline for raw-content documents:
// set conversion status → recreate chunks → optionally trigger extraction.
// Errors are logged but not returned — the document creation already succeeded.
func (h *Handler) processNewDocument(ctx context.Context, projectID, documentID string, autoExtract bool) {
	if err := h.svc.MarkConversionCompleted(ctx, documentID); err != nil {
		h.log.Warn("failed to mark conversion completed",
			slog.String("document_id", documentID),
			logger.Error(err))
		return
	}

	if h.chunking == nil {
		return
	}
	if err := h.chunking.RecreateChunks(ctx, projectID, documentID); err != nil {
		h.log.Warn("failed to recreate chunks for raw-content document",
			slog.String("document_id", documentID),
			logger.Error(err))
	}

	if !autoExtract || h.extractionJobs == nil {
		return
	}
	if err := h.extractionJobs.TriggerForDocument(ctx, projectID, documentID); err != nil {
		h.log.Warn("failed to trigger extraction for raw-content document",
			slog.String("document_id", documentID),
			logger.Error(err))
	}
}

// Delete handles DELETE /api/documents/:id
// @Summary      Delete document
// @Description  Delete a single document and all related entities (chunks, extraction jobs, graph objects)
// @Tags         documents
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Document ID"
// @Success      200 {object} DeleteResponse
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/documents/{id} [delete]
// @Security     bearerAuth
func (h *Handler) Delete(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	documentID := c.Param("id")
	if documentID == "" {
		return apperror.ErrBadRequest.WithMessage("document id required")
	}

	response, err := h.svc.Delete(c.Request().Context(), user.ProjectID, documentID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, response)
}

// BulkDelete handles DELETE /api/documents (with body)
// @Summary      Bulk delete documents
// @Description  Delete multiple documents and all related entities by ID list
// @Tags         documents
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        request body BulkDeleteRequest true "Document IDs to delete"
// @Success      200 {object} DeleteResponse
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Router       /api/documents [delete]
// @Security     bearerAuth
func (h *Handler) BulkDelete(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	var req BulkDeleteRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("Invalid request body")
	}

	// Validate request
	if len(req.IDs) == 0 {
		return apperror.ErrBadRequest.WithMessage("ids array is required and must not be empty")
	}

	response, err := h.svc.BulkDelete(c.Request().Context(), user.ProjectID, req.IDs)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, response)
}

// GetSourceTypes handles GET /api/documents/source-types
// @Summary      Get source types
// @Description  Returns a list of all available document source types with counts
// @Tags         documents
// @Accept       json
// @Produce      json
// @Success      200 {object} map[string][]SourceTypeWithCount
// @Failure      401 {object} apperror.Error
// @Router       /api/documents/source-types [get]
// @Security     bearerAuth
func (h *Handler) GetSourceTypes(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("project ID is required")
	}

	sourceTypes, err := h.svc.GetSourceTypes(c.Request().Context(), user.ProjectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string][]SourceTypeWithCount{
		"sourceTypes": sourceTypes,
	})
}

// GetSourceCounts handles GET /api/documents/source-counts
// @Summary      Get document counts by data source
// @Description  Returns document counts grouped by connected data source (empty sourceId for direct uploads), for the documents source filter.
// @Tags         documents
// @Accept       json
// @Produce      json
// @Success      200 {object} map[string][]SourceCount
// @Failure      401 {object} apperror.Error
// @Router       /api/documents/source-counts [get]
// @Security     bearerAuth
func (h *Handler) GetSourceCounts(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.NewBadRequest("project ID is required")
	}

	sources, err := h.svc.GetSourceCounts(c.Request().Context(), user.ProjectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string][]SourceCount{
		"sources": sources,
	})
}

// validateCreateRequest validates the create document request
func validateCreateRequest(req *CreateDocumentRequest) error {
	// Filename max length validation
	if len(req.Filename) > 512 {
		return apperror.ErrBadRequest.WithMessage("filename must be at most 512 characters")
	}
	return nil
}

// parsePositiveInt parses a string as an int and validates it's within bounds
func parsePositiveInt(s string, min, max int) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, apperror.ErrBadRequest
		}
		n = n*10 + int(c-'0')
		if n > max {
			return 0, apperror.ErrBadRequest
		}
	}
	if n < min {
		return 0, apperror.ErrBadRequest
	}
	return n, nil
}

// GetContent handles GET /api/documents/:id/content
// @Summary      Get document content
// @Description  Retrieve the text content of a document
// @Tags         documents
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Document ID"
// @Success      200 {object} ContentResponse
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/documents/{id}/content [get]
// @Security     bearerAuth
func (h *Handler) GetContent(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	documentID := c.Param("id")
	if documentID == "" {
		return apperror.ErrBadRequest.WithMessage("document id required")
	}

	// First verify the document exists and belongs to this project
	doc, err := h.svc.GetByID(c.Request().Context(), user.ProjectID, documentID)
	if err != nil {
		return err
	}
	if doc == nil {
		return apperror.ErrNotFound.WithMessage("Document not found")
	}

	// Get just the content
	content, err := h.svc.GetContent(c.Request().Context(), user.ProjectID, documentID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, ContentResponse{Content: content})
}

// GetDeletionImpact handles GET /api/documents/:id/deletion-impact
// @Summary      Get deletion impact
// @Description  Preview the impact of deleting a document (counts of related entities that will be deleted)
// @Tags         documents
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Document ID"
// @Success      200 {object} DeletionImpact
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/documents/{id}/deletion-impact [get]
// @Security     bearerAuth
func (h *Handler) GetDeletionImpact(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	documentID := c.Param("id")
	if documentID == "" {
		return apperror.ErrBadRequest.WithMessage("document id required")
	}

	impact, err := h.svc.GetDeletionImpact(c.Request().Context(), user.ProjectID, documentID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, impact)
}

// BulkDeletionImpact handles POST /api/documents/deletion-impact
// @Summary      Get bulk deletion impact
// @Description  Preview the impact of bulk deleting multiple documents
// @Tags         documents
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        request body BulkDeletionImpactRequest true "Document IDs to analyze"
// @Success      200 {object} BulkDeletionImpact
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Router       /api/documents/deletion-impact [post]
// @Security     bearerAuth
func (h *Handler) BulkDeletionImpact(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	var req BulkDeletionImpactRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("Invalid request body")
	}

	if len(req.IDs) == 0 {
		return apperror.ErrBadRequest.WithMessage("ids array is required and must not be empty")
	}

	impact, err := h.svc.GetBulkDeletionImpact(c.Request().Context(), user.ProjectID, req.IDs)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, impact)
}

// Download handles GET /api/documents/:id/download
// @Summary      Download document
// @Description  Returns a redirect to a signed URL for downloading the original file
// @Tags         documents
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Document ID"
// @Success      307 "Redirect to signed download URL"
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Failure      503 {object} apperror.Error "Storage service unavailable"
// @Router       /api/documents/{id}/download [get]
// @Security     bearerAuth
func (h *Handler) Download(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	documentID := c.Param("id")
	if documentID == "" {
		return apperror.ErrBadRequest.WithMessage("document id required")
	}

	// Check if storage is enabled
	if !h.storage.Enabled() {
		return apperror.New(503, "storage_unavailable", "Storage service is not configured")
	}

	// Get document storage info
	info, err := h.svc.GetStorageInfo(c.Request().Context(), user.ProjectID, documentID)
	if err != nil {
		return err
	}

	if info.StorageKey == nil || *info.StorageKey == "" {
		return apperror.ErrNotFound.WithMessage("No original file stored for this document")
	}

	// Generate signed URL for download
	contentDisposition := ""
	if info.Filename != nil && *info.Filename != "" {
		contentDisposition = mime.FormatMediaType("attachment", map[string]string{"filename": *info.Filename})
	}

	signedURL, err := h.storage.GetSignedDownloadURL(
		c.Request().Context(),
		*info.StorageKey,
		storage.GetSignedDownloadURLOptions{
			ExpiresIn:                  time.Hour, // 1 hour
			ResponseContentDisposition: contentDisposition,
		},
	)
	if err != nil {
		return apperror.ErrInternal.WithInternal(err)
	}

	return c.Redirect(http.StatusTemporaryRedirect, signedURL)
}

// GetExtractionSummary handles GET /api/documents/:id/extraction-summary
// @Summary      Get extraction summary
// @Description  Returns the extraction summary for the most recently completed extraction job for a document
// @Tags         documents
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Document ID"
// @Success      200 {object} ExtractionSummary
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/documents/{id}/extraction-summary [get]
// @Security     bearerAuth
func (h *Handler) GetExtractionSummary(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	documentID := c.Param("id")
	if documentID == "" {
		return apperror.ErrBadRequest.WithMessage("document id required")
	}

	summary, err := h.svc.GetExtractionSummary(c.Request().Context(), user.ProjectID, documentID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, summary)
}

// RetryExtraction handles POST /api/documents/:id/retry-extraction
// @Summary      Retry document extraction
// @Description  Re-queues the most recent failed or dead_letter extraction job for a document
// @Tags         documents
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Document ID"
// @Success      202 {object} map[string]any
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/documents/{id}/retry-extraction [post]
// @Security     bearerAuth
func (h *Handler) RetryExtraction(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	documentID := c.Param("id")
	if documentID == "" {
		return apperror.ErrBadRequest.WithMessage("document id required")
	}

	// Verify document exists and belongs to project
	doc, err := h.svc.GetByID(c.Request().Context(), user.ProjectID, documentID)
	if err != nil {
		return err
	}
	if doc == nil {
		return apperror.ErrNotFound.WithMessage("Document not found")
	}

	// Find latest retryable job for this document
	retryableJob, err := h.extraction.FindRetryableByDocument(c.Request().Context(), documentID, user.ProjectID)
	if err != nil {
		return apperror.ErrInternal.WithMessage("failed to find extraction jobs")
	}

	if retryableJob == nil {
		return apperror.ErrBadRequest.WithMessage("no retryable extraction job found for this document (only failed/dead_letter/processing jobs can be retried)")
	}

	job, err := h.extraction.RetryByJobID(c.Request().Context(), retryableJob.ID, user.ProjectID)
	if err != nil {
		return h.retryFailure(err)
	}

	return c.JSON(http.StatusAccepted, map[string]any{
		"job_id":  job.ID,
		"status":  job.Status,
		"message": "Extraction job queued for retry",
	})
}
