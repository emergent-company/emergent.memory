package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// TestUIRootForeignProjectFallsThroughToOrgLanding is the #1998 regression: a
// project membership without an org membership (ListProjects returns it, but the
// access tree does not) is seeded in the durable recent-projects cookie. GET /
// must not bind the session to that project — it falls through to the caller's
// org landing, writes no project to the session, and does not re-record the
// foreign project in recent-projects.
func TestUIRootForeignProjectFallsThroughToOrgLanding(t *testing.T) {
	f := &fakeMemory{
		// ListProjects returns a project whose org is NOT in the access tree.
		projects: []ProjectRef{{ID: "p-foreign", Name: "Foreign", OrgID: "o-foreign"}},
		orgs:     []Org{{ID: "o-member", Name: "Acme"}},
		orgsAndProjects: []OrgWithProjectsDto{
			{ID: "o-member", Name: "Acme", Projects: []ProjectAccessDto{}},
		},
	}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := orgContextUIServer(s)

	recentVal, err := issueRecentProjects("test-secret", recentProjectsClaims{
		Recent: map[string][]string{"sub-a": {"p-foreign"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
		AccessToken: "at",
		Sub:         "sub-a",
		OrgID:       "o-member",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}))
	req.AddCookie(&http.Cookie{Name: recentProjectsCookieName, Value: recentVal, Path: "/"})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/o/o-member" {
		t.Fatalf("/ = %d %q, want 302 /o/o-member", rec.Code, rec.Header().Get("Location"))
	}
	// No project written to the session: the org-matched path re-issues nothing,
	// but if a cookie is present it must not carry the foreign project.
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		claims, err := verifySession("test-secret", ck.Value, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if claims.ActiveProjectID != "" {
			t.Errorf("ActiveProjectID = %q, want empty (foreign project must not be activated)", claims.ActiveProjectID)
		}
	}
	// No recent-project write for the foreign project.
	if ck := findCookie(rec, recentProjectsCookieName); ck != nil {
		t.Errorf("recent-projects cookie must not be rewritten, got %+v", ck)
	}
}

// TestUIActivateProjectOutOfTreeRefused asserts the HTML project-switcher
// activate form refuses an out-of-tree project: error flash, no session re-issue,
// no recent-project write.
func TestUIActivateProjectOutOfTreeRefused(t *testing.T) {
	f := &fakeMemory{
		orgsAndProjects: []OrgWithProjectsDto{
			{ID: "o1", Name: "Acme", Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1"}}},
		},
	}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := echo.New()
	e.Use(s.authDispatch)
	e.POST("/projects/activate", s.uiActivateProject)

	req := httptest.NewRequest(http.MethodPost, "/projects/activate", strings.NewReader("projectId=p-foreign"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
		AccessToken: "at",
		Sub:         "sub-a",
		OrgID:       "o1",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "/orgs?err=scope+not+permitted") {
		t.Errorf("Location = %q, want /orgs?err=scope+not+permitted", loc)
	}
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		t.Errorf("session cookie must not be re-issued on refusal: %+v", ck)
	}
	if ck := findCookie(rec, recentProjectsCookieName); ck != nil {
		t.Errorf("recent-projects cookie must not be written on refusal: %+v", ck)
	}
}

// TestProjectIDForSessionEmptyProject asserts a session context with no active
// project yields "" rather than the deployment's static project id; only
// session-less (API-key) callers keep the static id (#1998).
func TestProjectIDForSessionEmptyProject(t *testing.T) {
	m := &MemoryClient{projectID: "static-proj"}

	if got := m.projectIDFor(context.Background()); got != "static-proj" {
		t.Errorf("no-session projectIDFor = %q, want static-proj", got)
	}
	if got := m.projectIDFor(withSessionContext(context.Background(), &sessionContext{ProjectID: "sess-proj"})); got != "sess-proj" {
		t.Errorf("session projectIDFor = %q, want sess-proj", got)
	}
	if got := m.projectIDFor(withSessionContext(context.Background(), &sessionContext{})); got != "" {
		t.Errorf("session-with-empty-project projectIDFor = %q, want empty", got)
	}
}

// TestSessionlessTokenScopesToStaticProject asserts a session-less bearer token
// (the GitHub webhook trigger credential) authenticates the request while
// project scope stays the MemoryClient's static project id via the URL — never a
// session project and never an X-Project-ID header, so the server derives the
// project from the token binding (#1998).
func TestSessionlessTokenScopesToStaticProject(t *testing.T) {
	m := &MemoryClient{projectID: "static-proj"}
	ctx := withSessionlessToken(context.Background(), "webhook-token")

	if got := m.tokenFor(ctx); got != "webhook-token" {
		t.Errorf("tokenFor = %q, want webhook-token", got)
	}
	if got := m.projectIDFor(ctx); got != "static-proj" {
		t.Errorf("projectIDFor = %q, want static-proj (session-less static id)", got)
	}
	if h := sessionHeaders(ctx); len(h) != 0 {
		t.Errorf("sessionHeaders = %v, want empty (no X-Project-ID for a session-less token)", h)
	}
}

// TestBlueprintMemoryErrorSurfacesForbidden asserts an upstream 403 is surfaced
// as 403 (not masked as 502), and a genuine transport failure stays 502.
func TestBlueprintMemoryErrorSurfacesForbidden(t *testing.T) {
	t.Run("upstream 403 is 403", func(t *testing.T) {
		f := &fakeMemory{blueprintErr: &memoryHTTPError{Status: http.StatusForbidden, Code: "forbidden", Message: "not a member"}}
		_, e := newBlueprintServer(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/blueprints/installed", nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("transport failure is 502", func(t *testing.T) {
		f := &fakeMemory{blueprintErr: &url.Error{Op: "Get", URL: "http://memory/api/blueprints", Err: context.DeadlineExceeded}}
		_, e := newBlueprintServer(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/blueprints/installed", nil))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502 (%s)", rec.Code, rec.Body.String())
		}
	})
}
