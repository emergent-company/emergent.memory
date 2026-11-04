package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func TestBodyLimitSkipper(t *testing.T) {
	tests := []struct {
		path string
		skip bool
	}{
		{"/documents", true},
		{"/api/documents", true},
		{"/api/documents/x/extract", true},
		{"/profile/avatar", true},
		{"/api/agents", false},
		{"/api/chat", false},
		{"/auth/callback", false},
	}
	for _, tc := range tests {
		e := echo.New()
		c := e.NewContext(httptest.NewRequest(http.MethodPost, tc.path, nil), httptest.NewRecorder())
		if got := bodyLimitSkipper(c); got != tc.skip {
			t.Errorf("bodyLimitSkipper(%q) = %v, want %v", tc.path, got, tc.skip)
		}
	}
}

func TestBodyLimitRejectsOversizedBody(t *testing.T) {
	e := echo.New()
	e.Use(middleware.BodyLimitWithConfig(middleware.BodyLimitConfig{Limit: "1K", Skipper: bodyLimitSkipper}))
	e.POST("/api/echo", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	e.POST("/documents", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	big := strings.Repeat("x", 4096)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader(big))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("limited route status = %d, want 413", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/documents", strings.NewReader(big))
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("upload route status = %d, want 200 (exempt)", rec.Code)
	}
}
