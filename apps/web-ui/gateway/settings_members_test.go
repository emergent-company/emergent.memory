package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// settingsMembersServer binds the Project Settings members routes to the given
// server.
func settingsMembersServer(s *Server) *echo.Echo {
	e := echo.New()
	e.GET("/settings/members", s.uiSettingsMembers)
	e.GET("/settings/members/new", s.uiSettingsMemberInvitePage)
	e.POST("/settings/members", s.uiSettingsMemberInviteCreate)
	e.GET("/settings/members/:userId", s.uiSettingsMemberDetails)
	e.POST("/settings/members/:userId/remove", s.uiSettingsRemoveMember)
	e.POST("/settings/members/:userId/role", s.uiSettingsChangeMemberRole)
	return e
}

// TestUISettingsMembers renders /settings/members inside the settings sub-nav
// ("Members" active) with the member list and, for an admin caller, the write
// controls (add member + remove).
func TestUISettingsMembers(t *testing.T) {
	admin, user := memberFixtures()
	f := &fakeMemory{
		projects: []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o1"}},
		members:  []ProjectMemberDto{admin, user},
		sentInvites: []SentInviteDto{
			{ID: "inv-1", Email: "lin@example.com", Role: "project_user", Status: "pending", CreatedAt: "2026-08-20T09:00:00Z"},
		},
		orgsAndProjects: []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_admin", Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1", Role: "project_admin"}}}},
	}
	s := &Server{cfg: Config{MemoryProjectID: "p1"}, memory: f}
	e := settingsMembersServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/members", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings/members = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"ada@example.com", "grace@example.com", "project admin", "project user",
		"lin@example.com", "Not responded",
		// settings sub-nav renders with Members active
		`href="/settings/members" aria-current="page"`,
		`href="/settings"`, `href="/settings/assistant"`, `href="/settings/overrides"`,
		// admin caller sees the write controls scoped to /settings/members
		`action="/settings/members/u-user/remove"`, `href="/settings/members/new"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /settings/members missing %q", want)
		}
	}
}

// TestUISettingsMembersReadOnly renders the settings members page for a
// non-admin caller: member rows render but the write controls are hidden.
func TestUISettingsMembersReadOnly(t *testing.T) {
	admin, _ := memberFixtures()
	f := &fakeMemory{
		projects: []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o1"}},
		members:  []ProjectMemberDto{admin},
		// project_user of p1, org_member of o1 → not an admin
		orgsAndProjects: []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_member", Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1", Role: "project_user"}}}},
	}
	s := &Server{cfg: Config{MemoryProjectID: "p1"}, memory: f}
	e := settingsMembersServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/members", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "ada@example.com") {
		t.Error("read-only members page should still list members")
	}
	for _, gone := range []string{`href="/settings/members/new"`, `action="/settings/members/u-admin/remove"`} {
		if strings.Contains(body, gone) {
			t.Errorf("non-admin must not see %q", gone)
		}
	}
}

// TestUISettingsChangeMemberRole exercises the settings role-change handler: it
// PATCHes the member's role in place and redirects to /settings/members.
func TestUISettingsChangeMemberRole(t *testing.T) {
	_, user := memberFixtures()
	f := &fakeMemory{
		projects: []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o1"}},
		members:  []ProjectMemberDto{user},
	}
	s := &Server{cfg: Config{MemoryProjectID: "p1"}, memory: f}
	e := settingsMembersServer(s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/members/u-user/role", strings.NewReader(url.Values{"role": {"project_viewer"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/members?role-changed=1" {
		t.Fatalf("change role = %d %q, want 303 /settings/members?role-changed=1", rec.Code, rec.Header().Get("Location"))
	}
	if f.updatedMemberRole != "u-user" || f.updatedRoleVal != "project_viewer" {
		t.Errorf("PATCH not recorded: user=%q role=%q", f.updatedMemberRole, f.updatedRoleVal)
	}
}
