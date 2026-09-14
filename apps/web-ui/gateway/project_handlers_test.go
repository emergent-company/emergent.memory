package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestCreateProjectReissuesSessionCookie asserts POST /api/projects creates
// the project and re-issues the session cookie with the new project as active
// (preserving the session's tokens).
func TestCreateProjectReissuesSessionCookie(t *testing.T) {
	f := &fakeMemory{orgs: []Org{{ID: "o1", Name: "Acme"}}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"New Proj","orgId":"o1"}`))
	req.Header.Set("Content-Type", "application/json")
	addSessionCookie(t, req, "test-secret", sessionClaims{
		AccessToken: "at-session", RefreshToken: "rt-session",
		ActiveProjectID: "proj-sess", OrgID: "org-sess",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	if len(f.createdProjects) != 1 || f.createdProjects[0].Name != "New Proj" || f.createdProjects[0].OrgID != "o1" {
		t.Fatalf("create not forwarded: %+v", f.createdProjects)
	}
	// session cookie re-issued with the created project active + org set
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not re-issued")
	}
	claims, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims.ActiveProjectID != "proj-1" {
		t.Errorf("ActiveProjectID = %q, want proj-1", claims.ActiveProjectID)
	}
	if claims.OrgID != "o1" {
		t.Errorf("OrgID = %q, want o1", claims.OrgID)
	}
	if claims.AccessToken != "at-session" || claims.RefreshToken != "rt-session" {
		t.Errorf("tokens not preserved: %+v", claims)
	}
}

// TestCreateProjectValidation asserts empty orgId/name are rejected with 400.
func TestCreateProjectValidation(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := sessionServer(s)
	for _, body := range []string{`{"name":"X","orgId":""}`, `{"name":"","orgId":"o1"}`, `{"name":"X"}`, `not json`} {
		req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		addSessionCookie(t, req, "test-secret", sessionClaims{
			AccessToken: "at", ExpiresAt: time.Now().Add(time.Hour).Unix(),
		})
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, rec.Code)
		}
	}
}

// TestCreateProjectWithAPIKeySkipsCookieReissue asserts an API-key caller
// (no session) still creates the project but gets no session cookie update.
func TestCreateProjectWithAPIKeySkipsCookieReissue(t *testing.T) {
	cfg := sessionCfg()
	cfg.ClientAPIKey = "admin-secret"
	f := &fakeMemory{}
	s := &Server{cfg: cfg, memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"API Proj","orgId":"o1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "admin-secret")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	if len(f.createdProjects) != 1 {
		t.Fatalf("create not forwarded: %+v", f.createdProjects)
	}
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		t.Errorf("no session to update for an API-key caller: %+v", ck)
	}
}

// TestActivateProjectReissuesSessionCookie asserts POST
// /api/projects/:id/activate flips ActiveProjectID in the session cookie,
// recovers the project's org, and preserves tokens.
func TestActivateProjectReissuesSessionCookie(t *testing.T) {
	f := &fakeMemory{projects: []ProjectRef{{ID: "proj-new", Name: "New", OrgID: "org-new"}}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-new/activate", nil)
	addSessionCookie(t, req, "test-secret", sessionClaims{
		AccessToken: "at-session", RefreshToken: "rt-session",
		ActiveProjectID: "proj-old", OrgID: "org-old",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"activeProjectId":"proj-new"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not re-issued")
	}
	claims, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims.ActiveProjectID != "proj-new" {
		t.Errorf("ActiveProjectID = %q, want proj-new", claims.ActiveProjectID)
	}
	// Org is recovered from the project record (not left stale/empty).
	if claims.OrgID != "org-new" {
		t.Errorf("OrgID = %q, want org-new after switch", claims.OrgID)
	}
	if claims.AccessToken != "at-session" || claims.RefreshToken != "rt-session" {
		t.Errorf("tokens not preserved: %+v", claims)
	}
}

// TestActivateProjectUnknownOrg asserts that switching to a project not present
// in the tenancy listing leaves the org empty (no stale X-Org-ID) rather than
// erroring.
func TestActivateProjectUnknownOrg(t *testing.T) {
	f := &fakeMemory{projects: []ProjectRef{{ID: "other", Name: "Other", OrgID: "org-other"}}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-ghost/activate", nil)
	addSessionCookie(t, req, "test-secret", sessionClaims{
		AccessToken:     "at-session",
		ActiveProjectID: "proj-old", OrgID: "org-old",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not re-issued")
	}
	claims, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims.OrgID != "" {
		t.Errorf("OrgID = %q, want empty for unknown project", claims.OrgID)
	}
}

// TestListProjectsHandler asserts GET /api/projects returns the backend list.
func TestListProjectsHandler(t *testing.T) {
	f := &fakeMemory{projects: []ProjectRef{{ID: "p1", Name: "Main", OrgID: "o1"}}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"p1"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}
