package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListOrgsHandler asserts GET /api/orgs returns an empty array when the
// user has no orgs.
func TestListOrgsHandler(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{orgs: []Org{}}}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/api/orgs", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `[]`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// TestCreateOrgHandler asserts POST /api/orgs creates with 201 and forwards
// the trimmed name; missing name → 400; backend conflict (409) → 409.
func TestCreateOrgHandler(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := &fakeMemory{}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := sessionServer(s)
		req := httptest.NewRequest(http.MethodPost, "/api/orgs", strings.NewReader(`{"name":"  Acme  "}`))
		req.Header.Set("Content-Type", "application/json")
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
		}
		if len(f.createdOrgs) != 1 || f.createdOrgs[0].Name != "Acme" {
			t.Fatalf("create not forwarded: %+v", f.createdOrgs)
		}
		if !strings.Contains(rec.Body.String(), `"org-1"`) {
			t.Errorf("body = %s", rec.Body.String())
		}
	})
	t.Run("missing name", func(t *testing.T) {
		s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
		e := sessionServer(s)
		req := httptest.NewRequest(http.MethodPost, "/api/orgs", strings.NewReader(`{"name":"  "}`))
		req.Header.Set("Content-Type", "application/json")
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
		}
	})
	t.Run("conflict", func(t *testing.T) {
		f := &fakeMemory{createOrgErr: &memoryHTTPError{Status: http.StatusConflict, Code: "conflict", Message: "org already exists"}}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := sessionServer(s)
		req := httptest.NewRequest(http.MethodPost, "/api/orgs", strings.NewReader(`{"name":"Acme"}`))
		req.Header.Set("Content-Type", "application/json")
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body.String())
		}
	})
}

// TestListMembersHandler asserts GET /api/members returns the seeded members.
func TestListMembersHandler(t *testing.T) {
	f := &fakeMemory{members: []ProjectMemberDto{
		{ID: "u1", Email: "ada@example.com", Role: "project_admin"},
		{ID: "u2", Email: "bob@example.com", Role: "project_user"},
	}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/api/members", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ada@example.com") || !strings.Contains(rec.Body.String(), "project_admin") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// TestRemoveMemberHandler asserts DELETE /api/members/:userId removes with 200
// and maps a backend last-admin error to 403.
func TestRemoveMemberHandler(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := &fakeMemory{}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := sessionServer(s)
		req := httptest.NewRequest(http.MethodDelete, "/api/members/u1", nil)
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		if f.removedMember != "u1" {
			t.Errorf("removedMember = %q, want u1", f.removedMember)
		}
		if !strings.Contains(rec.Body.String(), "removed") {
			t.Errorf("body = %s", rec.Body.String())
		}
	})
	t.Run("last admin", func(t *testing.T) {
		f := &fakeMemory{removeMemberErr: fmt.Errorf("memory 403 last-admin: cannot remove the sole admin")}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := sessionServer(s)
		req := httptest.NewRequest(http.MethodDelete, "/api/members/u1", nil)
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "assign another admin") {
			t.Errorf("body = %s", rec.Body.String())
		}
	})
}

// TestListInvitesHandler asserts GET /api/invites returns the seeded sent
// invites for the active project.
func TestListInvitesHandler(t *testing.T) {
	f := &fakeMemory{sentInvites: []SentInviteDto{
		{ID: "i1", Email: "ada@example.com", Role: "project_admin", Status: "pending"},
	}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/api/invites", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ada@example.com") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// TestCreateInviteHandler asserts POST /api/invites forwards orgId/email/role
// with 201; invalid role or missing email → 400.
func TestCreateInviteHandler(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := &fakeMemory{}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := sessionServer(s)
		req := httptest.NewRequest(http.MethodPost, "/api/invites",
			strings.NewReader(`{"orgId":"o1","projectId":"p1","email":"ada@example.com","role":"project_admin"}`))
		req.Header.Set("Content-Type", "application/json")
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
		}
		if len(f.createdInvites) != 1 {
			t.Fatalf("create not forwarded: %+v", f.createdInvites)
		}
		inv := f.createdInvites[0]
		if inv.OrgID != "o1" || inv.ProjectID != "p1" || inv.Email != "ada@example.com" || inv.Role != "project_admin" {
			t.Errorf("invite not forwarded correctly: %+v", inv)
		}
		if !strings.Contains(rec.Body.String(), `"token":"tok-1"`) {
			t.Errorf("body = %s", rec.Body.String())
		}
	})
	t.Run("invalid role", func(t *testing.T) {
		s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
		e := sessionServer(s)
		req := httptest.NewRequest(http.MethodPost, "/api/invites",
			strings.NewReader(`{"orgId":"o1","email":"ada@example.com","role":"superuser"}`))
		req.Header.Set("Content-Type", "application/json")
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
		}
	})
	t.Run("missing email", func(t *testing.T) {
		s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
		e := sessionServer(s)
		req := httptest.NewRequest(http.MethodPost, "/api/invites",
			strings.NewReader(`{"orgId":"o1","role":"project_user"}`))
		req.Header.Set("Content-Type", "application/json")
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
		}
	})
}

// TestListPendingInvitesHandler asserts GET /api/invites/pending returns the
// seeded invites addressed to the signed-in user.
func TestListPendingInvitesHandler(t *testing.T) {
	f := &fakeMemory{pendingInvites: []PendingInviteDto{
		{ID: "p1", OrganizationName: "Acme", ProjectName: "Web", Role: "project_user", Token: "tok-1"},
	}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/api/invites/pending", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Acme") || !strings.Contains(rec.Body.String(), `"token":"tok-1"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// TestAcceptInviteHandler asserts POST /api/invites/accept forwards the token.
func TestAcceptInviteHandler(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/invites/accept", strings.NewReader(`{"token":"tok-abc"}`))
	req.Header.Set("Content-Type", "application/json")
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if len(f.acceptedTokens) != 1 || f.acceptedTokens[0] != "tok-abc" {
		t.Errorf("accept not forwarded: %+v", f.acceptedTokens)
	}
	if !strings.Contains(rec.Body.String(), "accepted") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// TestDeclineInviteHandler asserts POST /api/invites/:id/decline succeeds and
// maps a backend 403 ("not for you") to StatusForbidden.
func TestDeclineInviteHandler(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := &fakeMemory{}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := sessionServer(s)
		req := httptest.NewRequest(http.MethodPost, "/api/invites/i1/decline", nil)
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		if f.declinedInvite != "i1" {
			t.Errorf("declinedInvite = %q, want i1", f.declinedInvite)
		}
		if !strings.Contains(rec.Body.String(), "declined") {
			t.Errorf("body = %s", rec.Body.String())
		}
	})
	t.Run("forbidden", func(t *testing.T) {
		f := &fakeMemory{declineInviteErr: &memoryHTTPError{Status: http.StatusForbidden, Message: "not for you"}}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := sessionServer(s)
		req := httptest.NewRequest(http.MethodPost, "/api/invites/i1/decline", nil)
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
		}
	})
}

// TestCancelInviteHandler asserts DELETE /api/invites/:id revokes the invite.
func TestCancelInviteHandler(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodDelete, "/api/invites/i1", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if f.canceledInvite != "i1" {
		t.Errorf("canceledInvite = %q, want i1", f.canceledInvite)
	}
	if !strings.Contains(rec.Body.String(), "revoked") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// TestSearchUsersHandler asserts GET /api/users/search?email= returns matches
// and forwards the query; a missing query → 400.
func TestSearchUsersHandler(t *testing.T) {
	t.Run("matches", func(t *testing.T) {
		f := &fakeMemory{searchUsers: []UserSearchResultDto{
			{ID: "u1", Email: "ada@example.com", DisplayName: "Ada L"},
		}}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := sessionServer(s)
		req := httptest.NewRequest(http.MethodGet, "/api/users/search?email=ada", nil)
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		if f.lastSearchTerm != "ada" {
			t.Errorf("lastSearchTerm = %q, want ada", f.lastSearchTerm)
		}
		if !strings.Contains(rec.Body.String(), "ada@example.com") {
			t.Errorf("body = %s", rec.Body.String())
		}
	})
	t.Run("empty query", func(t *testing.T) {
		s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
		e := sessionServer(s)
		req := httptest.NewRequest(http.MethodGet, "/api/users/search", nil)
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
		}
	})
}

// TestGetProfileHandler asserts GET /api/user/profile returns the profile.
func TestGetProfileHandler(t *testing.T) {
	f := &fakeMemory{profile: &UserProfileDto{ID: "u1", FirstName: "Ada", LastName: "L", DisplayName: "Ada L"}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/api/user/profile", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Ada") || !strings.Contains(rec.Body.String(), `"id":"u1"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// TestUpdateProfileHandler asserts PUT /api/user/profile forwards the body
// and returns the updated profile.
func TestUpdateProfileHandler(t *testing.T) {
	f := &fakeMemory{profile: &UserProfileDto{ID: "u1", FirstName: "Old", LastName: "Name"}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPut, "/api/user/profile",
		strings.NewReader(`{"firstName":"Ada","lastName":"Lovelace","displayName":"Ada Lovelace"}`))
	req.Header.Set("Content-Type", "application/json")
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if f.updatedProfile.FirstName != "Ada" || f.updatedProfile.LastName != "Lovelace" || f.updatedProfile.DisplayName != "Ada Lovelace" {
		t.Errorf("update not forwarded: %+v", f.updatedProfile)
	}
	if !strings.Contains(rec.Body.String(), "Ada Lovelace") {
		t.Errorf("body = %s", rec.Body.String())
	}
}
