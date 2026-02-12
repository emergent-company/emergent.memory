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

// CreatePack handles POST /api/template-packs
// @Summary      Create template pack
// @Description  Creates a new template pack in the global registry with object type schemas, relationship schemas, and optional UI/extraction configs
// @Tags         template-packs
// @Accept       json
// @Produce      json
// @Param        request body CreatePackRequest true "Template pack definition"
// @Success      201 {object} GraphTemplatePack "Created template pack"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/template-packs [post]
// @Security     bearerAuth
func (h *Handler) CreatePack(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var req CreatePackRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.Name == "" {
		return apperror.ErrBadRequest.WithMessage("name is required")
	}
	if req.Version == "" {
		return apperror.ErrBadRequest.WithMessage("version is required")
	}
	if len(req.ObjectTypeSchemas) == 0 {
		return apperror.ErrBadRequest.WithMessage("object_type_schemas is required")
	}

	pack, err := h.svc.CreatePack(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, pack)
}

// GetPack handles GET /api/template-packs/:packId
// @Summary      Get template pack
// @Description  Returns a template pack by ID including all schemas and configs
// @Tags         template-packs
// @Accept       json
// @Produce      json
// @Param        packId path string true "Template Pack ID (UUID)"
// @Success      200 {object} GraphTemplatePack "Template pack"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Template pack not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/template-packs/{packId} [get]
// @Security     bearerAuth
func (h *Handler) GetPack(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	packID := c.Param("packId")
	if packID == "" {
		return apperror.ErrBadRequest.WithMessage("packId is required")
	}

	pack, err := h.svc.GetPack(c.Request().Context(), packID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, pack)
}

// DeletePack handles DELETE /api/template-packs/:packId
// @Summary      Delete template pack
// @Description  Deletes a template pack from the global registry. Fails if the pack is assigned to any projects.
// @Tags         template-packs
// @Accept       json
// @Produce      json
// @Param        packId path string true "Template Pack ID (UUID)"
// @Success      204 "No content"
// @Failure      400 {object} apperror.Error "Bad request (e.g., pack is assigned to projects)"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Template pack not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/template-packs/{packId} [delete]
// @Security     bearerAuth
func (h *Handler) DeletePack(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	packID := c.Param("packId")
	if packID == "" {
		return apperror.ErrBadRequest.WithMessage("packId is required")
	}

	if err := h.svc.DeletePack(c.Request().Context(), packID); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
