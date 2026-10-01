package notifications_test

import (
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func TestRegisterRoutes(t *testing.T) {
	e := echo.New()
	h := notifications.NewHandler(&notifications.Service{})
	notifications.RegisterRoutes(e, h, &auth.Middleware{})

	got := map[string]bool{}
	for _, r := range e.Routes() {
		got[r.Method+" "+r.Path] = true
	}

	for _, want := range []string{
		"GET /api/notifications/stats",
		"GET /api/notifications/counts",
		"GET /api/notifications",
		"PATCH /api/notifications/:id/read",
		"POST /api/notifications/:id/unread",
		"DELETE /api/notifications/:id/dismiss",
		"POST /api/notifications/:id/snooze",
		"POST /api/notifications/:id/unsnooze",
		"POST /api/notifications/:id/clear",
		"POST /api/notifications/:id/restore",
		"POST /api/notifications/:id/resolve",
		"POST /api/notifications/mark-all-read",
		"GET /api/notifications/preferences",
		"PUT /api/notifications/preferences",
	} {
		if !got[want] {
			t.Fatalf("route %q not registered; have %v", want, got)
		}
	}
}
