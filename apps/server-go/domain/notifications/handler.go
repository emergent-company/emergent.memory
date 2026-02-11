package notifications

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for notifications
type Handler struct {
	svc *Service
}

// NewHandler creates a new notifications handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// GetStats handles GET /api/notifications/stats
// @Summary      Get notification statistics
// @Description  Returns aggregated statistics for the current user's notifications (unread count, dismissed count, total count)
// @Tags         notifications
// @Produce      json
// @Success      200 {object} NotificationStats "Notification statistics"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/notifications/stats [get]
// @Security     bearerAuth
func (h *Handler) GetStats(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	stats, err := h.svc.GetStats(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	// Stats returns directly (not wrapped in data)
	return c.JSON(http.StatusOK, stats)
}

// GetCounts handles GET /api/notifications/counts
// @Summary      Get notification counts by tab
// @Description  Returns notification counts grouped by tab (all, important, other, snoozed, cleared) for filtering purposes
// @Tags         notifications
// @Produce      json
// @Success      200 {object} NotificationCountsResponse "Counts by notification tab"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/notifications/counts [get]
// @Security     bearerAuth
func (h *Handler) GetCounts(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	counts, err := h.svc.GetCounts(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, NotificationCountsResponse{Data: *counts})
}

// List handles GET /api/notifications
// @Summary      List notifications
// @Description  Returns filtered list of notifications for the current user with optional tab, category, unread, and search filters
// @Tags         notifications
// @Produce      json
// @Param        tab query string false "Tab filter (all, important, other, snoozed, cleared)" Enums(all, important, other, snoozed, cleared)
// @Param        category query string false "Filter by notification category"
// @Param        unread_only query boolean false "Show only unread notifications"
// @Param        search query string false "Search notifications by title or message"
// @Success      200 {object} NotificationListResponse "Filtered notification list"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/notifications [get]
// @Security     bearerAuth
func (h *Handler) List(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	// Parse query parameters
	params := ListParams{
		Tab:        NotificationTab(c.QueryParam("tab")),
		Category:   c.QueryParam("category"),
		UnreadOnly: c.QueryParam("unread_only") == "true",
		Search:     c.QueryParam("search"),
	}

	// Default to "all" tab if not specified
	if params.Tab == "" {
		params.Tab = TabAll
	}

	notifications, err := h.svc.List(c.Request().Context(), user.ID, params)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, NotificationListResponse{Data: notifications})
}

// MarkRead handles PATCH /api/notifications/:id/read
// @Summary      Mark notification as read
// @Description  Marks a specific notification as read for the current user, updating read timestamp
// @Tags         notifications
// @Produce      json
// @Param        id path string true "Notification ID (UUID)"
// @Success      200 {object} map[string]string "Read confirmation"
// @Failure      400 {object} apperror.Error "Missing notification id"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Notification not found"
// @Router       /api/notifications/{id}/read [patch]
// @Security     bearerAuth
func (h *Handler) MarkRead(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	notificationID := c.Param("id")
	if notificationID == "" {
		return apperror.ErrBadRequest.WithMessage("notification id is required")
	}

	if err := h.svc.MarkRead(c.Request().Context(), user.ID, notificationID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "read"})
}

// Dismiss handles DELETE /api/notifications/:id/dismiss
// @Summary      Dismiss notification
// @Description  Dismisses a notification, hiding it from the notification list (marks as dismissed with timestamp)
// @Tags         notifications
// @Produce      json
// @Param        id path string true "Notification ID (UUID)"
// @Success      200 {object} map[string]string "Dismiss confirmation"
// @Failure      400 {object} apperror.Error "Missing notification id"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Notification not found"
// @Router       /api/notifications/{id}/dismiss [delete]
// @Security     bearerAuth
func (h *Handler) Dismiss(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	notificationID := c.Param("id")
	if notificationID == "" {
		return apperror.ErrBadRequest.WithMessage("notification id is required")
	}

	if err := h.svc.Dismiss(c.Request().Context(), user.ID, notificationID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "dismissed"})
}

// MarkAllRead handles POST /api/notifications/mark-all-read
// @Summary      Mark all notifications as read
// @Description  Marks all unread notifications as read for the current user and returns the count of affected notifications
// @Tags         notifications
// @Produce      json
// @Success      200 {object} map[string]interface{} "Confirmation with count of marked notifications"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/notifications/mark-all-read [post]
// @Security     bearerAuth
func (h *Handler) MarkAllRead(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	count, err := h.svc.MarkAllRead(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status": "marked_all_read",
		"count":  count,
	})
}
