package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- notifications JSON API (inbox subsystem, tasks 7.2/9.2) ---
//
// Thin session-scoped proxies over memory's notification API. The browser's
// inbox JS consumes these (list refresh, counts reconcile, inline actions); the
// session bearer never reaches the browser because every call is made
// server-side through MemoryBackend.

// notificationScope normalizes a raw scope query value to account|project,
// defaulting to account (the mandatory inbox).
func notificationScope(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), "project") {
		return "project"
	}
	return "account"
}

// listNotifications proxies GET /api/notifications. The scope/tab/category/
// unread_only/requires_action/search filters map onto NotificationListParams.
// Project scope without an explicit project_id falls back to the session's
// active project so the client cannot accidentally read another project.
func (s *Server) listNotifications(c echo.Context) error {
	scope := notificationScope(c.QueryParam("scope"))
	projectID := c.QueryParam("project_id")
	if scope == "project" && projectID == "" {
		projectID = activeProjectIDFromContext(c.Request().Context(), s.cfg.MemoryProjectID)
	}
	p := NotificationListParams{
		Scope:          scope,
		ProjectID:      projectID,
		Tab:            c.QueryParam("tab"),
		Category:       c.QueryParam("category"),
		UnreadOnly:     c.QueryParam("unread_only") == "true",
		RequiresAction: c.QueryParam("requires_action") == "true",
		Search:         c.QueryParam("search"),
	}
	items, err := s.memory.ListNotifications(c.Request().Context(), p)
	if err != nil {
		return s.notificationUpstreamError(c, err)
	}
	if items == nil {
		items = []Notification{}
	}
	return c.JSON(http.StatusOK, map[string]any{"notifications": items})
}

// notificationCounts proxies GET /api/notifications/counts.
func (s *Server) notificationCounts(c echo.Context) error {
	scope := notificationScope(c.QueryParam("scope"))
	projectID := c.QueryParam("project_id")
	if scope == "project" && projectID == "" {
		projectID = activeProjectIDFromContext(c.Request().Context(), s.cfg.MemoryProjectID)
	}
	counts, err := s.memory.NotificationCounts(c.Request().Context(), scope, projectID)
	if err != nil {
		return s.notificationUpstreamError(c, err)
	}
	return c.JSON(http.StatusOK, counts)
}

// listNotificationPreferences proxies GET /api/notifications/preferences.
func (s *Server) listNotificationPreferences(c echo.Context) error {
	prefs, err := s.memory.ListNotificationPreferences(c.Request().Context())
	if err != nil {
		return s.notificationUpstreamError(c, err)
	}
	if prefs == nil {
		prefs = []NotificationPreference{}
	}
	return c.JSON(http.StatusOK, map[string]any{"preferences": prefs})
}

// setNotificationPreference proxies PUT /api/notifications/preferences.
func (s *Server) setNotificationPreference(c echo.Context) error {
	var in struct {
		EventKey string `json:"eventKey"`
		Channel  string `json:"channel"`
		Enabled  bool   `json:"enabled"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if strings.TrimSpace(in.EventKey) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "eventKey is required"})
	}
	if in.Channel == "" {
		in.Channel = "in_app"
	}
	if err := s.memory.SetNotificationPreference(c.Request().Context(), in.EventKey, in.Channel, in.Enabled); err != nil {
		return s.notificationUpstreamError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

// markNotificationRead proxies PATCH /api/notifications/:id/read.
func (s *Server) markNotificationRead(c echo.Context) error {
	return s.notificationMutation(c, func(ctx context.Context) error {
		return s.memory.MarkNotificationRead(ctx, c.Param("id"))
	})
}

// markNotificationUnread proxies POST /api/notifications/:id/unread.
func (s *Server) markNotificationUnread(c echo.Context) error {
	return s.notificationMutation(c, func(ctx context.Context) error {
		return s.memory.MarkNotificationUnread(ctx, c.Param("id"))
	})
}

// dismissNotification proxies DELETE /api/notifications/:id/dismiss.
func (s *Server) dismissNotification(c echo.Context) error {
	return s.notificationMutation(c, func(ctx context.Context) error {
		return s.memory.DismissNotification(ctx, c.Param("id"))
	})
}

// snoozeNotification proxies POST /api/notifications/:id/snooze.
func (s *Server) snoozeNotification(c echo.Context) error {
	var in struct {
		Until string `json:"until"`
	}
	_ = c.Bind(&in) // body optional: memory applies its default snooze window
	return s.notificationMutation(c, func(ctx context.Context) error {
		return s.memory.SnoozeNotification(ctx, c.Param("id"), in.Until)
	})
}

// unsnoozeNotification proxies POST /api/notifications/:id/unsnooze.
func (s *Server) unsnoozeNotification(c echo.Context) error {
	return s.notificationMutation(c, func(ctx context.Context) error {
		return s.memory.UnsnoozeNotification(ctx, c.Param("id"))
	})
}

// clearNotification proxies POST /api/notifications/:id/clear.
func (s *Server) clearNotification(c echo.Context) error {
	return s.notificationMutation(c, func(ctx context.Context) error {
		return s.memory.ClearNotification(ctx, c.Param("id"))
	})
}

// restoreNotification proxies POST /api/notifications/:id/restore.
func (s *Server) restoreNotification(c echo.Context) error {
	return s.notificationMutation(c, func(ctx context.Context) error {
		return s.memory.RestoreNotification(ctx, c.Param("id"))
	})
}

// resolveNotification proxies POST /api/notifications/:id/resolve. The chosen
// action verb (accept|decline|approve|deny|…) travels in the body.
func (s *Server) resolveNotification(c echo.Context) error {
	var in struct {
		Action string `json:"action"`
	}
	_ = c.Bind(&in)
	return s.notificationMutation(c, func(ctx context.Context) error {
		return s.memory.ResolveNotification(ctx, c.Param("id"), in.Action)
	})
}

// markAllNotificationsRead proxies POST /api/notifications/mark-all-read.
func (s *Server) markAllNotificationsRead(c echo.Context) error {
	scope := notificationScope(c.QueryParam("scope"))
	projectID := c.QueryParam("project_id")
	if scope == "project" && projectID == "" {
		projectID = activeProjectIDFromContext(c.Request().Context(), s.cfg.MemoryProjectID)
	}
	return s.notificationMutation(c, func(ctx context.Context) error {
		return s.memory.MarkAllNotificationsRead(ctx, scope, projectID)
	})
}

// notificationMutation runs one notification write and returns {"ok":true},
// mapping upstream failures through notificationUpstreamError.
func (s *Server) notificationMutation(c echo.Context, fn func(context.Context) error) error {
	if err := fn(c.Request().Context()); err != nil {
		return s.notificationUpstreamError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

// notificationUpstreamError maps a memory failure to a 502 so the client can
// retry/reconcile without a generic 500.
func (s *Server) notificationUpstreamError(c echo.Context, err error) error {
	captureError(err)
	return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
}

// notificationStream proxies the session user's real-time notification stream
// as SSE (task 7.2). Account scope forwards memory's global stream (no
// projectId); project scope forwards that project's stream. The browser only
// sees this gateway path — the bearer is attached server-side.
//
// Frames pass through verbatim (memory sends `event: <entity>` /
// `data: <payload>`); EventSource on the client filters to `notification`.
// Response headers are flushed up front so a proxy cannot buffer the stream,
// and the upstream body is closed either when the client disconnects (request
// context done) or on process shutdown.
func (s *Server) notificationStream(c echo.Context) error {
	ctx := c.Request().Context()
	scope := notificationScope(c.QueryParam("scope"))
	projectID := ""
	if scope == "project" {
		projectID = c.QueryParam("project_id")
		if projectID == "" {
			projectID = activeProjectIDFromContext(ctx, s.cfg.MemoryProjectID)
		}
	}
	body, err := s.memory.NotificationEventStream(ctx, projectID)
	if err != nil {
		return s.notificationUpstreamError(c, err)
	}
	defer func() { _ = body.Close() }()

	w := c.Response()
	w.Header().Set(echo.HeaderContentType, "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	w.Flush()

	if s.shutdownCh != nil {
		go func() {
			select {
			case <-s.shutdownCh:
				_ = body.Close()
			case <-ctx.Done():
			}
		}()
	}

	// Flush after every read: net/http buffers small SSE frames until the 2 KiB
	// threshold, which would strand heartbeats/events behind an idle proxy.
	buf := make([]byte, 4096)
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return nil
			}
			w.Flush()
		}
		if rerr != nil {
			return nil
		}
	}
}
