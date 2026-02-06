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
