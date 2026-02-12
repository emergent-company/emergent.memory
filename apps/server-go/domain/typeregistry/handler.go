package typeregistry

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for the type registry
type Handler struct {
	repo *Repository
}

// NewHandler creates a new type registry handler
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// GetProjectTypes handles GET /api/type-registry/projects/:projectId
// @Summary      List project types
// @Description  Returns all object types registered for a project with optional filtering
// @Tags         type-registry
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        enabled_only query boolean false "Filter enabled types only (default true)" default(true)
// @Param        source query string false "Filter by source" Enums(template,custom,discovered,all)
// @Param        search query string false "Search in type names"
// @Success      200 {array} TypeRegistryEntryDTO "List of types"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/type-registry/projects/{projectId} [get]
// @Security     bearerAuth
func (h *Handler) GetProjectTypes(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	// Parse query parameters
	query := ListTypesQuery{
		EnabledOnly: c.QueryParam("enabled_only") == "true",
		Source:      c.QueryParam("source"),
		Search:      c.QueryParam("search"),
	}

	// Default enabled_only to true if not specified (matches NestJS)
	if c.QueryParam("enabled_only") == "" {
		query.EnabledOnly = true
	}

	types, err := h.repo.GetProjectTypes(c.Request().Context(), projectID, query)
	if err != nil {
		if strings.Contains(err.Error(), "project not found") {
			return apperror.NewBadRequest("Project not found")
		}
		return apperror.NewInternal("failed to get project types", err)
	}

	return c.JSON(http.StatusOK, types)
}

// GetObjectType handles GET /api/type-registry/projects/:projectId/types/:typeName
// @Summary      Get object type
// @Description  Returns a specific object type definition including incoming/outgoing relationships
// @Tags         type-registry
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        typeName path string true "Object type name"
// @Success      200 {object} TypeRegistryEntryDTO "Type definition"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Type not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/type-registry/projects/{projectId}/types/{typeName} [get]
// @Security     bearerAuth
func (h *Handler) GetObjectType(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	typeName := c.Param("typeName")
	if typeName == "" {
		return apperror.NewBadRequest("typeName is required")
	}

	typeEntry, err := h.repo.GetTypeByName(c.Request().Context(), projectID, typeName)
	if err != nil {
		if strings.Contains(err.Error(), "type not found") {
			return apperror.NewNotFound("Type", typeName)
		}
		if strings.Contains(err.Error(), "project not found") {
			return apperror.NewBadRequest("Project not found")
		}
		return apperror.NewInternal("failed to get type", err)
	}

	return c.JSON(http.StatusOK, typeEntry)
}

// GetTypeStats handles GET /api/type-registry/projects/:projectId/stats
// @Summary      Get type statistics
// @Description  Returns statistics about a project's type registry including counts and object distribution
// @Tags         type-registry
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {object} TypeRegistryStats "Type statistics"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/type-registry/projects/{projectId}/stats [get]
// @Security     bearerAuth
func (h *Handler) GetTypeStats(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	stats, err := h.repo.GetTypeStats(c.Request().Context(), projectID)
	if err != nil {
		if strings.Contains(err.Error(), "project not found") {
			return apperror.NewBadRequest("Project not found")
		}
		return apperror.NewInternal("failed to get type stats", err)
	}

	return c.JSON(http.StatusOK, stats)
}

// CreateType handles POST /api/type-registry/projects/:projectId/types
// @Summary      Register custom type
// @Description  Registers a new custom object type for a project with JSON schema, UI config, and extraction config
// @Tags         type-registry
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body CreateTypeRequest true "Type definition"
// @Success      201 {object} ProjectObjectTypeRegistry "Created type"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      409 {object} apperror.Error "Type already exists"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/type-registry/projects/{projectId}/types [post]
// @Security     bearerAuth
func (h *Handler) CreateType(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	var req CreateTypeRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	if req.TypeName == "" {
		return apperror.NewBadRequest("type_name is required")
	}
	if len(req.JSONSchema) == 0 {
		return apperror.NewBadRequest("json_schema is required")
	}

	entry, err := h.repo.CreateType(c.Request().Context(), projectID, user.ID, &req)
	if err != nil {
		if strings.Contains(err.Error(), "project not found") {
			return apperror.NewBadRequest("Project not found")
		}
		if strings.Contains(err.Error(), "type already exists") {
			return apperror.ErrConflict.WithMessage("type already exists: " + req.TypeName)
		}
		return apperror.NewInternal("failed to create type", err)
	}

	return c.JSON(http.StatusCreated, entry)
}

// UpdateType handles PUT /api/type-registry/projects/:projectId/types/:typeName
// @Summary      Update registered type
// @Description  Updates an existing type in the project type registry. Bumps schema_version when json_schema changes.
// @Tags         type-registry
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        typeName path string true "Object type name"
// @Param        request body UpdateTypeRequest true "Update request"
// @Success      200 {object} ProjectObjectTypeRegistry "Updated type"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Type not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/type-registry/projects/{projectId}/types/{typeName} [put]
// @Security     bearerAuth
func (h *Handler) UpdateType(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	typeName := c.Param("typeName")
	if projectID == "" || typeName == "" {
		return apperror.NewBadRequest("projectId and typeName are required")
	}

	var req UpdateTypeRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	entry, err := h.repo.UpdateType(c.Request().Context(), projectID, typeName, &req)
	if err != nil {
		if strings.Contains(err.Error(), "project not found") {
			return apperror.NewBadRequest("Project not found")
		}
		if strings.Contains(err.Error(), "type not found") {
			return apperror.NewNotFound("Type", typeName)
		}
		if strings.Contains(err.Error(), "no update fields") {
			return apperror.NewBadRequest("no update fields provided")
		}
		return apperror.NewInternal("failed to update type", err)
	}

	return c.JSON(http.StatusOK, entry)
}

// DeleteType handles DELETE /api/type-registry/projects/:projectId/types/:typeName
// @Summary      Delete registered type
// @Description  Removes a type from the project type registry
// @Tags         type-registry
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        typeName path string true "Object type name"
// @Success      204 "No content"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Type not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/type-registry/projects/{projectId}/types/{typeName} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteType(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	typeName := c.Param("typeName")
	if projectID == "" || typeName == "" {
		return apperror.NewBadRequest("projectId and typeName are required")
	}

	err := h.repo.DeleteType(c.Request().Context(), projectID, typeName)
	if err != nil {
		if strings.Contains(err.Error(), "project not found") {
			return apperror.NewBadRequest("Project not found")
		}
		if strings.Contains(err.Error(), "type not found") {
			return apperror.NewNotFound("Type", typeName)
		}
		return apperror.NewInternal("failed to delete type", err)
	}

	return c.NoContent(http.StatusNoContent)
}
