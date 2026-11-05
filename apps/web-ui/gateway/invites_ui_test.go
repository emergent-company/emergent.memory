package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// inviteTestServer wires only the invitation page routes. Keeping it local
// (instead of growing sessionServer) lets these tests assert the exact routing
// the invite link depends on: /invites/accept is a page route, not the
// /api/invites/accept JSON endpoint, and it is registered alongside the
// token-bound per-invite accept/decline forms.
func inviteTestServer(s *Server) *echo.Echo {
	e := echo.New()
	e.Use(s.authDispatch)
	e.GET("/invites/accept", s.uiInviteAccept)
	e.POST("/invites/accept", s.uiInviteAcceptSubmit)
	e.POST("/invites/:id/accept", s.uiAcceptInvite)
	e.POST("/invites/:id/decline", s.uiDeclineInvite)
	return e
}

func pendingInviteAcceptFixture() PendingInviteDto {
	return PendingInviteDto{
		ID:               "inv-1",
		ProjectID:        "proj-1",
		ProjectName:      "Huma KB",
		OrganizationID:   "org-1",
		OrganizationName: "Huma",
		Role:             "project_admin",
		Token:            "tok-abc123",
		CreatedAt:        "2026-10-29T10:34:00Z",
		ExpiresAt:        "2026-11-11T10:34:00Z",
	}
}

// TestInviteAcceptPath pins where an accepted invitation lands: the target
// project's agents page when project-scoped, the org landing for an org-only
// invite, and the org list as the fallback.
func TestInviteAcceptPath(t *testing.T) {
	cases := []struct {
		name      string
		org, proj string
		want      string
	}{
		{"project invite", "org-1", "proj-1", "/o/org-1/p/proj-1/agents"},
		{"org-only invite", "org-1", "", "/o/org-1"},
		{"no scope", "", "", "/orgs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inviteAcceptPath(tc.org, tc.proj); got != tc.want {
				t.Errorf("inviteAcceptPath(%q, %q) = %q, want %q", tc.org, tc.proj, got, tc.want)
			}
			if got := inviteAcceptLanding(tc.org, tc.proj); got != tc.want+"?accepted=1" {
				t.Errorf("inviteAcceptLanding(%q, %q) = %q, want %q", tc.org, tc.proj, got, tc.want+"?accepted=1")
			}
		})
	}
}

// TestInviteTargetLabel covers the org-only invite, which has no project name
// to show and must not render a blank headline.
func TestInviteTargetLabel(t *testing.T) {
	inv := pendingInviteAcceptFixture()
	if got := inviteTargetLabel(&inv); got != "Huma KB" {
		t.Errorf("project invite label = %q, want Huma KB", got)
	}
	inv.ProjectName = ""
	inv.ProjectID = ""
	if got := inviteTargetLabel(&inv); got != "Organization invitation" {
		t.Errorf("org-only invite label = %q, want Organization invitation", got)
	}
}

// TestInviteAcceptPageStates renders the three landing states. The wrong-account
// copy is the piece the naive fix would have hidden behind a generic 502, so it
// is asserted with its account-switch link.
func TestInviteAcceptPageStates(t *testing.T) {
	t.Run("pending invite", func(t *testing.T) {
		inv := pendingInviteAcceptFixture()
		html := renderHTML(t, InviteAcceptPage(inviteAcceptPageData{
			Invite:        &inv,
			Token:         inv.Token,
			SignedInEmail: "nikolai@example.com",
		}))
		for _, want := range []string{"Accept invitation", "Huma KB", inv.Token, "Signed in as nikolai@example.com", "/invites/inv-1/decline"} {
			if !strings.Contains(html, want) {
				t.Errorf("pending-invite page missing %q", want)
			}
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		html := renderHTML(t, InviteAcceptPage(inviteAcceptPageData{
			Token:         "tok-gone",
			SignedInEmail: "nikolai@example.com",
			Unavailable:   true,
		}))
		for _, want := range []string{"Invitation unavailable", "already have been accepted", "/profile/invitations"} {
			if !strings.Contains(html, want) {
				t.Errorf("unavailable page missing %q", want)
			}
		}
		if strings.Contains(html, "Accept invitation") {
			t.Error("unavailable page must not offer an accept button")
		}
	})

	t.Run("wrong account", func(t *testing.T) {
		html := renderHTML(t, InviteAcceptPage(inviteAcceptPageData{
			Token:         "tok-abc123",
			SignedInEmail: "someone.else@example.com",
			Unavailable:   true,
			WrongAccount:  true,
		}))
		for _, want := range []string{
			"This invitation is for another account",
			"someone.else@example.com",
			"/auth/add?return_to=" + url.QueryEscape("/invites/accept?token=tok-abc123"),
		} {
			if !strings.Contains(html, want) {
				t.Errorf("wrong-account page missing %q", want)
			}
		}
	})
}

// TestProjectSwitcherShowsPendingInvitations is the discovery guard: a signed-in
// invitee must see the invitation (and be able to accept it) in the project
// switcher, with a count badge on the trigger, instead of having to find the
// email.
func TestProjectSwitcherShowsPendingInvitations(t *testing.T) {
	orgs := []Org{{ID: "org-1", Name: "Huma"}}
	groups := []projectGroup{{OrgID: "org-1", OrgName: "Huma"}}
	html := renderHTML(t, projectSwitcher(nil, "", nil, groups, orgs, nil, false, []PendingInviteDto{pendingInviteAcceptFixture()}))

	for _, want := range []string{
		`data-testid="pending-invites-badge"`,
		"1 pending invitation",
		"Huma KB",
		"/invites/inv-1/accept",
		`name="return_to" value="/o/org-1/p/proj-1/agents"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("switcher with a pending invite missing %q", want)
		}
	}

	// No invites → no badge and no section.
	plain := renderHTML(t, projectSwitcher(nil, "", nil, groups, orgs, nil, false, nil))
	if strings.Contains(plain, "pending-invites-badge") || strings.Contains(plain, "pending invitation") {
		t.Error("switcher without invites must not render the badge or the section")
	}
}

// TestUiInviteAcceptResolvesToken proves the page resolves the token against the
// caller's own pending invitations: a matching token renders the invitation, an
// unknown one renders the unavailable state (never the invite's details).
func TestUiInviteAcceptResolvesToken(t *testing.T) {
	f := &fakeMemory{pendingInvites: []PendingInviteDto{pendingInviteAcceptFixture()}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := inviteTestServer(s)
	claims := testSessionClaims()
	claims.Email = "nikolai@example.com"

	t.Run("matching token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/invites/accept?token=tok-abc123", nil)
		addSessionCookie(t, req, "test-secret", claims)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		for _, want := range []string{"Accept invitation", "Huma KB", "nikolai@example.com"} {
			if !strings.Contains(rec.Body.String(), want) {
				t.Errorf("body missing %q", want)
			}
		}
	})

	t.Run("unknown token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/invites/accept?token=tok-other", nil)
		addSessionCookie(t, req, "test-secret", claims)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Invitation unavailable") {
			t.Errorf("body should report the unavailable state: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "Huma KB") {
			t.Error("unknown token must not leak another invite's project name")
		}
	})

	t.Run("anonymous redirect preserves the token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/invites/accept?token=tok-abc123", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302", rec.Code)
		}
		loc := rec.Header().Get("Location")
		if !strings.HasPrefix(loc, "/auth/login?return_to=") ||
			!strings.Contains(loc, url.QueryEscape("/invites/accept?token=tok-abc123")) {
			t.Errorf("Location = %q, want a login redirect that preserves the invite URL", loc)
		}
	})
}

// TestUiInviteAcceptSubmitLandsInProject covers the accept form: the token is
// forwarded to the backend and the browser lands inside the joined project.
func TestUiInviteAcceptSubmitLandsInProject(t *testing.T) {
	f := &fakeMemory{pendingInvites: []PendingInviteDto{pendingInviteAcceptFixture()}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := inviteTestServer(s)

	form := url.Values{"token": {"tok-abc123"}, "orgId": {"org-1"}, "projectId": {"proj-1"}}
	req := httptest.NewRequest(http.MethodPost, "/invites/accept", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (%s)", rec.Code, rec.Body.String())
	}
	if got, want := rec.Header().Get("Location"), "/o/org-1/p/proj-1/agents?accepted=1"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	if len(f.acceptedTokens) != 1 || f.acceptedTokens[0] != "tok-abc123" {
		t.Errorf("acceptedTokens = %v, want [tok-abc123]", f.acceptedTokens)
	}
}

// TestUiInviteAcceptSubmitWrongAccount is the behaviour the wrong-account fix
// exists for: a 403 from the backend renders "this invitation is for another
// account" with a switch-account link, instead of a generic failure.
func TestUiInviteAcceptSubmitWrongAccount(t *testing.T) {
	f := &fakeMemory{acceptInviteErr: &memoryHTTPError{Status: http.StatusForbidden, Message: "this invite is not for you"}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := inviteTestServer(s)

	form := url.Values{"token": {"tok-abc123"}, "orgId": {"org-1"}, "projectId": {"proj-1"}}
	req := httptest.NewRequest(http.MethodPost, "/invites/accept", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "This invitation is for another account") {
		t.Errorf("body should name the wrong-account case: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/auth/add?return_to=") {
		t.Errorf("body should offer an account switch: %s", rec.Body.String())
	}
}

// TestUiAcceptInviteFromSwitcherLandsAndFlashes covers the switcher form: it
// posts to /invites/:id/accept with a return_to inside the app, and the accept
// lands there (not on the invitations page) with the success flash.
func TestUiAcceptInviteFromSwitcherLandsAndFlashes(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := inviteTestServer(s)

	form := url.Values{"token": {"tok-abc123"}, "return_to": {"/o/org-1/p/proj-1/agents"}}
	req := httptest.NewRequest(http.MethodPost, "/invites/inv-1/accept", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (%s)", rec.Code, rec.Body.String())
	}
	if got, want := rec.Header().Get("Location"), "/o/org-1/p/proj-1/agents?accepted=1"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

// TestUiAcceptInviteExternallyHostedReturnToRejected proves the open-redirect
// guard still applies to the new return_to parameter: an absolute URL is
// ignored and the invitations page is used.
func TestUiAcceptInviteExternallyHostedReturnToRejected(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := inviteTestServer(s)

	form := url.Values{"token": {"tok-abc123"}, "return_to": {"https://evil.example/phish"}}
	req := httptest.NewRequest(http.MethodPost, "/invites/inv-1/accept", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if got := rec.Header().Get("Location"); got != "/profile/invitations?accepted=1" {
		t.Errorf("Location = %q, want the local invitations page", got)
	}
}

// TestAgentsPageConfirmsInvitationAcceptance covers the landing confirmation:
// accepting an invitation redirects to the joined project's agents page with
// ?accepted=1, and that page has to say so — otherwise the one action the user
// took leaves no visible trace.
func TestAgentsPageConfirmsInvitationAcceptance(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := sessionServer(s)

	read := func(query string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/agents"+query, nil)
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /agents%s = %d, want 200 (%s)", query, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	if body := read("?accepted=1"); !strings.Contains(body, "Invitation accepted.") {
		t.Errorf("agents page with ?accepted=1 must confirm the acceptance")
	}
	if body := read(""); strings.Contains(body, "Invitation accepted.") {
		t.Errorf("agents page without ?accepted=1 must not claim an invitation was accepted")
	}
}

// TestOrgLandingConfirmsInvitationAcceptance covers the organization-only
// invite landing: /o/{org} rewrites to /orgs/{org}, so uiOrg is where the
// invitee arrives and therefore where the confirmation has to be rendered.
func TestOrgLandingConfirmsInvitationAcceptance(t *testing.T) {
	f := &fakeMemory{orgs: []Org{{ID: "org-1", Name: "Huma"}}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := echo.New()
	e.Use(s.authDispatch)
	e.GET("/orgs/:id", s.uiOrg)

	read := func(query string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/orgs/org-1"+query, nil)
		addSessionCookie(t, req, "test-secret", testSessionClaims())
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /orgs/org-1%s = %d, want 200 (%s)", query, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	if body := read("?accepted=1"); !strings.Contains(body, "Invitation accepted.") {
		t.Errorf("org landing with ?accepted=1 must confirm the acceptance")
	}
	if body := read(""); strings.Contains(body, "Invitation accepted.") {
		t.Errorf("org landing without ?accepted=1 must not claim an invitation was accepted")
	}
}

// TestPendingInviteRowDeclineReturnsToItsPage pins the return path of the
// decline form per surface. The row is rendered on both /orgs and
// /profile/invitations, and each has its own decline flash: a single hardcoded
// return_to makes the other page's flash unreachable (and bounces the user to a
// page they were not on).
func TestPendingInviteRowDeclineReturnsToItsPage(t *testing.T) {
	pending := []PendingInviteDto{pendingInviteAcceptFixture()}

	orgs := renderHTML(t, OrgsPage(orgsPageData{Pending: pending}))
	if !strings.Contains(orgs, `name="return_to" value="/orgs"`) {
		t.Error("decline form on /orgs must carry return_to=/orgs")
	}
	if strings.Contains(orgs, `name="return_to" value="/profile/invitations"`) {
		t.Error("decline form on /orgs must not send the user to /profile/invitations")
	}

	invitations := renderHTML(t, InvitationsPage(invitationsPageData{Pending: pending}))
	if !strings.Contains(invitations, `name="return_to" value="/profile/invitations"`) {
		t.Error("decline form on /profile/invitations must carry return_to=/profile/invitations")
	}
	if strings.Contains(invitations, `name="return_to" value="/orgs"`) {
		t.Error("decline form on /profile/invitations must not send the user to /orgs")
	}
}
