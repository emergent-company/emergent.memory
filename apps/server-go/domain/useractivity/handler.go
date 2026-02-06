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
