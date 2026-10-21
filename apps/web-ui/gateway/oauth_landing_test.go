package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// landingSessionCookie signs claims into a memory_session cookie for a landing
// request, so activateProjectInSession (which reads the cookie, not the request
// context) has a session to re-issue.
func landingSessionCookie(t *testing.T, secret string, claims sessionClaims) *http.Cookie {
	t.Helper()
	val, err := issueSession(secret, claims, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: val, Path: "/"}
}

// TestOAuthLandingScopeProjectSwitchesSession verifies a landing whose
// scope_project hint resolves to a project other than the session's active one
// re-issues the session cookie with that project active (and its owning org), so
// the scope survives the request and the redirect is scoped to it.
func TestOAuthLandingScopeProjectSwitchesSession(t *testing.T) {
	f := &fakeMemory{orgsAndProjects: oauthLandingTestTree()}
	cfg := Config{DefaultAgent: "memory", SessionSecret: "test-secret"}
	srv := newOAuthLandingServerWithConfig(f, cfg)

	claims := sessionClaims{
		AccessToken: "at", RefreshToken: "rt",
		ActiveProjectID: "p2", OrgID: "o2",
		Sub:       "u1",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	// Session's active project is p2 (org o2); the hint targets p1 (org o1).
	rec := landingGet(t, srv, oauthLandingDataSourcesPath+"?connected=s1&scope_project=p1",
		&sessionContext{Sub: "u1", ProjectID: "p2", OrgID: "o2"},
		landingSessionCookie(t, "test-secret", claims))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/o/o1/p/p1/data-sources?connected=s1" {
		t.Errorf("location = %q, want /o/o1/p/p1/data-sources?connected=s1", loc)
	}

	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not re-issued")
	}
	got, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveProjectID != "p1" {
		t.Errorf("ActiveProjectID = %q, want p1 (session must switch to the landed project)", got.ActiveProjectID)
	}
	if got.OrgID != "o1" {
		t.Errorf("OrgID = %q, want o1 (the landed project's owning org)", got.OrgID)
	}
	if got.AccessToken != "at" || got.RefreshToken != "rt" {
		t.Errorf("tokens not preserved: %+v", got)
	}
}

// TestOAuthLandingInaccessibleHintFallsBackNoEscalation verifies an
// inaccessible/stale scope_project hint is skipped and the landing falls back to
// the session's active project without switching the session to the hinted (but
// forbidden) project — the hinted project never gains the active slot.
func TestOAuthLandingInaccessibleHintFallsBackNoEscalation(t *testing.T) {
	f := &fakeMemory{orgsAndProjects: oauthLandingTestTree()}
	cfg := Config{DefaultAgent: "memory", SessionSecret: "test-secret"}
	srv := newOAuthLandingServerWithConfig(f, cfg)

	claims := sessionClaims{
		AccessToken: "at", RefreshToken: "rt",
		ActiveProjectID: "p2", OrgID: "o2",
		Sub:       "u1",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	rec := landingGet(t, srv, oauthLandingDataSourcesPath+"?connected=s1&scope_project=p-forbidden",
		&sessionContext{Sub: "u1", ProjectID: "p2", OrgID: "o2"},
		landingSessionCookie(t, "test-secret", claims))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/o/o2/p/p2/data-sources?connected=s1" {
		t.Errorf("location = %q, want fallback /o/o2/p/p2/data-sources?connected=s1", loc)
	}

	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not re-issued")
	}
	got, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveProjectID != "p2" {
		t.Errorf("ActiveProjectID = %q, want p2 (inaccessible hint must not escalate)", got.ActiveProjectID)
	}
}

// TestOAuthLandingFollowUpMutationResolvesLandedProject exercises the full
// session-mode middleware: a landing into p1 (while the session is active in p2)
// re-issues the cookie, and an unscoped follow-up mutation carrying that cookie
// resolves to p1 — not the previously active p2 — so the just-connected source
// is found rather than 404ing.
func TestOAuthLandingFollowUpMutationResolvesLandedProject(t *testing.T) {
	f := &fakeMemory{orgsAndProjects: oauthLandingTestTree()}
	cfg := Config{AuthMode: "session", SessionSecret: "test-secret", DefaultAgent: "memory"}
	s := &Server{cfg: cfg, memory: f}
	e := echo.New()
	e.Use(s.authDispatch)
	e.Use(s.applyScope)
	e.Use(s.resolveClientScope)
	e.GET(oauthLandingDataSourcesPath, s.uiOAuthLandingDataSources)
	// The unscoped resource-step save (datasources_oauth.templ) resolves the
	// project from the session context; report it so the test can assert it.
	e.POST("/data-sources/:id/resources", func(c echo.Context) error {
		sc, _ := sessionContextFrom(c.Request().Context())
		return c.String(http.StatusOK, sc.OrgID+"/"+sc.ProjectID)
	})

	claims := sessionClaims{
		AccessToken: "at", RefreshToken: "rt",
		ActiveProjectID: "p2", OrgID: "o2",
		Sub:       "u1",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}

	// 1. Landing into p1, active project p2.
	req := httptest.NewRequest(http.MethodGet, oauthLandingDataSourcesPath+"?connected=s1&scope_project=p1", nil)
	addSessionCookie(t, req, "test-secret", claims)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("landing status = %d (%s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/o/o1/p/p1/data-sources?connected=s1" {
		t.Fatalf("landing location = %q", loc)
	}
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("landing did not re-issue the session cookie")
	}

	// 2. Unscoped follow-up mutation with the re-issued cookie.
	req2 := httptest.NewRequest(http.MethodPost, "/data-sources/s1/resources", strings.NewReader("resource_id=f1"))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(ck)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("follow-up status = %d (%s)", rec2.Code, rec2.Body.String())
	}
	if got := rec2.Body.String(); got != "o1/p1" {
		t.Fatalf("follow-up resolved %q, want o1/p1 (the landed project, not p2)", got)
	}
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
