package processingproblems

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Handler exposes the project's processing-problem report.
type Handler struct {
	svc *Service
}

// NewHandler creates the HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// List handles GET /api/projects/:projectId/processing-problems.
// @Summary      List processing problems
// @Description  Terminal failures across every project-scoped job queue, grouped by area with a classified cause and a next action.
// @Tags         processing
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Success      200 {object} Report
// @Failure      401 {object} apperror.Error
// @Failure      403 {object} apperror.Error
// @Router       /api/projects/{projectId}/processing-problems [get]
// @Security     bearerAuth
func (h *Handler) List(c echo.Context) error {
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("project id required")
	}
	if err := auth.RequireProjectMembership(c.Request().Context(), h.svc.DB(), projectID); err != nil {
		return err
	}

	report, err := h.svc.List(c.Request().Context(), projectID)
	if err != nil {
		return apperror.NewInternal("failed to load processing problems", err)
	}
	return c.JSON(http.StatusOK, report)
}
