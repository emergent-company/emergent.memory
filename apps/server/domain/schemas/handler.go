package schemas

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Handler handles HTTP requests for memory schemas
type Handler struct {
	svc *Service
}

// NewHandler creates a new schemas handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// GetCompiledTypes handles GET /api/schemas/projects/:projectId/compiled-types
// @Summary      Get compiled types
// @Description  Returns compiled object and relationship type definitions for a project based on installed schemas
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {object} CompiledTypesResponse "Compiled type definitions"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/projects/{projectId}/compiled-types [get]
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

// GetAvailablePacks handles GET /api/schemas/projects/:projectId/available
// @Summary      List available schemas
// @Description  Returns schemas available for a project to install (not yet installed)
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {array} MemorySchemaListItem "Available schemas"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/projects/{projectId}/available [get]
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

// GetInstalledPacks handles GET /api/schemas/projects/:projectId/installed
// @Summary      List installed schemas
// @Description  Returns schemas currently installed and assigned to a project
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {array} InstalledSchemaItem "Installed schemas"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/projects/{projectId}/installed [get]
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

// AssignPack handles POST /api/schemas/projects/:projectId/assign
// @Summary      Assign schema
// @Description  Assigns a schema to a project, making its types available for use.
// @Description  When dry_run=true, returns a full conflict/merge preview (HTTP 200) without making changes.
// @Description  When merge=true, additively merges incoming type schemas into existing registered types.
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body AssignPackRequest true "Assignment request"
// @Success      200 {object} AssignPackResult "Dry-run preview (when dry_run=true)"
// @Success      201 {object} AssignPackResult "Created assignment"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/projects/{projectId}/assign [post]
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

	if req.SchemaID == "" {
		return apperror.ErrBadRequest.WithMessage("schema_id is required")
	}

	result, err := h.svc.AssignPack(c.Request().Context(), projectID, user.ID, &req)
	if err != nil {
		return err
	}

	if req.DryRun {
		return c.JSON(http.StatusOK, result)
	}
	return c.JSON(http.StatusCreated, result)
}

// UpdateAssignment handles PATCH /api/schemas/projects/:projectId/assignments/:assignmentId
// @Summary      Update pack assignment
// @Description  Updates a schema assignment (e.g., toggle active status)
// @Tags         schemas
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
// @Router       /api/schemas/projects/{projectId}/assignments/{assignmentId} [patch]
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

// DeleteAssignment handles DELETE /api/schemas/projects/:projectId/assignments/:assignmentId
// @Summary      Delete pack assignment
// @Description  Removes a schema assignment from a project
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        assignmentId path string true "Assignment ID (UUID)"
// @Success      204 "No content"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Assignment not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/projects/{projectId}/assignments/{assignmentId} [delete]
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

// CreatePack handles POST /api/schemas
// @Summary      Create schema
// @Description  Creates a new schema in the global registry with object type schemas, relationship schemas, and optional UI/extraction configs
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        request body CreatePackRequest true "Template pack definition"
// @Success      201 {object} GraphMemorySchema "Created schema"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas [post]
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

// GetPack handles GET /api/schemas/:packId
// @Summary      Get schema
// @Description  Returns a schema by ID including all schemas and configs
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        packId path string true "Template Pack ID (UUID)"
// @Success      200 {object} GraphMemorySchema "Template pack"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Template pack not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/{packId} [get]
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

// UpdatePack handles PUT /api/schemas/:packId
// @Summary      Update schema
// @Description  Partially updates an existing schema in the global registry
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        packId path string true "Template Pack ID (UUID)"
// @Param        request body UpdatePackRequest true "Fields to update (all optional)"
// @Success      200 {object} GraphMemorySchema "Updated schema"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Template pack not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/{packId} [put]
// @Security     bearerAuth
func (h *Handler) UpdatePack(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	packID := c.Param("packId")
	if packID == "" {
		return apperror.ErrBadRequest.WithMessage("packId is required")
	}

	var req UpdatePackRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	pack, err := h.svc.UpdatePack(c.Request().Context(), packID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, pack)
}

// DeletePack handles DELETE /api/schemas/:packId
// @Summary      Delete schema
// @Description  Deletes a schema from the global registry. Fails if the schema is assigned to any projects.
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        packId path string true "Template Pack ID (UUID)"
// @Success      204 "No content"
// @Failure      400 {object} apperror.Error "Bad request (e.g., pack is assigned to projects)"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Template pack not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/{packId} [delete]
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
