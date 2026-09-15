package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// orgContextUIServer mirrors main.go's org-context wiring: the session
// middleware, the landing redirect, and the org context routes.
func orgContextUIServer(s *Server) *echo.Echo {
	e := echo.New()
	e.Use(s.authDispatch)
	e.GET("/", s.uiRoot)
	e.GET("/orgs/:id", s.uiOrg)
	e.GET("/orgs/:id/members", s.uiOrgMembers)
	e.GET("/orgs/:id/invite", s.uiOrgInvitePage)
	e.POST("/orgs/:id/invite", s.uiOrgInviteCreate)
	e.GET("/orgs/:id/settings", s.uiOrgSettings)
	e.GET("/orgs/:id/settings/general", s.uiOrgSettingsGeneral)
	e.GET("/orgs/:id/settings/danger-zone", s.uiOrgSettingsDangerZone)
	e.POST("/orgs/:id/rename", s.uiOrgRename)
	e.POST("/orgs/:id/tool-settings/:toolName", s.uiOrgToolSettingUpdate)
	e.POST("/orgs/:id/tool-settings/:toolName/delete", s.uiOrgToolSettingDelete)
	e.POST("/orgs/:id/delete", s.uiDeleteOrg)
	e.POST("/projects/delete", s.uiDeleteProjects)
	e.POST("/projects/transfer", s.uiTransferProject)
	e.GET("/agents", s.uiAgents)
	return e
}

func sessionCookie(t *testing.T, secret string, claims sessionClaims) *http.Cookie {
	t.Helper()
	val, err := issueSession(secret, claims, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: val, Path: "/"}
}

// TestUIRootRedirect covers the GET / landing resolution across contexts.
func TestUIRootRedirect(t *testing.T) {
	t.Run("dev mode keeps the static-project landing", func(t *testing.T) {
		s := &Server{cfg: Config{MemoryProjectID: "memory"}, memory: &fakeMemory{}}
		e := orgContextUIServer(s)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/agents" {
			t.Fatalf("/ in dev mode = %d %q, want 302 /agents", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("session project wins", func(t *testing.T) {
		s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
		e := orgContextUIServer(s)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
			AccessToken:     "at",
			ActiveProjectID: "p1",
			ExpiresAt:       time.Now().Add(time.Hour).Unix(),
		}))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/agents" {
			t.Fatalf("/ with project = %d %q, want 302 /agents", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("session org redirects to the org landing", func(t *testing.T) {
		s := &Server{cfg: sessionCfg(), memory: &fakeMemory{orgs: []Org{{ID: "o1", Name: "Acme"}}}}
		e := orgContextUIServer(s)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
			AccessToken: "at",
			OrgID:       "o1",
			ExpiresAt:   time.Now().Add(time.Hour).Unix(),
		}))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/orgs/o1" {
			t.Fatalf("/ with org = %d %q, want 302 /orgs/o1", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("first org auto-selected when none active", func(t *testing.T) {
		s := &Server{cfg: sessionCfg(), memory: &fakeMemory{orgs: []Org{{ID: "o1", Name: "Acme"}}}}
		e := orgContextUIServer(s)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
			AccessToken: "at",
			Sub:         "sub-a",
			ExpiresAt:   time.Now().Add(time.Hour).Unix(),
		}))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/orgs/o1" {
			t.Fatalf("/ with no org = %d %q, want 302 /orgs/o1", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("no orgs redirects to the wizard", func(t *testing.T) {
		s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
		e := orgContextUIServer(s)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
			AccessToken: "at",
			Sub:         "sub-a",
			ExpiresAt:   time.Now().Add(time.Hour).Unix(),
		}))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/orgs/new" {
			t.Fatalf("/ with no orgs = %d %q, want 302 /orgs/new", rec.Code, rec.Header().Get("Location"))
		}
	})
}

// TestUIOrgLanding covers the org landing: populated list, empty state, and
// the org-context sidebar (not the project nav).
func TestUIOrgLanding(t *testing.T) {
	f := &fakeMemory{
		orgs: []Org{{ID: "o1", Name: "Acme"}},
		projects: []ProjectRef{
			{ID: "p1", Name: "Home", OrgID: "o1"},
			{ID: "p2", Name: "Lab", OrgID: "o1"},
			{ID: "p3", Name: "Archived", OrgID: "o1", DeletionStatus: "pending_deletion"},
		},
	}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := orgContextUIServer(s)

	req := httptest.NewRequest(http.MethodGet, "/orgs/o1", nil)
	req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
		AccessToken: "at",
		OrgID:       "o1",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /orgs/o1 = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Acme", "Home", "Lab", "Archived"} {
		if !strings.Contains(body, want) {
			t.Errorf("org landing missing %q", want)
		}
	}
	// Active project names are clickable: an activate button with a title.
	for _, want := range []string{`hx-post="/projects/activate?projectId=p1"`, `title="Open Home"`, `hx-post="/projects/activate?projectId=p2"`, `title="Open Lab"`} {
		if !strings.Contains(body, want) {
			t.Errorf("org landing active project affordance missing %q", want)
		}
	}
	// Pending-deletion project name is a plain span, never an activate target.
	if strings.Contains(body, `hx-post="/projects/activate?projectId=p3"`) {
		t.Errorf("org landing must not render an activate target for pending project p3")
	}
	// org-context sidebar renders, project nav does not
	for _, want := range []string{`href="/orgs/o1/members"`, `href="/orgs/o1/settings"`} {
		if !strings.Contains(body, want) {
			t.Errorf("org sidebar missing %q", want)
		}
	}
	for _, absent := range []string{"Memory Browser", `href="/objects"`} {
		if strings.Contains(body, absent) {
			t.Errorf("project nav must not render in org context, found %q", absent)
		}
	}
}

// TestUIOrgLandingEmpty covers the empty-org state with the create CTA.
func TestUIOrgLandingEmpty(t *testing.T) {
	f := &fakeMemory{orgs: []Org{{ID: "o1", Name: "Acme"}}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := orgContextUIServer(s)

	req := httptest.NewRequest(http.MethodGet, "/orgs/o1", nil)
	req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
		AccessToken: "at",
		OrgID:       "o1",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /orgs/o1 = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "No projects yet") || !strings.Contains(body, "Create project") {
		t.Errorf("empty org landing missing empty state/CTA, got %q", body)
	}
}

// TestUIOrgLandingNotFound covers the unknown-org error path.
func TestUIOrgLandingNotFound(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := orgContextUIServer(s)
	req := httptest.NewRequest(http.MethodGet, "/orgs/missing", nil)
	req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
		AccessToken: "at",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Organization unavailable") {
		t.Errorf("unknown org = %d, want 200 with error state", rec.Code)
	}
}

// TestUIOrgMembers covers the org members page.
func TestUIOrgMembers(t *testing.T) {
	dn := "Ada Lovelace"
	f := &fakeMemory{
		orgs: []Org{{ID: "o1", Name: "Acme"}},
		orgMembers: []OrgMemberDto{
			{ID: "u1", Email: "ada@x.io", DisplayName: &dn, Role: "org_admin", JoinedAt: time.Now().Format(time.RFC3339)},
		},
	}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := orgContextUIServer(s)
	req := httptest.NewRequest(http.MethodGet, "/orgs/o1/members", nil)
	req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
		AccessToken: "at",
		OrgID:       "o1",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /orgs/o1/members = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Ada Lovelace", "ada@x.io", "org admin"} {
		if !strings.Contains(body, want) {
			t.Errorf("org members page missing %q", want)
		}
	}
}

// TestUIOrgInviteRoutes covers the org-level invite flow: the GET page renders
// the form, the POST creates an organization-scoped org_admin invite (org id,
// no projectId), invalid roles are rejected server-side, and a memory failure
// surfaces honestly via the ?err= flash.
func TestUIOrgInviteRoutes(t *testing.T) {
	f := &fakeMemory{orgs: []Org{{ID: "o1", Name: "Acme"}}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := orgContextUIServer(s)

	// GET page renders the form bound to the org.
	rec := orgGet(t, e, "/orgs/o1/invite")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /orgs/o1/invite = %d, want 200", rec.Code)
	}
	for _, want := range []string{`action="/orgs/o1/invite"`, `name="orgId" value="o1"`, `value="org_admin"`, "Send invitation"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("GET /orgs/o1/invite missing %q", want)
		}
	}
	if strings.Contains(rec.Body.String(), `name="projectId"`) {
		t.Error("org invite form must not carry a projectId")
	}

	// POST org_admin invite → org-scoped invite (no projectId), 303 to members.
	rec = orgPost(t, e, "/orgs/o1/invite", "email=newadmin@example.com&role=org_admin")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/orgs/o1/members?invited=1" {
		t.Fatalf("POST /orgs/o1/invite = %d %q, want 303 /orgs/o1/members?invited=1", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.createdInvites) != 1 {
		t.Fatalf("CreateInvite not recorded: %+v", f.createdInvites)
	}
	inv := f.createdInvites[0]
	if inv.OrgID != "o1" || inv.ProjectID != "" || inv.Email != "newadmin@example.com" || inv.Role != "org_admin" {
		t.Errorf("created org invite fields wrong: %+v", inv)
	}

	// The members page surfaces the success flash.
	rec = orgGet(t, e, "/orgs/o1/members?invited=1")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Invitation sent.") {
		t.Errorf("org members invited flash = %d, want 200 with %q", rec.Code, "Invitation sent.")
	}

	// Invalid role (a project role) is rejected server-side, before memory.
	rec = orgPost(t, e, "/orgs/o1/invite", "email=x@example.com&role=project_admin")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/orgs/o1/invite?err=invalid+role") {
		t.Errorf("invalid role = %d %q, want /orgs/o1/invite error redirect", rec.Code, rec.Header().Get("Location"))
	}
	// Missing email is rejected before memory.
	rec = orgPost(t, e, "/orgs/o1/invite", "email=&role=org_admin")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "err=email+is+required") {
		t.Errorf("missing email = %d %q, want error redirect", rec.Code, rec.Header().Get("Location"))
	}
	// Both local rejections leave the recorded invite list untouched.
	if len(f.createdInvites) != 1 {
		t.Errorf("local rejections must not call CreateInvite, recorded %+v", f.createdInvites)
	}

	// A memory rejection surfaces honestly as an error flash with no invite row.
	f.createInviteErr = errTest
	rec = orgPost(t, e, "/orgs/o1/invite", "email=admin2@example.com&role=org_admin")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "err=backend+unreachable") {
		t.Errorf("memory error = %d %q, want error redirect", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.createdInvites) != 1 {
		t.Errorf("rejected invite must not be recorded, got %+v", f.createdInvites)
	}
}

// TestUIOrgSettings covers the Tools section of the org Settings hub and the
// toggle/delete PRG flows; TestUIOrgSettingsDangerZone covers the Danger zone
// section with the delete-org form.
func TestUIOrgSettings(t *testing.T) {
	f := &fakeMemory{
		orgs: []Org{{ID: "o1", Name: "Acme"}},
		orgToolSettings: []OrgToolSettingDto{
			{ID: "ts1", OrgID: "o1", ToolName: "web_search", Enabled: true},
		},
	}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := orgContextUIServer(s)

	req := httptest.NewRequest(http.MethodGet, "/orgs/o1/settings", nil)
	req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
		AccessToken: "at",
		OrgID:       "o1",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /orgs/o1/settings = %d, want 200", rec.Code)
	}
	for _, want := range []string{"web_search", `href="/orgs/o1/settings/danger-zone"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("org settings page missing %q", want)
		}
	}

	// toggle → redirect back to the settings hub with ?updated=1
	rec = orgPost(t, e, "/orgs/o1/tool-settings/web_search", "enabled=false")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/orgs/o1/settings?updated=1" {
		t.Errorf("toggle = %d %q, want 303 /orgs/o1/settings?updated=1", rec.Code, rec.Header().Get("Location"))
	}
	if f.toolSettingOrg != "o1" {
		t.Errorf("UpsertOrgToolSetting org = %q, want o1", f.toolSettingOrg)
	}

	// delete → redirect back
	rec = orgPost(t, e, "/orgs/o1/tool-settings/web_search/delete", "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/orgs/o1/settings?updated=1" {
		t.Errorf("delete = %d %q, want 303 updated", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIOrgSettingsDangerZone(t *testing.T) {
	f := &fakeMemory{orgs: []Org{{ID: "o1", Name: "Acme"}}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := orgContextUIServer(s)
	req := httptest.NewRequest(http.MethodGet, "/orgs/o1/settings/danger-zone", nil)
	req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
		AccessToken: "at",
		OrgID:       "o1",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /orgs/o1/settings/danger-zone = %d, want 200", rec.Code)
	}
	for _, want := range []string{"Danger zone", `action="/orgs/o1/delete"`, `href="/orgs/o1/settings"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("danger-zone page missing %q", want)
		}
	}
}

// TestUIOrgRename covers the org General settings section and the rename PRG
// flow: the GET form prefilled with the current name, the POST forwarding id +
// name to memory (303 + ?renamed=1 flash), the local empty-name guard, and the
// backend-error path.
func TestUIOrgRename(t *testing.T) {
	f := &fakeMemory{orgs: []Org{{ID: "o1", Name: "Acme"}}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := orgContextUIServer(s)

	// GET renders the rename form bound to the org's current name.
	rec := orgGet(t, e, "/orgs/o1/settings/general")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /orgs/o1/settings/general = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`action="/orgs/o1/rename"`, `name="name"`, `value="Acme"`, "Save name", `href="/orgs/o1/settings/general"`} {
		if !strings.Contains(body, want) {
			t.Errorf("org general page missing %q", want)
		}
	}

	// POST renames via memory and redirects to the General section.
	rec = orgPost(t, e, "/orgs/o1/rename", "name=Acme+Renamed")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/orgs/o1/settings/general?renamed=1" {
		t.Fatalf("POST /orgs/o1/rename = %d %q, want 303 /orgs/o1/settings/general?renamed=1", rec.Code, rec.Header().Get("Location"))
	}
	if f.renamedOrgID != "o1" || f.renamedOrgName != "Acme Renamed" {
		t.Errorf("UpdateOrg = (%q, %q), want (o1, Acme Renamed)", f.renamedOrgID, f.renamedOrgName)
	}

	// The success flash surfaces on the redirected page.
	rec = orgGet(t, e, "/orgs/o1/settings/general?renamed=1")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Organization renamed.") {
		t.Errorf("renamed flash = %d, want 200 with %q", rec.Code, "Organization renamed.")
	}

	// Empty name is rejected locally, before memory.
	f.renamedOrgID, f.renamedOrgName = "", ""
	rec = orgPost(t, e, "/orgs/o1/rename", "name=")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/orgs/o1/settings/general?err=name+is+required") {
		t.Errorf("empty name = %d %q, want error redirect", rec.Code, rec.Header().Get("Location"))
	}
	if f.renamedOrgID != "" || f.renamedOrgName != "" {
		t.Errorf("local rejection must not call UpdateOrg, got (%q, %q)", f.renamedOrgID, f.renamedOrgName)
	}

	// A memory rejection surfaces honestly via the ?err= flash, no state change.
	f.updateOrgErr = errTest
	rec = orgPost(t, e, "/orgs/o1/rename", "name=Nope")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/orgs/o1/settings/general?err=backend+unreachable") {
		t.Errorf("memory error = %d %q, want error redirect", rec.Code, rec.Header().Get("Location"))
	}
	if f.renamedOrgID != "" {
		t.Errorf("rejected rename must not record a call, got %q", f.renamedOrgID)
	}
}

// orgPost issues an authenticated (session) urlencoded POST as org o1.
func orgPost(t *testing.T, e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
		AccessToken: "at",
		OrgID:       "o1",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}))
	e.ServeHTTP(rec, req)
	return rec
}

// TestUIDeleteOrg covers the delete-org flow: memory delete + context clear.
func TestUIDeleteOrg(t *testing.T) {
	f := &fakeMemory{orgs: []Org{{ID: "o1", Name: "Acme"}}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := orgContextUIServer(s)

	req := httptest.NewRequest(http.MethodPost, "/orgs/o1/delete", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
		AccessToken: "at",
		OrgID:       "o1",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/orgs/new" {
		t.Errorf("delete org = %d %q, want 303 /orgs/new", rec.Code, rec.Header().Get("Location"))
	}
	if f.deletedOrgID != "o1" {
		t.Errorf("DeleteOrg id = %q, want o1", f.deletedOrgID)
	}

	t.Run("delete failure redirects to the danger zone", func(t *testing.T) {
		f := &fakeMemory{orgs: []Org{{ID: "o1", Name: "Acme"}}, deleteOrgErr: fmt.Errorf("org has active projects")}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := orgContextUIServer(s)
		rec := orgPost(t, e, "/orgs/o1/delete", "")
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("delete failure = %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/orgs/o1/settings/danger-zone?err=") {
			t.Errorf("delete failure Location = %q, want /orgs/o1/settings/danger-zone?err=", loc)
		}
	})
}

// TestIsOrgContextPath covers the Referer-path classifier used to avoid
// bouncing a project switch back onto an org-context page.
func TestIsOrgContextPath(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"http://localhost:8095/orgs/o1", true},
		{"http://localhost:8095/orgs/o1/members", true},
		{"http://localhost:8095/orgs/o1/settings", true},
		{"http://localhost:8095/orgs/o1/settings/danger-zone", true},
		{"http://localhost:8095/orgs/new", true},
		{"http://localhost:8095/orgs", true},
		{"http://localhost:8095/agents", false},
		{"http://localhost:8095/settings", false},
		{"", false},
		{"not a url", false},
	} {
		if got := isOrgContextPath(c.in); got != c.want {
			t.Errorf("isOrgContextPath(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestUIActivateProjectFromOrgContext asserts a project switch from the org
// context lands on the project page (not back on the org page, which would
// re-activate the org and undo the switch).
func TestUIActivateProjectFromOrgContext(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()
	e.Use(s.authDispatch)
	e.POST("/projects/activate", s.uiActivateProject)

	post := func(referer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/projects/activate", strings.NewReader("projectId=p1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
			AccessToken: "at",
			OrgID:       "o1",
			ExpiresAt:   time.Now().Add(time.Hour).Unix(),
		}))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	if rec := post("http://localhost:8095/orgs/o1"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/agents" {
		t.Errorf("activate from org = %d %q, want 303 /agents", rec.Code, rec.Header().Get("Location"))
	}
	if rec := post("http://localhost:8095/settings"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "http://localhost:8095/settings" {
		t.Errorf("activate from settings = %d %q, want 303 back to settings", rec.Code, rec.Header().Get("Location"))
	}
	if rec := post(""); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/agents" {
		t.Errorf("activate without referer = %d %q, want 303 /agents", rec.Code, rec.Header().Get("Location"))
	}
}

// TestUIDeleteProjects covers the delete-project flow on the org landing:
// bulk via the table form (plain POST → 303), single via the row action menu
// (HTMX → 200 + HX-Redirect), and the empty-selection error path.
func TestUIDeleteProjects(t *testing.T) {
	t.Run("bulk delete via plain form", func(t *testing.T) {
		f := &fakeMemory{}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := orgContextUIServer(s)
		rec := orgPost(t, e, "/projects/delete", "orgId=o1&projectId=p1&projectId=p2")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/orgs/o1?deleted=2" {
			t.Errorf("bulk delete = %d %q, want 303 /orgs/o1?deleted=2", rec.Code, rec.Header().Get("Location"))
		}
		if got := f.deletedProjectIDs; !reflect.DeepEqual(got, []string{"p1", "p2"}) {
			t.Errorf("DeleteProject ids = %v, want [p1 p2]", got)
		}
	})

	t.Run("single delete via HTMX", func(t *testing.T) {
		f := &fakeMemory{}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := orgContextUIServer(s)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/projects/delete?orgId=o1&projectId=p1", nil)
		req.Header.Set("HX-Request", "true")
		req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
			AccessToken: "at",
			OrgID:       "o1",
			ExpiresAt:   time.Now().Add(time.Hour).Unix(),
		}))
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/orgs/o1?deleted=1" {
			t.Errorf("single HTMX delete = %d HX-Redirect %q, want 200 /orgs/o1?deleted=1", rec.Code, rec.Header().Get("HX-Redirect"))
		}
		if got := f.deletedProjectIDs; !reflect.DeepEqual(got, []string{"p1"}) {
			t.Errorf("DeleteProject ids = %v, want [p1]", got)
		}
	})

	t.Run("empty selection errors", func(t *testing.T) {
		f := &fakeMemory{}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := orgContextUIServer(s)
		rec := orgPost(t, e, "/projects/delete", "orgId=o1")
		if rec.Code != http.StatusSeeOther {
			t.Errorf("empty selection = %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "?err=") {
			t.Errorf("empty selection Location = %q, want ?err= flash", loc)
		}
		if len(f.deletedProjectIDs) != 0 {
			t.Errorf("DeleteProject called with %v, want none", f.deletedProjectIDs)
		}
	})
}

// transferTree is a two-org access tree (source o1 org_admin + destination o2)
// for the transfer handler/page tests.
func transferTree() []OrgWithProjectsDto {
	return []OrgWithProjectsDto{
		{ID: "o1", Name: "Acme", Role: "org_admin"},
		{ID: "o2", Name: "Globex", Role: "org_member"},
	}
}

// TestUITransferProject covers the transfer-project flow on the org landing:
// success via plain POST (303) and HTMX (200 + HX-Redirect) with the exact
// TransferProject call args, the local guard short-circuits (source-as-
// destination and missing destination never reach the backend), and the
// backend-error path (error flash, no state change).
func TestUITransferProject(t *testing.T) {
	t.Run("success via plain form", func(t *testing.T) {
		f := &fakeMemory{orgsAndProjects: transferTree()}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := orgContextUIServer(s)
		rec := orgPost(t, e, "/projects/transfer", "orgId=o1&projectId=p1&destinationOrgId=o2")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/orgs/o1?moved=o2&project=p1" {
			t.Errorf("transfer = %d %q, want 303 /orgs/o1?moved=o2&project=p1", rec.Code, rec.Header().Get("Location"))
		}
		if f.transferCount != 1 || f.transferredID != "p1" || f.transferredToOrg != "o2" {
			t.Errorf("TransferProject = (%d, %q -> %q), want (1, p1 -> o2)", f.transferCount, f.transferredID, f.transferredToOrg)
		}
	})

	t.Run("success via HTMX", func(t *testing.T) {
		f := &fakeMemory{orgsAndProjects: transferTree()}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := orgContextUIServer(s)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/projects/transfer?orgId=o1&projectId=p1&destinationOrgId=o2", nil)
		req.Header.Set("HX-Request", "true")
		req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
			AccessToken: "at",
			OrgID:       "o1",
			ExpiresAt:   time.Now().Add(time.Hour).Unix(),
		}))
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/orgs/o1?moved=o2&project=p1" {
			t.Errorf("HTMX transfer = %d HX-Redirect %q, want 200 /orgs/o1?moved=o2&project=p1", rec.Code, rec.Header().Get("HX-Redirect"))
		}
		if f.transferCount != 1 || f.transferredID != "p1" || f.transferredToOrg != "o2" {
			t.Errorf("TransferProject = (%d, %q -> %q), want (1, p1 -> o2)", f.transferCount, f.transferredID, f.transferredToOrg)
		}
	})

	t.Run("source as destination is rejected locally", func(t *testing.T) {
		f := &fakeMemory{orgsAndProjects: transferTree()}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := orgContextUIServer(s)
		rec := orgPost(t, e, "/projects/transfer", "orgId=o1&projectId=p1&destinationOrgId=o1")
		if rec.Code != http.StatusSeeOther {
			t.Errorf("self transfer = %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "?err=") {
			t.Errorf("self transfer Location = %q, want ?err= flash", loc)
		}
		if f.transferCount != 0 {
			t.Errorf("TransferProject called %d times, want 0 (local guard)", f.transferCount)
		}
	})

	t.Run("missing destination is rejected locally", func(t *testing.T) {
		f := &fakeMemory{orgsAndProjects: transferTree()}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := orgContextUIServer(s)
		rec := orgPost(t, e, "/projects/transfer", "orgId=o1&projectId=p1")
		if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "?err=") {
			t.Errorf("missing destination = %d %q, want 303 with ?err= flash", rec.Code, rec.Header().Get("Location"))
		}
		if f.transferCount != 0 {
			t.Errorf("TransferProject called %d times, want 0 (local guard)", f.transferCount)
		}
	})

	t.Run("destination outside the user's orgs is rejected locally", func(t *testing.T) {
		f := &fakeMemory{orgsAndProjects: transferTree()}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := orgContextUIServer(s)
		rec := orgPost(t, e, "/projects/transfer", "orgId=o1&projectId=p1&destinationOrgId=o-other")
		if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "?err=") {
			t.Errorf("unknown destination = %d %q, want 303 with ?err= flash", rec.Code, rec.Header().Get("Location"))
		}
		if f.transferCount != 0 {
			t.Errorf("TransferProject called %d times, want 0 (local guard)", f.transferCount)
		}
	})

	t.Run("backend error surfaces and leaves state unchanged", func(t *testing.T) {
		f := &fakeMemory{orgsAndProjects: transferTree(), transferProjectErr: fmt.Errorf("memory 403 forbidden: not an admin of the destination org")}
		s := &Server{cfg: sessionCfg(), memory: f}
		e := orgContextUIServer(s)
		rec := orgPost(t, e, "/projects/transfer", "orgId=o1&projectId=p1&destinationOrgId=o2")
		if rec.Code != http.StatusSeeOther {
			t.Errorf("backend error = %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "?err=") {
			t.Errorf("backend error Location = %q, want ?err= flash", loc)
		}
		if f.transferCount != 0 || f.transferredID != "" || f.transferredToOrg != "" {
			t.Errorf("TransferProject recorded (%d, %q, %q) after error, want none", f.transferCount, f.transferredID, f.transferredToOrg)
		}
	})
}

// TestTransferState covers the org-landing transfer affordance derivation from
// the access tree: candidates are the user's orgs minus the source, and the
// action shows only for an org_admin of the source with ≥1 candidate.
func TestTransferState(t *testing.T) {
	t.Run("org_admin with a candidate can transfer", func(t *testing.T) {
		dest, can := transferState(transferTree(), "o1")
		if !can {
			t.Errorf("canTransfer = false, want true")
		}
		if want := []Org{{ID: "o2", Name: "Globex"}}; !reflect.DeepEqual(dest, want) {
			t.Errorf("destinations = %+v, want %+v", dest, want)
		}
	})

	t.Run("org_admin with no other org cannot transfer", func(t *testing.T) {
		tree := []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_admin"}}
		if dest, can := transferState(tree, "o1"); can || len(dest) != 0 {
			t.Errorf("single-org admin = (%+v, %v), want (nil, false)", dest, can)
		}
	})

	t.Run("non-admin cannot transfer even with candidates", func(t *testing.T) {
		tree := []OrgWithProjectsDto{
			{ID: "o1", Name: "Acme", Role: "org_member"},
			{ID: "o2", Name: "Globex", Role: "org_member"},
		}
		dest, can := transferState(tree, "o1")
		if can {
			t.Errorf("canTransfer = true for org_member, want false")
		}
		if want := []Org{{ID: "o2", Name: "Globex"}}; !reflect.DeepEqual(dest, want) {
			t.Errorf("destinations = %+v, want %+v", dest, want)
		}
	})

	t.Run("empty tree cannot transfer", func(t *testing.T) {
		if dest, can := transferState(nil, "o1"); can || len(dest) != 0 {
			t.Errorf("empty tree = (%+v, %v), want (nil, false)", dest, can)
		}
	})
}

// TestUIOrgLandingTransferAffordance asserts the Transfer row action and its
// destination dialog render only when the acting user can transfer (org_admin
// of the current org with ≥1 other org), and that the success flash names the
// moved project and its new org.
func TestUIOrgLandingTransferAffordance(t *testing.T) {
	f := &fakeMemory{
		orgs:            []Org{{ID: "o1", Name: "Acme"}, {ID: "o2", Name: "Globex"}},
		projects:        []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o1"}},
		orgsAndProjects: transferTree(),
	}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := orgContextUIServer(s)

	rec := orgGet(t, e, "/orgs/o1")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /orgs/o1 = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	// Transfer row action carries the row's project id/name for the dialog.
	for _, want := range []string{`data-project-id="p1"`, `data-project-name="Home"`, `id="transfer-project-modal"`, `name="destinationOrgId"`, `name="projectId"`, `<option value="o2">Globex</option>`} {
		if !strings.Contains(body, want) {
			t.Errorf("org landing with transfer missing %q", want)
		}
	}
	// The source org is excluded from the destination options.
	if strings.Contains(body, `<option value="o1">`) {
		t.Errorf("org landing destination options must exclude the source org")
	}

	// Success flash after a transfer names project + destination org. The
	// moved project now belongs to o2, so it left o1's list but stays in the
	// user's overall project list (which uiOrg loads to name the flash).
	f.projects = []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o2"}}
	rec = orgGet(t, e, "/orgs/o1?moved=o2&project=p1")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Home moved to Globex.") {
		t.Errorf("moved flash = %d, want 200 with %q", rec.Code, "Home moved to Globex.")
	}

	// Non-admin: no Transfer row action and no dialog. (The id string also
	// appears in the page's JS, so assert on the dialog's rendered markup.)
	fNonAdmin := &fakeMemory{
		orgs:            []Org{{ID: "o1", Name: "Acme"}, {ID: "o2", Name: "Globex"}},
		projects:        []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o1"}},
		orgsAndProjects: []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_member"}, {ID: "o2", Name: "Globex", Role: "org_member"}},
	}
	rec = orgGet(t, orgContextUIServer(&Server{cfg: sessionCfg(), memory: fNonAdmin}), "/orgs/o1")
	for _, absent := range []string{`id="transfer-project-modal"`, `action="/projects/transfer"`, `data-project-id="p1"`, `name="destinationOrgId"`} {
		if strings.Contains(rec.Body.String(), absent) {
			t.Errorf("non-admin org landing must not render the transfer dialog, found %q", absent)
		}
	}

	// Admin with no other org: no Transfer row action and no dialog.
	fSolo := &fakeMemory{
		orgs:            []Org{{ID: "o1", Name: "Acme"}},
		projects:        []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o1"}},
		orgsAndProjects: []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_admin"}},
	}
	rec = orgGet(t, orgContextUIServer(&Server{cfg: sessionCfg(), memory: fSolo}), "/orgs/o1")
	for _, absent := range []string{`id="transfer-project-modal"`, `action="/projects/transfer"`, `data-project-id="p1"`, `name="destinationOrgId"`} {
		if strings.Contains(rec.Body.String(), absent) {
			t.Errorf("solo-org admin org landing must not render the transfer dialog, found %q", absent)
		}
	}
}

// orgGet issues an authenticated (session) GET as org o1.
func orgGet(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(sessionCookie(t, "test-secret", sessionClaims{
		AccessToken: "at",
		OrgID:       "o1",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}
