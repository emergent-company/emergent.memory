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
		"DELETE /api/notifications/:id/dismiss",
		"POST /api/notifications/mark-all-read",
	} {
		if !got[want] {
			t.Fatalf("route %q not registered; have %v", want, got)
		}
	}
}
