package chunking

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles chunking HTTP requests
type Handler struct {
	svc *Service
}

// NewHandler creates a new chunking handler
func NewHandler(svc *Service) *Handler {
	return &Handler{
		svc: svc,
	}
}

// RecreateChunks handles POST /api/documents/:id/recreate-chunks
// @Summary      Recreate document chunks
// @Description  Deletes existing chunks for a document and regenerates them using the current chunking strategy
// @Tags         chunking
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Document ID (UUID)"
// @Success      200 {object} RecreateChunksResponse "Chunking result"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Document not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/documents/{id}/recreate-chunks [post]
// @Security     bearerAuth
func (h *Handler) RecreateChunks(c echo.Context) error {
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

	result, err := h.svc.RecreateChunks(c.Request().Context(), user.ProjectID, documentID)
	if err != nil {
		return err // apperror is already wrapped by service
	}

	return c.JSON(http.StatusOK, result)
}
