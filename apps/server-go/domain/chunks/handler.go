package chunks

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for chunks
type Handler struct {
	svc *Service
}

// NewHandler creates a new chunks handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// getProjectID extracts and parses the project ID from the request context.
func getProjectID(c echo.Context) (uuid.UUID, error) {
	projectIDStr, err := auth.GetProjectID(c)
	if err != nil {
		return uuid.Nil, err
	}

	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		return uuid.Nil, apperror.ErrBadRequest.WithMessage("invalid project ID")
	}

	return projectID, nil
}

// List handles GET /chunks
// @Summary List chunks
// @Description List all chunks for the project, optionally filtered by document ID
// @Tags chunks
// @Accept json
// @Produce json
// @Param documentId query string false "Filter by document ID"
// @Success 200 {object} ListChunksResponse
// @Failure 400 {object} apperror.Error
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /chunks [get]
func (h *Handler) List(c echo.Context) error {
	projectID, err := getProjectID(c)
	if err != nil {
		return err
	}

	// Parse optional document ID filter
	var documentID *uuid.UUID
	if docIDStr := c.QueryParam("documentId"); docIDStr != "" {
		parsed, err := uuid.Parse(docIDStr)
		if err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid document ID")
		}
		documentID = &parsed
	}

	response, err := h.svc.List(c.Request().Context(), projectID, documentID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, response)
}

// Delete handles DELETE /chunks/:id
// @Summary Delete a chunk
// @Description Delete a single chunk by ID
// @Tags chunks
// @Accept json
// @Produce json
// @Param id path string true "Chunk ID"
// @Success 204
// @Failure 400 {object} apperror.Error
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Failure 404 {object} apperror.Error
// @Router /chunks/{id} [delete]
func (h *Handler) Delete(c echo.Context) error {
	projectID, err := getProjectID(c)
	if err != nil {
		return err
	}

	chunkID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid chunk ID")
	}

	if err := h.svc.Delete(c.Request().Context(), projectID, chunkID); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// BulkDelete handles DELETE /chunks
// @Summary Bulk delete chunks
// @Description Delete multiple chunks by IDs
// @Tags chunks
// @Accept json
// @Produce json
// @Param body body BulkDeleteRequest true "Chunk IDs to delete"
// @Success 200 {object} BulkDeletionSummary
// @Failure 400 {object} apperror.Error
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /chunks [delete]
func (h *Handler) BulkDelete(c echo.Context) error {
	projectID, err := getProjectID(c)
	if err != nil {
		return err
	}

	var req BulkDeleteRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if len(req.IDs) == 0 {
		return apperror.ErrBadRequest.WithMessage("ids array cannot be empty")
	}

	result, err := h.svc.BulkDelete(c.Request().Context(), projectID, req.IDs)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// DeleteByDocument handles DELETE /chunks/by-document/:documentId
// @Summary Delete chunks by document
// @Description Delete all chunks for a specific document
// @Tags chunks
// @Accept json
// @Produce json
// @Param documentId path string true "Document ID"
// @Success 200 {object} DocumentChunksDeletionResult
// @Failure 400 {object} apperror.Error
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /chunks/by-document/{documentId} [delete]
func (h *Handler) DeleteByDocument(c echo.Context) error {
	projectID, err := getProjectID(c)
	if err != nil {
		return err
	}

	documentID, err := uuid.Parse(c.Param("documentId"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid document ID")
	}

	result, err := h.svc.DeleteByDocument(c.Request().Context(), projectID, documentID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// BulkDeleteByDocuments handles DELETE /chunks/by-documents
// @Summary Bulk delete chunks by documents
// @Description Delete all chunks for multiple documents
// @Tags chunks
// @Accept json
// @Produce json
// @Param body body BulkDeleteByDocumentsRequest true "Document IDs"
// @Success 200 {object} BulkDocumentChunksDeletionSummary
// @Failure 400 {object} apperror.Error
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /chunks/by-documents [delete]
func (h *Handler) BulkDeleteByDocuments(c echo.Context) error {
	projectID, err := getProjectID(c)
	if err != nil {
		return err
	}

	var req BulkDeleteByDocumentsRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if len(req.DocumentIDs) == 0 {
		return apperror.ErrBadRequest.WithMessage("documentIds array cannot be empty")
	}

	result, err := h.svc.BulkDeleteByDocuments(c.Request().Context(), projectID, req.DocumentIDs)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}
