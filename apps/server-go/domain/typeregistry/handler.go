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
