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