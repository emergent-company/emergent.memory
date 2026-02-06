package templatepacks

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for template packs
type Handler struct {
	svc *Service
}

// NewHandler creates a new template packs handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// GetCompiledTypes handles GET /api/template-packs/projects/:projectId/compiled-types
// Returns compiled object and relationship types for a project
func (h *Handler) GetCompiledTypes(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	compiledTypes, err := h.svc.GetCompiledTypes(c.Request().Context(), projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, compiledTypes)
}

// GetAvailablePacks handles GET /api/template-packs/projects/:projectId/available
// Returns template packs available for a project to install
func (h *Handler) GetAvailablePacks(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	packs, err := h.svc.GetAvailablePacks(c.Request().Context(), projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, packs)
}

// GetInstalledPacks handles GET /api/template-packs/projects/:projectId/installed
// Returns template packs installed for a project
func (h *Handler) GetInstalledPacks(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	packs, err := h.svc.GetInstalledPacks(c.Request().Context(), projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, packs)
}

// AssignPack handles POST /api/template-packs/projects/:projectId/assign
// Assigns a template pack to a project
func (h *Handler) AssignPack(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var req AssignPackRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.TemplatePackID == "" {
		return apperror.ErrBadRequest.WithMessage("template_pack_id is required")
	}

	assignment, err := h.svc.AssignPack(c.Request().Context(), projectID, user.ID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, assignment)
}

// UpdateAssignment handles PATCH /api/template-packs/projects/:projectId/assignments/:assignmentId
// Updates a pack assignment (e.g., active status)
func (h *Handler) UpdateAssignment(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	assignmentID := c.Param("assignmentId")
	if projectID == "" || assignmentID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId and assignmentId are required")
	}

	var req UpdateAssignmentRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if err := h.svc.UpdateAssignment(c.Request().Context(), projectID, assignmentID, &req); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteAssignment handles DELETE /api/template-packs/projects/:projectId/assignments/:assignmentId
// Removes a pack assignment from a project
func (h *Handler) DeleteAssignment(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	assignmentID := c.Param("assignmentId")
	if projectID == "" || assignmentID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId and assignmentId are required")
	}

	if err := h.svc.DeleteAssignment(c.Request().Context(), projectID, assignmentID); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
