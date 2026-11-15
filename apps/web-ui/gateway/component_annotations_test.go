package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emergent-company/go-daisy/devmode"
	"github.com/labstack/echo/v4"
)

// TestComponentAnnotationsEnablesDevModeRatchet guards the always-on go-daisy
// annotation middleware: every request context must be dev-mode flagged so
// components emit data-component for the element-level feedback overlay.
//
// The `Ratchet` suffix is load-bearing: the required `ci` job runs
// `go test -run Ratchet ./...` for this module (ci.yml, "Consistency ratchets"),
// and the full suite only at promotion. Without it this test would first run on
// the promotion PR that would have caught a broken switch weeks late.
func TestComponentAnnotationsEnablesDevModeRatchet(t *testing.T) {
	e := echo.New()
	e.Use(componentAnnotations())

	var enabled bool
	e.GET("/", func(c echo.Context) error {
		enabled = devmode.IsDevMode(c.Request().Context())
		return c.String(http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !enabled {
		t.Fatal("componentAnnotations did not enable go-daisy dev mode in the request context")
	}
}
