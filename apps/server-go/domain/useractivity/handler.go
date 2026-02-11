package useractivity

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for user activity
type Handler struct {
	svc *Service
}

// NewHandler creates a new user activity handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Record handles POST /api/user-activity/record
// @Summary      Record user activity
// @Description  Records a user activity event for recent items tracking
// @Tags         user-activity
// @Accept       json
// @Produce      json
// @Param        project_id query string true "Project ID (UUID)"
// @Param        request body RecordActivityRequest true "Activity details"
// @Success      200 {object} map[string]string "Activity recorded"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/user-activity/record [post]
// @Security     bearerAuth
func (h *Handler) Record(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project_id query param is required")
	}

	var req RecordActivityRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("Invalid request body")
	}

	if req.ResourceType == "" || req.ResourceID == "" || req.ActionType == "" {
		return apperror.ErrBadRequest.WithMessage("resourceType, resourceId, and actionType are required")
	}

	if err := h.svc.Record(c.Request().Context(), user.ID, projectID, &req); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "recorded"})
}

// GetRecent handles GET /api/user-activity/recent
// @Summary      Get recent activity
// @Description  Retrieves the user's recent activity across all resource types
// @Tags         user-activity
// @Accept       json
// @Produce      json
// @Param        limit query int false "Max results (default 20)" minimum(1) maximum(100) default(20)
// @Success      200 {object} RecentItemsResponse "Recent items"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/user-activity/recent [get]
// @Security     bearerAuth
func (h *Handler) GetRecent(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	limit := 20
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	result, err := h.svc.GetRecent(c.Request().Context(), user.ID, limit)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// GetRecentByType handles GET /api/user-activity/recent/:type
// @Summary      Get recent activity by type
// @Description  Retrieves the user's recent activity filtered by resource type
// @Tags         user-activity
// @Accept       json
// @Produce      json
// @Param        type path string true "Resource type (e.g., 'document', 'graph_object')"
// @Param        limit query int false "Max results (default 20)" minimum(1) maximum(100) default(20)
// @Success      200 {object} RecentItemsResponse "Recent items"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/user-activity/recent/{type} [get]
// @Security     bearerAuth
func (h *Handler) GetRecentByType(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	resourceType := c.Param("type")
	if resourceType == "" {
		return apperror.ErrBadRequest.WithMessage("type parameter is required")
	}

	limit := 20
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	result, err := h.svc.GetRecentByType(c.Request().Context(), user.ID, resourceType, limit)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// DeleteAll handles DELETE /api/user-activity/recent
// @Summary      Delete all recent activity
// @Description  Deletes all recent activity records for the authenticated user
// @Tags         user-activity
// @Accept       json
// @Produce      json
// @Success      200 {object} map[string]string "All activity deleted"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/user-activity/recent [delete]
// @Security     bearerAuth
func (h *Handler) DeleteAll(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if err := h.svc.DeleteAll(c.Request().Context(), user.ID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// DeleteByResource handles DELETE /api/user-activity/recent/:type/:resourceId
// @Summary      Delete activity by resource
// @Description  Deletes a specific recent activity record by type and resource ID
// @Tags         user-activity
// @Accept       json
// @Produce      json
// @Param        type path string true "Resource type"
// @Param        resourceId path string true "Resource ID (UUID)"
// @Success      200 {object} map[string]string "Activity deleted"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/user-activity/recent/{type}/{resourceId} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteByResource(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	resourceType := c.Param("type")
	resourceID := c.Param("resourceId")

	if resourceType == "" || resourceID == "" {
		return apperror.ErrBadRequest.WithMessage("type and resourceId parameters are required")
	}

	if err := h.svc.DeleteByResource(c.Request().Context(), user.ID, resourceType, resourceID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}
