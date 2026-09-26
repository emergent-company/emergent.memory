package devtools_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/devtools"
	"github.com/emergent-company/emergent.memory/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func routeSet(e *echo.Echo) map[string]bool {
	set := map[string]bool{}
	for _, r := range e.Routes() {
		set[r.Method+" "+r.Path] = true
	}
	return set
}

func TestRegisterRoutesDebug(t *testing.T) {
	e := echo.New()
	cfg := &config.Config{Debug: true}
	h := devtools.NewHandler(discardLogger(), cfg)
	devtools.RegisterRoutes(e, h, cfg, discardLogger())

	got := routeSet(e)
	for _, want := range []string{
		"GET /docs",
		"GET /docs/",
		"GET /docs/*",
		"GET /openapi.json",
		"GET /coverage",
		"GET /coverage/*",
	} {
		if !got[want] {
			t.Fatalf("route %q not registered in debug mode; have %v", want, got)
		}
	}
}

func TestRegisterRoutesNoDebugOmitsCoverage(t *testing.T) {
	e := echo.New()
	cfg := &config.Config{}
	h := devtools.NewHandler(discardLogger(), cfg)
	devtools.RegisterRoutes(e, h, cfg, discardLogger())

	got := routeSet(e)
	for _, want := range []string{"GET /docs", "GET /openapi.json"} {
		if !got[want] {
			t.Fatalf("route %q not registered; have %v", want, got)
		}
	}
	if got["GET /coverage"] {
		t.Fatal("coverage routes must not be registered in non-debug mode")
	}
}

func TestServeOpenAPISpecMinimal(t *testing.T) {
	e := echo.New()
	h := devtools.NewHandler(discardLogger(), &config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.ServeOpenAPISpec(c); err != nil {
		t.Fatalf("ServeOpenAPISpec returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var spec map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("spec is not valid JSON: %v", err)
	}
	if spec["openapi"] != "3.0.3" {
		t.Fatalf("openapi = %v, want 3.0.3", spec["openapi"])
	}
}

func TestServeDocsIndex(t *testing.T) {
	e := echo.New()
	h := devtools.NewHandler(discardLogger(), &config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.ServeDocsIndex(c); err != nil {
		t.Fatalf("ServeDocsIndex returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestServeCoverageNotFound(t *testing.T) {
	e := echo.New()
	h := devtools.NewHandler(discardLogger(), &config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/coverage", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.ServeCoverage(c); err != nil {
		t.Fatalf("ServeCoverage returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (not-generated help page)", rec.Code)
	}
}
