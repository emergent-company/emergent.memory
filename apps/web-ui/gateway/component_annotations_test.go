package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emergent-company/go-daisy/devmode"
	"github.com/labstack/echo/v4"
)

// TestComponentAnnotationsEnablesDevMode guards the always-on go-daisy
// annotation middleware: every request context must be dev-mode flagged so
// components emit data-component for the element-level feedback overlay.
func TestComponentAnnotationsEnablesDevMode(t *testing.T) {
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
