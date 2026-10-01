package notifications

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Handler handles HTTP requests for notifications
type Handler struct {
	svc *Service
}

// NewHandler creates a new notifications handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// parseScope reads the scope query param, returning "" when absent or a 400
// apperror when the value is not a recognised scope.
func parseScope(c echo.Context) (Scope, error) {
	scope := Scope(c.QueryParam("scope"))
	if scope == "" {
		return "", nil
	}
	if !scope.Valid() {
		return "", apperror.NewBadRequest("invalid scope: must be 'account' or 'project'")
	}
	return scope, nil
}

// parseProjectID reads the optional project_id query param.
func parseProjectID(c echo.Context) *string {
	if pid := c.QueryParam("project_id"); pid != "" {
		return &pid
	}
	return nil
}

// parseRequiresAction reads the optional requires_action query param.
func parseRequiresAction(c echo.Context) bool {
	return c.QueryParam("requires_action") == "true"
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
	user := auth.MustGetUser(c)

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
// @Param        scope query string false "Scope filter (account, project)" Enums(account, project)
// @Param        project_id query string false "Project ID filter (project scope)"
// @Param        requires_action query boolean false "Only action-required notifications"
// @Success      200 {object} NotificationCountsResponse "Counts by notification tab"
// @Failure      400 {object} apperror.Error "Invalid scope"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/notifications/counts [get]
// @Security     bearerAuth
func (h *Handler) GetCounts(c echo.Context) error {
	user := auth.MustGetUser(c)

	scope, err := parseScope(c)
	if err != nil {
		return err
	}

	params := CountParams{
		Scope:          scope,
		ProjectID:      parseProjectID(c),
		RequiresAction: parseRequiresAction(c),
	}

	counts, err := h.svc.GetCounts(c.Request().Context(), user.ID, params)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, NotificationCountsResponse{Data: *counts})
}

// List handles GET /api/notifications
// @Summary      List notifications
// @Description  Returns filtered list of notifications for the current user with optional tab, category, unread, scope, project, and search filters
// @Tags         notifications
// @Produce      json
// @Param        tab query string false "Tab filter (all, important, other, snoozed, cleared)" Enums(all, important, other, snoozed, cleared)
// @Param        category query string false "Filter by notification category"
// @Param        unread_only query boolean false "Show only unread notifications"
// @Param        scope query string false "Scope filter (account, project)" Enums(account, project)
// @Param        project_id query string false "Project ID filter (project scope)"
// @Param        requires_action query boolean false "Only action-required notifications"
// @Param        search query string false "Search notifications by title or message"
// @Success      200 {object} NotificationListResponse "Filtered notification list"
// @Failure      400 {object} apperror.Error "Invalid scope"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/notifications [get]
// @Security     bearerAuth
func (h *Handler) List(c echo.Context) error {
	user := auth.MustGetUser(c)

	scope, err := parseScope(c)
	if err != nil {
		return err
	}

	// Parse query parameters
	params := ListParams{
		Tab:            NotificationTab(c.QueryParam("tab")),
		Category:       c.QueryParam("category"),
		UnreadOnly:     c.QueryParam("unread_only") == "true",
		Search:         c.QueryParam("search"),
		Scope:          scope,
		ProjectID:      parseProjectID(c),
		RequiresAction: parseRequiresAction(c),
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
	user := auth.MustGetUser(c)

	notificationID := c.Param("id")
	if notificationID == "" {
		return apperror.NewBadRequest("notification id is required")
	}

	if err := h.svc.MarkRead(c.Request().Context(), user.ID, notificationID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "read"})
}

// MarkUnread handles POST /api/notifications/:id/unread
// @Summary      Mark notification as unread
// @Description  Marks a specific notification as unread for the current user
// @Tags         notifications
// @Produce      json
// @Param        id path string true "Notification ID (UUID)"
// @Success      200 {object} map[string]string "Unread confirmation"
// @Failure      400 {object} apperror.Error "Missing notification id"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Notification not found"
// @Router       /api/notifications/{id}/unread [post]
// @Security     bearerAuth
func (h *Handler) MarkUnread(c echo.Context) error {
	user := auth.MustGetUser(c)

	notificationID := c.Param("id")
	if notificationID == "" {
		return apperror.NewBadRequest("notification id is required")
	}

	if err := h.svc.MarkUnread(c.Request().Context(), user.ID, notificationID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "unread"})
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
	user := auth.MustGetUser(c)

	notificationID := c.Param("id")
	if notificationID == "" {
		return apperror.NewBadRequest("notification id is required")
	}

	if err := h.svc.Dismiss(c.Request().Context(), user.ID, notificationID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "dismissed"})
}

// Snooze handles POST /api/notifications/:id/snooze
// @Summary      Snooze notification
// @Description  Snoozes a notification until a time (until= RFC3339 or duration= Go duration; defaults to 24h)
// @Tags         notifications
// @Produce      json
// @Param        id path string true "Notification ID (UUID)"
// @Param        until query string false "RFC3339 timestamp to snooze until"
// @Param        duration query string false "Duration to snooze (e.g. 24h, 1h30m)"
// @Success      200 {object} map[string]string "Snooze confirmation"
// @Failure      400 {object} apperror.Error "Missing notification id or invalid until/duration"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Notification not found"
// @Router       /api/notifications/{id}/snooze [post]
// @Security     bearerAuth
func (h *Handler) Snooze(c echo.Context) error {
	user := auth.MustGetUser(c)

	notificationID := c.Param("id")
	if notificationID == "" {
		return apperror.NewBadRequest("notification id is required")
	}

	// until may arrive three ways: query `until` (RFC3339), query `duration`
	// (Go duration), or a JSON body `{"until": "<RFC3339>"}` (the gateway's
	// client contract). Absent all three, default to 24h.
	until := time.Now().Add(24 * time.Hour)
	switch {
	case c.QueryParam("until") != "":
		t, err := time.Parse(time.RFC3339, c.QueryParam("until"))
		if err != nil {
			return apperror.NewBadRequest("invalid until timestamp")
		}
		until = t
	case c.QueryParam("duration") != "":
		dur, err := time.ParseDuration(c.QueryParam("duration"))
		if err != nil {
			return apperror.NewBadRequest("invalid duration")
		}
		until = time.Now().Add(dur)
	default:
		var req snoozeRequest
		if err := c.Bind(&req); err == nil && req.Until != "" {
			t, err := time.Parse(time.RFC3339, req.Until)
			if err != nil {
				return apperror.NewBadRequest("invalid until timestamp")
			}
			until = t
		}
	}

	if err := h.svc.Snooze(c.Request().Context(), user.ID, notificationID, until); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "snoozed"})
}

// Unsnooze handles POST /api/notifications/:id/unsnooze
// @Summary      Unsnooze notification
// @Description  Clears a notification's snooze
// @Tags         notifications
// @Produce      json
// @Param        id path string true "Notification ID (UUID)"
// @Success      200 {object} map[string]string "Unsnooze confirmation"
// @Failure      400 {object} apperror.Error "Missing notification id"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Notification not found"
// @Router       /api/notifications/{id}/unsnooze [post]
// @Security     bearerAuth
func (h *Handler) Unsnooze(c echo.Context) error {
	user := auth.MustGetUser(c)

	notificationID := c.Param("id")
	if notificationID == "" {
		return apperror.NewBadRequest("notification id is required")
	}

	if err := h.svc.Unsnooze(c.Request().Context(), user.ID, notificationID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "unsnoozed"})
}

// Clear handles POST /api/notifications/:id/clear
// @Summary      Clear notification
// @Description  Moves a notification to the cleared tab
// @Tags         notifications
// @Produce      json
// @Param        id path string true "Notification ID (UUID)"
// @Success      200 {object} map[string]string "Clear confirmation"
// @Failure      400 {object} apperror.Error "Missing notification id"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Notification not found"
// @Router       /api/notifications/{id}/clear [post]
// @Security     bearerAuth
func (h *Handler) Clear(c echo.Context) error {
	user := auth.MustGetUser(c)

	notificationID := c.Param("id")
	if notificationID == "" {
		return apperror.NewBadRequest("notification id is required")
	}

	if err := h.svc.Clear(c.Request().Context(), user.ID, notificationID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "cleared"})
}

// Restore handles POST /api/notifications/:id/restore
// @Summary      Restore notification
// @Description  Un-clears a notification
// @Tags         notifications
// @Produce      json
// @Param        id path string true "Notification ID (UUID)"
// @Success      200 {object} map[string]string "Restore confirmation"
// @Failure      400 {object} apperror.Error "Missing notification id"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Notification not found"
// @Router       /api/notifications/{id}/restore [post]
// @Security     bearerAuth
func (h *Handler) Restore(c echo.Context) error {
	user := auth.MustGetUser(c)

	notificationID := c.Param("id")
	if notificationID == "" {
		return apperror.NewBadRequest("notification id is required")
	}

	if err := h.svc.Restore(c.Request().Context(), user.ID, notificationID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "restored"})
}

// resolveRequest is the body for POST /api/notifications/:id/resolve.
type resolveRequest struct {
	Status string `json:"status"`
}

// snoozeRequest is the JSON body for POST /api/notifications/:id/snooze. The
// gateway sends `{"until": "<RFC3339>"}`; the query-param forms remain the
// legacy contract.
type snoozeRequest struct {
	Until string `json:"until"`
}

// Resolve handles POST /api/notifications/:id/resolve
// @Summary      Resolve notification action
// @Description  Records the outcome of an actionable notification
// @Tags         notifications
// @Produce      json
// @Param        id path string true "Notification ID (UUID)"
// @Param        status query string false "Resolution status (e.g. accepted, declined)"
// @Success      200 {object} map[string]string "Resolve confirmation"
// @Failure      400 {object} apperror.Error "Missing notification id or status"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Notification not found"
// @Router       /api/notifications/{id}/resolve [post]
// @Security     bearerAuth
func (h *Handler) Resolve(c echo.Context) error {
	user := auth.MustGetUser(c)

	notificationID := c.Param("id")
	if notificationID == "" {
		return apperror.NewBadRequest("notification id is required")
	}

	status := c.QueryParam("status")
	if status == "" {
		var req resolveRequest
		if err := c.Bind(&req); err == nil {
			status = req.Status
		}
	}
	if status == "" {
		return apperror.NewBadRequest("status is required")
	}

	if err := h.svc.ResolveAction(c.Request().Context(), user.ID, notificationID, status); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "resolved", "action_status": status})
}

// MarkAllRead handles POST /api/notifications/mark-all-read
// @Summary      Mark all notifications as read
// @Description  Marks all unread notifications as read for the current user within the given scope and returns the count of affected notifications
// @Tags         notifications
// @Produce      json
// @Param        scope query string false "Scope filter (account, project)" Enums(account, project)
// @Param        project_id query string false "Project ID filter (project scope)"
// @Success      200 {object} map[string]interface{} "Confirmation with count of marked notifications"
// @Failure      400 {object} apperror.Error "Invalid scope"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/notifications/mark-all-read [post]
// @Security     bearerAuth
func (h *Handler) MarkAllRead(c echo.Context) error {
	user := auth.MustGetUser(c)

	scope, err := parseScope(c)
	if err != nil {
		return err
	}

	count, err := h.svc.MarkAllRead(c.Request().Context(), user.ID, scope, parseProjectID(c))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status": "marked_all_read",
		"count":  count,
	})
}

// GetPreferences handles GET /api/notifications/preferences
// @Summary      List notification preferences
// @Description  Returns the effective preference for every project event key, materialising defaults for keys without a stored preference
// @Tags         notifications
// @Produce      json
// @Param        project_id query string false "Project ID"
// @Success      200 {object} PreferenceListResponse "Effective preferences"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/notifications/preferences [get]
// @Security     bearerAuth
func (h *Handler) GetPreferences(c echo.Context) error {
	user := auth.MustGetUser(c)

	entries, err := h.svc.ListEffectivePreferences(c.Request().Context(), user.ID, parseProjectID(c))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, PreferenceListResponse{Data: entries})
}

// setPreferenceRequest is the body for PUT /api/notifications/preferences.
type setPreferenceRequest struct {
	ProjectID *string `json:"projectId"`
	EventKey  string  `json:"eventKey"`
	Channel   string  `json:"channel"`
	Enabled   bool    `json:"enabled"`
}

// SavePreference handles PUT /api/notifications/preferences
// @Summary      Set a notification preference
// @Description  Upserts a delivery preference for a project event key. Account keys are mandatory and not configurable.
// @Tags         notifications
// @Produce      json
// @Accept       json
// @Param        body body setPreferenceRequest true "Preference to set"
// @Success      200 {object} NotificationPreference "Saved preference"
// @Failure      400 {object} apperror.Error "Invalid request body"
// @Failure      422 {object} apperror.Error "Unknown event key"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/notifications/preferences [put]
// @Security     bearerAuth
func (h *Handler) SavePreference(c echo.Context) error {
	user := auth.MustGetUser(c)

	var req setPreferenceRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if req.EventKey == "" {
		return apperror.NewBadRequest("eventKey is required")
	}

	pref, err := h.svc.SavePreference(c.Request().Context(), user.ID, req.ProjectID, req.EventKey, req.Channel, req.Enabled)
	if err != nil {
		return err
	}

	if pref == nil {
		// Account-scope key: not user-configurable (no-op).
		return c.JSON(http.StatusOK, map[string]string{
			"status": "ok",
			"note":   "account_scope_not_configurable",
		})
	}

	return c.JSON(http.StatusOK, pref)
}
