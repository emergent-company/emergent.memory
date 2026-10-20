package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// oauthLandingTestTree is the access tree used by the landing tests: o1 holds
// p1, o2 holds p2.
func oauthLandingTestTree() []OrgWithProjectsDto {
	return []OrgWithProjectsDto{
		{ID: "o1", Name: "Org One", Role: "org_admin", Projects: []ProjectAccessDto{{ID: "p1", Name: "One", OrgID: "o1", Role: "project_admin"}}},
		{ID: "o2", Name: "Org Two", Role: "org_member", Projects: []ProjectAccessDto{{ID: "p2", Name: "Two", OrgID: "o2", Role: "project_user"}}},
	}
}

func newOAuthLandingServer(f *fakeMemory) *echo.Echo {
	return newOAuthLandingServerWithConfig(f, Config{DefaultAgent: "memory"})
}

func newOAuthLandingServerWithConfig(f *fakeMemory, cfg Config) *echo.Echo {
	s := &Server{cfg: cfg, memory: f}
	e := echo.New()
	e.GET(oauthLandingDataSourcesPath, s.uiOAuthLandingDataSources)
	e.GET(oauthLandingMCPServersPath, s.uiOAuthLandingMCPServers)
	return e
}

func landingGet(t *testing.T, e *echo.Echo, path string, sess *sessionContext, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if sess != nil {
		req = req.WithContext(withSessionContext(req.Context(), sess))
	}
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// TestOAuthLandingDataSources_ResolvesScope verifies a callback lands in the
// scope the API carried, falls back to the caller's active project when the hint
// is unusable, and never 404s.
func TestOAuthLandingDataSources_ResolvesScope(t *testing.T) {
	f := &fakeMemory{orgsAndProjects: oauthLandingTestTree()}
	srv := newOAuthLandingServer(f)
	sess := &sessionContext{Sub: "u1", ProjectID: "p2", OrgID: "o2"}

	t.Run("valid hint wins", func(t *testing.T) {
		rec := landingGet(t, srv, oauthLandingDataSourcesPath+"?oauth_error=connect_failed&scope_project=p1", sess)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d", rec.Code)
		}
		want := "/o/o1/p/p1/data-sources?oauth_error=connect_failed"
		if loc := rec.Header().Get("Location"); loc != want {
			t.Errorf("location = %q, want %q", loc, want)
		}
	})

	t.Run("inaccessible hint falls back to the session project", func(t *testing.T) {
		rec := landingGet(t, srv, oauthLandingDataSourcesPath+"?connected=s1&scope_project=p-forbidden", sess)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d", rec.Code)
		}
		want := "/o/o2/p/p2/data-sources?connected=s1"
		if loc := rec.Header().Get("Location"); loc != want {
			t.Errorf("location = %q, want %q", loc, want)
		}
	})

	t.Run("no hint and no session project lands on the root", func(t *testing.T) {
		rec := landingGet(t, srv, oauthLandingDataSourcesPath+"?connected=s1", &sessionContext{Sub: "u1"})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/" {
			t.Errorf("location = %q, want /", loc)
		}
	})

	t.Run("unexpected query params are dropped", func(t *testing.T) {
		rec := landingGet(t, srv, oauthLandingDataSourcesPath+"?connected=s1&next=https://evil.test&scope_project=p1", sess)
		loc := rec.Header().Get("Location")
		if strings.Contains(loc, "evil.test") || strings.Contains(loc, "next=") {
			t.Errorf("location leaked a passthrough param: %q", loc)
		}
	})

	t.Run("access-tree failure still lands, never 404", func(t *testing.T) {
		bad := &fakeMemory{orgsAndProjectsErr: errors.New("upstream down")}
		badSrv := newOAuthLandingServer(bad)
		rec := landingGet(t, badSrv, oauthLandingDataSourcesPath+"?connected=s1&scope_project=p1", sess)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/" {
			t.Errorf("location = %q, want /", loc)
		}
	})
}

// TestOAuthLandingMCPServers_ResolvesScope verifies the MCP callback maps onto
// the scoped settings page and preserves ?mcp_oauth.
func TestOAuthLandingMCPServers_ResolvesScope(t *testing.T) {
	f := &fakeMemory{orgsAndProjects: oauthLandingTestTree()}
	srv := newOAuthLandingServer(f)

	rec := landingGet(t, srv, oauthLandingMCPServersPath+"?mcp_oauth=connected&scope_project=p1", &sessionContext{Sub: "u1", ProjectID: "p2"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	want := "/o/o1/p/p1" + mcpServerSettingsBase + "?mcp_oauth=connected"
	if loc := rec.Header().Get("Location"); loc != want {
		t.Errorf("location = %q, want %q", loc, want)
	}
}

// TestOAuthLandingFallbacks covers the non-hint resolutions: the caller's
// recent-project list (stale entries skipped) and the configured static
// project, which is used whenever it is set.
func TestOAuthLandingFallbacks(t *testing.T) {
	tree := &fakeMemory{orgsAndProjects: oauthLandingTestTree()}

	t.Run("recent project, skipping a stale entry", func(t *testing.T) {
		cfg := Config{DefaultAgent: "memory", SessionSecret: "landing-test-secret"}
		srv := newOAuthLandingServerWithConfig(tree, cfg)
		value, err := issueRecentProjects(cfg.SessionSecret, recentProjectsClaims{
			Recent: map[string][]string{"u1": {"p-gone", "p1"}},
		})
		if err != nil {
			t.Fatalf("issueRecentProjects() error = %v", err)
		}
		rec := landingGet(t, srv, oauthLandingDataSourcesPath+"?connected=s1",
			&sessionContext{Sub: "u1"}, &http.Cookie{Name: recentProjectsCookieName, Value: value})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d", rec.Code)
		}
		want := "/o/o1/p/p1/data-sources?connected=s1"
		if loc := rec.Header().Get("Location"); loc != want {
			t.Errorf("location = %q, want %q", loc, want)
		}
	})

	t.Run("static project (no session)", func(t *testing.T) {
		srv := newOAuthLandingServerWithConfig(tree, Config{DefaultAgent: "memory", MemoryProjectID: "p2"})
		rec := landingGet(t, srv, oauthLandingDataSourcesPath+"?connected=s1", nil)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d", rec.Code)
		}
		want := "/o/o2/p/p2/data-sources?connected=s1"
		if loc := rec.Header().Get("Location"); loc != want {
			t.Errorf("location = %q, want %q", loc, want)
		}
	})
}

// TestOAuthLandingIsGlobal verifies the landing routes are exempt from the
// scoped-URL hard cut and never auto-select a project.
func TestOAuthLandingIsGlobal(t *testing.T) {
	for _, p := range []string{oauthLandingDataSourcesPath, oauthLandingMCPServersPath} {
		if !globalScopePath(p) {
			t.Errorf("globalScopePath(%q) = false, want true", p)
		}
		if strictScopedPath(http.MethodGet, p) {
			t.Errorf("strictScopedPath(GET %q) = true, want false (landing must not 404)", p)
		}
		if projectScopePath(p) {
			t.Errorf("projectScopePath(%q) = true, want false (no auto-select)", p)
		}
		if got := scopedPathFor("/o/o1/p/p1", p); got != p {
			t.Errorf("scopedPathFor(base, %q) = %q, want global passthrough", p, got)
		}
	}
}
