package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// pageErrorLeakTokens are the raw internal details a swept page must never
// render. They cover both failure shapes injected by rawBackendFailures: the
// transport *url.Error (host "172.17.0.1", "dial tcp", port ":8080") and the
// memoryHTTPError rendering ("memory 5", "service_unavailable", "Token
// validation").
var pageErrorLeakTokens = []string{
	"172.17.0.1",
	"dial tcp",
	":8080",
	"memory 5",
	"service_unavailable",
	"Token validation",
}

// rawBackendFailures builds the two raw (unsanitized) backend failure errors
// swept across every page. They are deliberately NOT wrapped with
// userFacingError: each page handler must sanitize them itself, so a page that
// renders err.Error() directly leaks the detail in pageErrorLeakTokens.
func rawBackendFailures() []struct {
	name string
	err  error
} {
	return []struct {
		name string
		err  error
	}{
		{
			name: "transport",
			err: &url.Error{
				Op:  "Get",
				URL: "http://172.17.0.1:8080/api/x",
				Err: errors.New("dial tcp 172.17.0.1:8080: connect: connection refused"),
			},
		},
		{
			name: "service_unavailable",
			err: &memoryHTTPError{
				Status:  http.StatusServiceUnavailable,
				Code:    "service_unavailable",
				Message: "Token validation temporarily unavailable",
			},
		},
	}
}

// sweepPage is one swept page: the fakeMemory error field to inject the
// failure, and a mount that builds an echo instance registering the page's
// handler.
type sweepPage struct {
	name   string
	path   string
	setErr func(f *fakeMemory, err error)
	mount  func(s *Server) *echo.Echo
}

// sweptPages are representative pages from different handler families, each
// with a fakeMemory failure field that makes its list/browse load fail.
var sweptPages = []sweepPage{
	{
		name:   "documents list",
		path:   "/documents",
		setErr: func(f *fakeMemory, err error) { f.docErr = err },
		mount: func(s *Server) *echo.Echo {
			e := echo.New()
			e.GET("/documents", s.uiDocuments)
			return e
		},
	},
	{
		name:   "skills list",
		path:   "/skills",
		setErr: func(f *fakeMemory, err error) { f.skillErr = err },
		mount: func(s *Server) *echo.Echo {
			e := echo.New()
			e.GET("/skills", s.uiSkills)
			return e
		},
	},
	{
		name:   "sessions list",
		path:   "/sessions",
		setErr: func(f *fakeMemory, err error) { f.convErr = err },
		mount: func(s *Server) *echo.Echo {
			e := echo.New()
			e.GET("/sessions", s.uiSessions)
			return e
		},
	},
	{
		name:   "board",
		path:   "/board",
		setErr: func(f *fakeMemory, err error) { f.workItemsErr = err },
		mount: func(s *Server) *echo.Echo {
			e := echo.New()
			e.GET("/board", s.uiBoard)
			return e
		},
	},
	{
		name:   "usage",
		path:   "/usage",
		setErr: func(f *fakeMemory, err error) { f.usageErr = err },
		mount: func(s *Server) *echo.Echo {
			e := echo.New()
			e.GET("/usage", s.uiUsage)
			return e
		},
	},
	{
		name:   "objects browse",
		path:   "/objects",
		setErr: func(f *fakeMemory, err error) { f.pageErr = err },
		mount: func(s *Server) *echo.Echo {
			e := echo.New()
			e.GET("/objects", s.uiObjects)
			return e
		},
	},
}

// TestPageErrorSweepRedactsRawBackendErrors renders representative pages
// through their real handlers with a raw transport failure and a raw 503
// service-unavailable error injected into the fake backend, then asserts the
// rendered body is sanitized (issue #1671): every page must show
// memoryUnavailableMessage and never leak the internal host/port/dial or
// "memory <status> <code>:" detail. A page reverted to rendering err.Error()
// directly fails this test.
func TestPageErrorSweepRedactsRawBackendErrors(t *testing.T) {
	for _, pg := range sweptPages {
		for _, fail := range rawBackendFailures() {
			t.Run(pg.name+"/"+fail.name, func(t *testing.T) {
				f := &fakeMemory{}
				pg.setErr(f, fail.err)
				s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
				e := pg.mount(s)

				req := httptest.NewRequest(http.MethodGet, pg.path, nil).WithContext(t.Context())
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)

				if rec.Code != http.StatusOK {
					t.Fatalf("%s with %s failure = %d, want 200", pg.name, fail.name, rec.Code)
				}
				body := rec.Body.String()
				if !strings.Contains(body, memoryUnavailableMessage) {
					t.Errorf("%s with %s failure missing sanitized message %q, got %q", pg.name, fail.name, memoryUnavailableMessage, body)
				}
				for _, leak := range pageErrorLeakTokens {
					if strings.Contains(body, leak) {
						t.Errorf("%s with %s failure leaked %q into the page", pg.name, fail.name, leak)
					}
				}
			})
		}
	}
}
