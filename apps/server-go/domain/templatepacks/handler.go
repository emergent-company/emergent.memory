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
// @Summary      Get compiled types
// @Description  Returns compiled object and relationship type definitions for a project based on installed template packs
// @Tags         template-packs
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {object} CompiledTypesResponse "Compiled type definitions"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/template-packs/projects/{projectId}/compiled-types [get]
// @Security     bearerAuth
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
// @Summary      List available template packs
// @Description  Returns template packs available for a project to install (not yet installed)
// @Tags         template-packs
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {array} TemplatePackListItem "Available template packs"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/template-packs/projects/{projectId}/available [get]
// @Security     bearerAuth
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
// @Summary      List installed template packs
// @Description  Returns template packs currently installed and assigned to a project
// @Tags         template-packs
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {array} InstalledPackItem "Installed template packs"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/template-packs/projects/{projectId}/installed [get]
// @Security     bearerAuth
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
// @Summary      Assign template pack
// @Description  Assigns a template pack to a project, making its types available for use
// @Tags         template-packs
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body AssignPackRequest true "Assignment request"
// @Success      201 {object} ProjectTemplatePack "Created assignment"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/template-packs/projects/{projectId}/assign [post]
// @Security     bearerAuth
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
// @Summary      Update pack assignment
// @Description  Updates a template pack assignment (e.g., toggle active status)
// @Tags         template-packs
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        assignmentId path string true "Assignment ID (UUID)"
// @Param        request body UpdateAssignmentRequest true "Update request"
// @Success      200 {object} map[string]string "Update confirmation"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Assignment not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/template-packs/projects/{projectId}/assignments/{assignmentId} [patch]
// @Security     bearerAuth
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
// @Summary      Delete pack assignment
// @Description  Removes a template pack assignment from a project
// @Tags         template-packs
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        assignmentId path string true "Assignment ID (UUID)"
// @Success      204 "No content"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Assignment not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/template-packs/projects/{projectId}/assignments/{assignmentId} [delete]
// @Security     bearerAuth
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
