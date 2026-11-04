package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// orgInboxTestTree is the access tree for the org-scoped inbox tests: o1 holds
// two projects, o2 one, o3 none.
func orgInboxTestTree() []OrgWithProjectsDto {
	return []OrgWithProjectsDto{
		{ID: "o1", Name: "One", Role: "org_admin", Projects: []ProjectAccessDto{
			{ID: "p1", Name: "First", OrgID: "o1"},
			{ID: "p2", Name: "Second", OrgID: "o1"},
		}},
		{ID: "o2", Name: "Two", Role: "org_member", Projects: []ProjectAccessDto{
			{ID: "p3", Name: "Third", OrgID: "o2"},
		}},
		{ID: "o3", Name: "Empty", Role: "org_member"},
	}
}

// newOrgInboxServer wires the scope pipeline the way main.go does, so a request
// to /o/{org}/inbox exercises the same prefix strip, resolver and redirect
// rewrite the browser goes through.
func newOrgInboxServer(f *fakeMemory, sess *sessionContext) *echo.Echo {
	return newOrgInboxServerWithConfig(f, sess, sessionCfg())
}

func newOrgInboxServerWithConfig(f *fakeMemory, sess *sessionContext, cfg Config) *echo.Echo {
	s := &Server{cfg: cfg, memory: f}
	e := echo.New()
	e.Pre(s.scopeRewrite)
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if sess != nil {
				ctx := withSessionContext(c.Request().Context(), sess)
				c.SetRequest(c.Request().WithContext(ctx))
			}
			return next(c)
		}
	})
	e.Use(s.applyScope)
	e.Use(s.scopeLocation)
	e.GET("/orgs/:id/inbox", s.uiOrgInbox)
	return e
}

func orgInboxGet(t *testing.T, e *echo.Echo, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestOrgInboxProject(t *testing.T) {
	tree := orgInboxTestTree()
	tests := []struct {
		name   string
		orgID  string
		recent []string
		want   string
	}{
		{"no recents falls back to the first project", "o1", nil, "p1"},
		{"recent project in the org wins", "o1", []string{"p2"}, "p2"},
		{"recents from another org are skipped", "o1", []string{"p3", "p2"}, "p2"},
		{"stale recent ids are skipped", "o1", []string{"p-gone", "p2"}, "p2"},
		{"org with no projects", "o3", []string{"p1"}, ""},
		{"unknown org", "nope", []string{"p1"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := orgInboxProject(tree, tc.recent, tc.orgID); got != tc.want {
				t.Errorf("orgInboxProject(%q, %v) = %q, want %q", tc.orgID, tc.recent, got, tc.want)
			}
		})
	}
}

// TestOrgInboxRedirectsFromOrgScope is the regression for the topbar bell on an
// org page: /o/{org}/inbox carried no project ref, was rewritten to
// /orgs/{org}/inbox and 404'd because the org route tree has no inbox.
func TestOrgInboxRedirectsFromOrgScope(t *testing.T) {
	srv := newOrgInboxServer(&fakeMemory{orgsAndProjects: orgInboxTestTree()}, &sessionContext{Sub: "u1"})

	rec := orgInboxGet(t, srv, "/o/o1/inbox?tab=action")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/o/o1/p/p1/inbox?tab=action" {
		t.Errorf("location = %q, want /o/o1/p/p1/inbox?tab=action", loc)
	}
}

func TestOrgInboxUsesRecentProjectInOrg(t *testing.T) {
	srv := newOrgInboxServer(&fakeMemory{orgsAndProjects: orgInboxTestTree()}, &sessionContext{Sub: "u1"})

	value, err := issueRecentProjects(sessionCfg().SessionSecret, recentProjectsClaims{
		Recent: map[string][]string{"u1": {"p3", "p2"}},
	})
	if err != nil {
		t.Fatalf("issueRecentProjects() error = %v", err)
	}
	rec := orgInboxGet(t, srv, "/o/o1/inbox", &http.Cookie{Name: recentProjectsCookieName, Value: value})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	// p3 is in another org; p2 is the caller's most recent project in o1.
	if loc := rec.Header().Get("Location"); loc != "/o/o1/p/p2/inbox" {
		t.Errorf("location = %q, want /o/o1/p/p2/inbox", loc)
	}
}

func TestOrgInboxWithoutProjectsLandsOnOrg(t *testing.T) {
	srv := newOrgInboxServer(&fakeMemory{orgsAndProjects: orgInboxTestTree()}, &sessionContext{Sub: "u1"})

	rec := orgInboxGet(t, srv, "/o/o3/inbox")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/o/o3" {
		t.Errorf("location = %q, want /o/o3", loc)
	}
}

func TestOrgInboxUnknownOrgIs404(t *testing.T) {
	srv := newOrgInboxServer(&fakeMemory{orgsAndProjects: orgInboxTestTree()}, &sessionContext{Sub: "u1"})

	rec := orgInboxGet(t, srv, "/o/nope/inbox")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestLegacyOrgInboxRedirects covers an unscoped /orgs/{id}/inbox link, which
// never passes through applyScope and so is validated by the handler itself.
// Session mode hard-cuts unscoped app paths (the URL is the scope authority), so
// the legacy form is exercised in dev/API-key mode, where unscoped navigation is
// still how the harness moves around.
func TestLegacyOrgInboxRedirects(t *testing.T) {
	srv := newOrgInboxServerWithConfig(&fakeMemory{orgsAndProjects: orgInboxTestTree()}, nil, Config{AuthMode: "dev"})

	rec := orgInboxGet(t, srv, "/orgs/o2/inbox")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/o/o2/p/p3/inbox" {
		t.Errorf("location = %q, want /o/o2/p/p3/inbox", loc)
	}
}
