package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// sidebarScopeCase is one request shape the sidebar's link base must handle on
// a full page load.
type sidebarScopeCase struct {
	name string
	path string
	dev  bool           // true = dev/API-key mode (no session)
	sess *sessionClaims // non-nil in session mode
	// dropProject hides the project from the list picked up by page()/applyScope,
	// so an active session project cannot resolve an org (no-nav fallback).
	dropProject bool
	// wantBase is the prefix every project nav href must carry; "" = unscoped
	// (the legacy dev-mode path).
	wantBase string
	// wantNav false asserts page() rendered no project nav at all.
	wantNav bool
}

// sidebarScopeTree is the access tree used by the sidebar render cases.
func sidebarScopeTree() []OrgWithProjectsDto {
	return []OrgWithProjectsDto{{
		ID: "o1", Name: "Acme", Role: "org_admin",
		Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1", Role: "project_admin"}},
	}}
}

// newSidebarScopeServer wires the scope pipeline the way main.go does, with the
// project page and the global account page the table exercises.
func newSidebarScopeServer(t *testing.T, tc sidebarScopeCase) *echo.Echo {
	t.Helper()
	cfg := Config{MemoryProjectID: "memory"}
	if !tc.dev {
		cfg = Config{AuthMode: "session", SessionSecret: "test-secret"}
	}
	f := &fakeMemory{
		orgs:            []Org{{ID: "o1", Name: "Acme"}},
		orgsAndProjects: sidebarScopeTree(),
		projects:        []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o1"}},
	}
	if tc.dropProject {
		f.projects = nil
		f.orgsAndProjects = nil
	}
	s := &Server{cfg: cfg, memory: f}
	e := echo.New()
	e.Pre(s.scopeRewrite)
	e.Use(s.authDispatch)
	e.Use(s.applyScope)
	e.Use(s.scopeLocation)
	e.GET("/agents", s.uiAgents)
	e.GET("/profile", s.uiProfile)
	return e
}

// sidebarAnchor returns the <a …></a> block whose href is exactly href, or "".
func sidebarAnchor(html, href string) string {
	for _, chunk := range strings.Split(html, "</a>") {
		if strings.Contains(chunk, `href="`+href+`"`) {
			return chunk
		}
	}
	return ""
}

// TestSidebarScopedHrefsAndProviderWarn is the table regression for #37 and
// #184: whatever link base the page render gets (unscoped dev, a project scope,
// or a global account page that carries the session's active project), every
// project nav href must match that base AND the no-provider warning must bind
// to the Settings hub only — never to Work Settings or a settings sub-page.
func TestSidebarScopedHrefsAndProviderWarn(t *testing.T) {
	projSess := &sessionClaims{
		AccessToken:     "at",
		ActiveProjectID: "p1",
		OrgID:           "o1",
		ExpiresAt:       time.Now().Add(time.Hour).Unix(),
	}
	// Same active project, but no session org and no project list entry: no base
	// can be built, so the render must drop the project nav rather than emit
	// links the session-mode hard cut 404s.
	orphanSess := &sessionClaims{
		AccessToken:     "at",
		ActiveProjectID: "p1",
		ExpiresAt:       time.Now().Add(time.Hour).Unix(),
	}

	cases := []sidebarScopeCase{
		{name: "unscoped dev page", path: "/agents", dev: true, wantBase: "", wantNav: true},
		{name: "scoped project page", path: "/o/o1/p/p1/agents", sess: projSess, wantBase: "/o/o1/p/p1", wantNav: true},
		{name: "global account page with active project", path: "/profile", sess: projSess, wantBase: "/o/o1/p/p1", wantNav: true},
		{name: "global page without a resolvable scope renders no nav", path: "/profile", sess: orphanSess, dropProject: true, wantNav: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSidebarScopeServer(t, tc)
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.sess != nil {
				addSessionCookie(t, req, "test-secret", *tc.sess)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200 (body: %.400s)", tc.path, rec.Code, rec.Body.String())
			}
			body := rec.Body.String()

			if !tc.wantNav {
				if strings.Contains(body, `id="_layout-sidebar"`) {
					t.Error("render emitted a project sidebar with no scope base (its hrefs would 404)")
				}
				if strings.Contains(body, `href="/objects"`) {
					t.Error("render emitted an unscoped project href")
				}
				return
			}

			// (a) every project nav entry carries the page's link base.
			for _, entry := range []string{"/objects", "/chat", "/settings", "/settings/approvals", "/work/board"} {
				if want := `href="` + tc.wantBase + entry + `"`; !strings.Contains(body, want) {
					t.Errorf("sidebar missing %s", want)
				}
			}
			if tc.wantBase != "" {
				if strings.Contains(body, `href="/objects"`) {
					t.Error("sidebar leaked an unscoped href alongside its scoped base")
				}
			}

			// (b) the no-provider warning binds to the Settings hub for this base…
			settings := sidebarAnchor(body, tc.wantBase+"/settings")
			if settings == "" {
				t.Fatalf("settings nav anchor href=%q not rendered", tc.wantBase+"/settings")
			}
			if !strings.Contains(settings, "No provider configured") {
				t.Error("no-provider warning did not attach to the Settings hub")
			}
			// …and only there: suffix-shaped siblings must stay clean.
			for _, sibling := range []string{"/work/settings", "/settings/approvals", "/settings/members"} {
				anchor := sidebarAnchor(body, tc.wantBase+sibling)
				if anchor == "" {
					t.Fatalf("nav anchor href=%q not rendered", tc.wantBase+sibling)
				}
				if strings.Contains(anchor, "No provider configured") {
					t.Errorf("no-provider warning leaked onto %s", sibling)
				}
			}
		})
	}
}
