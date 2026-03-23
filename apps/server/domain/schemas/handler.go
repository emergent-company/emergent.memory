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

	packs, err := h.svc.GetAvailablePacks(c.Request().Context(), projectID, user.OrgID)
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

	// Determine project ID from auth context
	projectID := user.APITokenProjectID
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project context required (X-Project-ID header or API token)")
	}

	orgID := user.OrgID
	if orgID == "" {
		return apperror.ErrBadRequest.WithMessage("organization context required (X-Org-ID header)")
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
	if len(req.GetObjectTypeSchemas()) == 0 {
		return apperror.ErrBadRequest.WithMessage("object_type_schemas is required")
	}

	pack, err := h.svc.CreatePack(c.Request().Context(), projectID, orgID, &req)
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

	projectID := user.APITokenProjectID
	if projectID == "" {
		projectID = user.ProjectID
	}

	pack, err := h.svc.GetPack(c.Request().Context(), packID, projectID, user.OrgID)
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

	projectID := user.APITokenProjectID
	if projectID == "" {
		projectID = user.ProjectID
	}

	var req UpdatePackRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	pack, err := h.svc.UpdatePack(c.Request().Context(), packID, projectID, user.OrgID, &req)
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

	projectID := user.APITokenProjectID
	if projectID == "" {
		projectID = user.ProjectID
	}

	if err := h.svc.DeletePack(c.Request().Context(), packID, projectID, user.OrgID); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// GetAllPacks handles GET /api/schemas/projects/:projectId/all
// @Summary      List all schemas (installed + available)
// @Description  Returns all schemas visible to a project — both installed and available — in a single unified list
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {array} UnifiedSchemaItem "All schemas"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/projects/{projectId}/all [get]
// @Security     bearerAuth
func (h *Handler) GetAllPacks(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	packs, err := h.svc.GetAllPacks(c.Request().Context(), projectID, user.OrgID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, packs)
}

// GetSchemaHistory handles GET /api/schemas/projects/:projectId/history
// @Summary      Schema installation history
// @Description  Returns all schema assignments for a project including removed (soft-deleted) ones
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {array} SchemaHistoryItem "Schema history"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/projects/{projectId}/history [get]
// @Security     bearerAuth
func (h *Handler) GetSchemaHistory(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	history, err := h.svc.GetSchemaHistory(c.Request().Context(), projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, history)
}

// MigrateTypes handles POST /api/schemas/projects/:projectId/migrate
// @Summary      Migrate live graph data
// @Description  Renames object/edge types and/or property keys across live graph objects and edges in a single transaction. Supports dry_run.
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body MigrateRequest true "Migration request"
// @Success      200 {object} MigrateResponse "Migration result (or dry-run preview)"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/projects/{projectId}/migrate [post]
// @Security     bearerAuth
func (h *Handler) MigrateTypes(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var req MigrateRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if len(req.TypeRenames) == 0 && len(req.PropertyRenames) == 0 {
		return apperror.ErrBadRequest.WithMessage("at least one type_rename or property_rename is required")
	}

	result, err := h.svc.MigrateTypes(c.Request().Context(), projectID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// PreviewMigration handles POST /api/schemas/projects/:projectId/migrate/preview
// @Summary      Preview schema migration
// @Description  Runs a dry-run migration against all project objects to assess risk before executing. Returns per-type risk breakdown and an overall risk level.
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body SchemaMigrationPreviewRequest true "Preview request"
// @Success      200 {object} SchemaMigrationPreviewResponse "Migration preview"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/projects/{projectId}/migrate/preview [post]
// @Security     bearerAuth
func (h *Handler) PreviewMigration(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var req SchemaMigrationPreviewRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.FromSchemaID == "" || req.ToSchemaID == "" {
		return apperror.ErrBadRequest.WithMessage("from_schema_id and to_schema_id are required")
	}

	result, err := h.svc.PreviewSchemaMigration(c.Request().Context(), projectID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// ExecuteMigration handles POST /api/schemas/projects/:projectId/migrate/execute
// @Summary      Execute schema migration
// @Description  Executes a schema migration for all project objects, applying type/property renames and archiving removed properties.
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body SchemaMigrationExecuteRequest true "Execute request"
// @Success      200 {object} SchemaMigrationExecuteResponse "Migration result"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/projects/{projectId}/migrate/execute [post]
// @Security     bearerAuth
func (h *Handler) ExecuteMigration(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var req SchemaMigrationExecuteRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.FromSchemaID == "" || req.ToSchemaID == "" {
		return apperror.ErrBadRequest.WithMessage("from_schema_id and to_schema_id are required")
	}

	result, err := h.svc.ExecuteSchemaMigration(c.Request().Context(), projectID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// RollbackMigration handles POST /api/schemas/projects/:projectId/migrate/rollback
// @Summary      Rollback schema migration
// @Description  Restores archived property data for objects that were migrated to a given schema version. Optionally re-installs old schema types.
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body SchemaMigrationRollbackRequest true "Rollback request"
// @Success      200 {object} SchemaMigrationRollbackResponse "Rollback result"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/projects/{projectId}/migrate/rollback [post]
// @Security     bearerAuth
func (h *Handler) RollbackMigration(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var req SchemaMigrationRollbackRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.ToVersion == "" {
		return apperror.ErrBadRequest.WithMessage("to_version is required")
	}

	result, err := h.svc.RollbackSchemaMigration(c.Request().Context(), projectID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// CommitMigrationArchive handles POST /api/schemas/projects/:projectId/migrate/commit
// @Summary      Commit migration archive
// @Description  Prunes migration_archive entries up to a given schema version, permanently discarding archived data that is no longer needed for rollback.
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body CommitMigrationArchiveRequest true "Commit request"
// @Success      200 {object} CommitMigrationArchiveResponse "Commit result"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/projects/{projectId}/migrate/commit [post]
// @Security     bearerAuth
func (h *Handler) CommitMigrationArchive(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var req CommitMigrationArchiveRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.ThroughVersion == "" {
		return apperror.ErrBadRequest.WithMessage("through_version is required")
	}

	result, err := h.svc.CommitMigrationArchive(c.Request().Context(), projectID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// GetMigrationJobStatus handles GET /api/schemas/projects/:projectId/migration-jobs/:jobId
// @Summary      Get migration job status
// @Description  Returns the current status and progress of an async schema migration job.
// @Tags         schemas
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        jobId path string true "Migration Job ID (UUID)"
// @Success      200 {object} SchemaMigrationJob "Migration job"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/schemas/projects/{projectId}/migration-jobs/{jobId} [get]
// @Security     bearerAuth
func (h *Handler) GetMigrationJobStatus(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	jobID := c.Param("jobId")
	if jobID == "" {
		return apperror.ErrBadRequest.WithMessage("jobId is required")
	}

	job, err := h.svc.GetMigrationJobStatus(c.Request().Context(), projectID, jobID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, job)
}
