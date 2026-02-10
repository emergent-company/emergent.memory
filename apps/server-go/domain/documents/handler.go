package documents

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/internal/storage"
	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Handler handles document HTTP requests
type Handler struct {
	svc     *Service
	storage *storage.Service
	log     *slog.Logger
}

// NewHandler creates a new documents handler
func NewHandler(
	svc *Service,
	storageSvc *storage.Service,
	log *slog.Logger,
) *Handler {
	return &Handler{
		svc:     svc,
		storage: storageSvc,
		log:     log.With(logger.Scope("documents.handler")),
	}
}

// List handles GET /api/v2/documents
func (h *Handler) List(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

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

	// Cursor
	if cursorStr := c.QueryParam("cursor"); cursorStr != "" {
		cursor, err := ParseCursor(cursorStr)
		if err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid cursor")
		}
		params.Cursor = cursor
	}

	// Filters
	if sourceType := c.QueryParam("sourceType"); sourceType != "" {
		params.SourceType = &sourceType
	}
	if integrationID := c.QueryParam("integrationId"); integrationID != "" {
		params.IntegrationID = &integrationID
	}
	if rootOnly := c.QueryParam("rootOnly"); rootOnly == "true" {
		params.RootOnly = true
	}
	if parentID := c.QueryParam("parentDocumentId"); parentID != "" {
		params.ParentDocumentID = &parentID
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

// GetByID handles GET /api/v2/documents/:id
func (h *Handler) GetByID(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

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

// Create handles POST /api/v2/documents
func (h *Handler) Create(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

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
		ProjectID: user.ProjectID,
		Filename:  &req.Filename,
		Content:   &req.Content,
	})
	if err != nil {
		return err
	}

	// Return 201 for new document, 200 for deduplicated existing document
	status := http.StatusCreated
	if !wasCreated {
		status = http.StatusOK
	}

	return c.JSON(status, doc)
}

// Delete handles DELETE /api/v2/documents/:id
func (h *Handler) Delete(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

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

// BulkDelete handles DELETE /api/v2/documents (with body)
func (h *Handler) BulkDelete(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

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
// Returns a list of all available document source types
func (h *Handler) GetSourceTypes(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	sourceTypes, err := h.svc.GetSourceTypes(c.Request().Context())
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string][]SourceTypeWithCount{
		"sourceTypes": sourceTypes,
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

// GetContent handles GET /api/v2/documents/:id/content
func (h *Handler) GetContent(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

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

// GetDeletionImpact handles GET /api/v2/documents/:id/deletion-impact
func (h *Handler) GetDeletionImpact(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

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

// BulkDeletionImpact handles POST /api/v2/documents/deletion-impact
func (h *Handler) BulkDeletionImpact(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

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

// Download handles GET /api/v2/documents/:id/download
// Returns a redirect to a signed URL for downloading the original file
func (h *Handler) Download(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

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
		contentDisposition = `attachment; filename="` + *info.Filename + `"`
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

func (h *Handler) Upload(c echo.Context) error {
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

	file, err := c.FormFile("file")
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("file required in multipart form")
	}

	maxSize := int64(100 * 1024 * 1024)
	if file.Size > maxSize {
		return apperror.New(413, "file_too_large", "File size exceeds 100MB limit")
	}

	src, err := file.Open()
	if err != nil {
		return apperror.ErrInternal.WithMessage("failed to read uploaded file")
	}
	defer src.Close()

	uploadResult, err := h.storage.UploadDocument(
		c.Request().Context(),
		src,
		file.Size,
		storage.DocumentUploadOptions{
			ProjectID: user.ProjectID,
			OrgID:     user.OrgID,
			Filename:  file.Filename,
			UploadOptions: storage.UploadOptions{
				ContentType: file.Header.Get("Content-Type"),
			},
		},
	)
	if err != nil {
		return apperror.ErrInternal.WithInternal(err).WithMessage("failed to upload file")
	}

	sourceType := "upload"
	if st := c.FormValue("source_type"); st != "" {
		sourceType = st
	}

	doc, wasCreated, err := h.svc.Create(c.Request().Context(), CreateParams{
		ProjectID:     user.ProjectID,
		Filename:      &file.Filename,
		StorageKey:    &uploadResult.Key,
		SourceType:    &sourceType,
		MimeType:      &uploadResult.ContentType,
		FileSizeBytes: &uploadResult.Size,
	})
	if err != nil {
		return apperror.ErrInternal.WithInternal(err).WithMessage("failed to create document record")
	}

	response := map[string]any{
		"document":    doc,
		"was_created": wasCreated,
		"message":     "Document uploaded successfully",
	}

	status := http.StatusCreated
	if !wasCreated {
		status = http.StatusOK
		response["message"] = "Document already exists (deduplicated by content hash)"
	}

	return c.JSON(status, response)
}
