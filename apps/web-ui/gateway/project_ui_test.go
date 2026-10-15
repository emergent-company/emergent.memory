package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// TestUICreateProject pins the PRG redirect targets of the project-switcher
// create form. Its form POSTs to the global POST /projects route (scoped
// surfaces are GET-only here), so scopeLocation no-ops and every redirect must
// land on a scoped URL or a global page — never the now-cut unscoped /agents.
func TestUICreateProject(t *testing.T) {
	newServer := func() *echo.Echo {
		s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
		e := echo.New()
		e.POST("/projects", s.uiCreateProject)
		return e
	}
	post := func(e *echo.Echo, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/projects", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.ServeHTTP(rec, req)
		return rec
	}

	t.Run("missing name and org falls back to org list", func(t *testing.T) {
		rec := post(newServer(), "")
		if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/orgs?err=") {
			t.Fatalf("empty create = %d %q, want 303 /orgs?err=…", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("missing name with org returns to scoped org landing", func(t *testing.T) {
		rec := post(newServer(), "orgId=o1")
		if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/o/o1?err=") {
			t.Fatalf("no-name create = %d %q, want 303 /o/o1?err=…", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("success lands on scoped agents", func(t *testing.T) {
		rec := post(newServer(), "name=Home&orgId=o1")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/o/o1/p/proj-1/agents" {
			t.Fatalf("create = %d %q, want 303 /o/o1/p/proj-1/agents", rec.Code, rec.Header().Get("Location"))
		}
	})
}

// TestUIActivateProjectEmptyProject pins that a project switch with no
// projectId lands on the global org list rather than the now-cut /agents.
func TestUIActivateProjectEmptyProject(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()
	e.POST("/projects/activate", s.uiActivateProject)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/projects/activate", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/orgs?err=") {
		t.Fatalf("empty activate = %d %q, want 303 /orgs?err=…", rec.Code, rec.Header().Get("Location"))
	}
}
