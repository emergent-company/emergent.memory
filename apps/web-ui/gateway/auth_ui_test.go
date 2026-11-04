package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// switcherTrigger returns the switcher's trigger-button HTML (identified by
// data-testid="project-switcher") so tests can assert what the trigger shows
// independently of the dropdown rows.
func switcherTrigger(html string) string {
	i := strings.Index(html, `data-testid="project-switcher"`)
	if i < 0 {
		return ""
	}
	j := strings.Index(html[i:], "</button>")
	if j < 0 {
		return ""
	}
	return html[i : i+j]
}

// switcherMenu returns the dropdown <ul role="menu">…</ul> region of the
// rendered switcher.
func switcherMenu(html string) string {
	i := strings.Index(html, `role="menu"`)
	if i < 0 {
		return ""
	}
	start := strings.LastIndex(html[:i], "<ul")
	if start < 0 {
		return ""
	}
	end := strings.Index(html[i:], "</ul>")
	if end < 0 {
		return ""
	}
	return html[start : i+end+len("</ul>")]
}

// pickerDialogHTML returns the mobile picker <dialog>…</dialog> region so tests
// can assert on the mobile presentation without matching the desktop menu.
func pickerDialogHTML(html string) string {
	i := strings.Index(html, `data-testid="project-picker-modal"`)
	if i < 0 {
		return ""
	}
	start := strings.LastIndex(html[:i], "<dialog")
	if start < 0 {
		return ""
	}
	end := strings.Index(html[i:], "</dialog>")
	if end < 0 {
		return ""
	}
	return html[start : i+end+len("</dialog>")]
}

// TestRenderLoginPage asserts the sign-in page renders as a standalone dark
// document: brand, exact copy, and a "Sign in" action pointing at /auth/start
// (which performs the real OIDC redirect).
func TestRenderLoginPage(t *testing.T) {
	html := renderHTML(t, loginPage("", "development", 0, 0, 0, ""))
	for _, want := range []string{
		"<title>Sign in — Memory</title>",
		"Sign in to Memory",
		"Continue with your Memory account",
		"Sign in",
		`href="/auth/start"`,
		`data-theme="dark"`,
		"memory-monogram",
		"theme-color",
		"background-color:#0E1017",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("login page missing %q", want)
		}
	}
	// standalone document: no app shell chrome (no sidebar/navbar)
	for _, absent := range []string{"_layout-topbar", "main-content", "toast-queue"} {
		if strings.Contains(html, absent) {
			t.Errorf("login page must not render app-shell chrome (%q)", absent)
		}
	}

	// Sentry DSN set → the loader script is included; unset → not.
	if h := renderHTML(t, loginPage("", "development", 0, 0, 0, "")); strings.Contains(h, "browser.sentry-cdn.com") {
		t.Error("empty Sentry DSN must not inject the Sentry loader")
	}
	if h := renderHTML(t, loginPage("https://dsn.example/1", "prod", 0, 0, 0, "")); !strings.Contains(h, "browser.sentry-cdn.com") {
		t.Error("configured Sentry DSN should inject the Sentry loader")
	}
}

// TestRenderLoginPageAuthModeRedirect is a behavior guard for the authLogin
// handler: in dev mode it must 302 home instead of rendering (covered at the
// HTTP layer in oidc_test.go), so here we only assert the page template is a
// full document the handler can render in session mode.
func TestRenderLoginPageIsFullDocument(t *testing.T) {
	html := renderHTML(t, loginPage("", "development", 0, 0, 0, ""))
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(html)), "<!doctype html>") {
		t.Error("login page must be a standalone <!DOCTYPE html> document")
	}
}

// switcherTestData returns a representative org/project mix for render tests:
// two orgs (Acme o1, Globex o2), two projects in Acme, one in Globex, one
// orphan project (unknown org, must land under "Other"), with Home (o1) as the
// current project.
func switcherTestData() (current *ProjectRef, currentOrgName string, groups []projectGroup, orgs []Org) {
	orgs = []Org{
		{ID: "o1", Name: "Acme"},
		{ID: "o2", Name: "Globex"},
	}
	projects := []ProjectRef{
		{ID: "p1", Name: "Home", OrgID: "o1"},
		{ID: "p2", Name: "Lab", OrgID: "o1"},
		{ID: "p3", Name: "Main", OrgID: "o2"},
		{ID: "p4", Name: "Orphan", OrgID: "o9"},
	}
	current = &projects[0]
	currentOrgName = orgNameFor(orgs, current.OrgID)
	groups = groupProjectsByOrg(projects, orgs)
	return current, currentOrgName, groups, orgs
}

// projNames joins project names for compact equality assertions.
func projNames(ps []ProjectRef) string {
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name)
	}
	return strings.Join(names, ",")
}

// TestGroupProjectsByOrg covers the grouping rules: org order preserved, orgs
// with no projects omitted, projects with an unknown or empty OrgID collected
// into a trailing "Other" group (order preserved), and empty input → nil.
func TestGroupProjectsByOrg(t *testing.T) {
	orgs := []Org{
		{ID: "o1", Name: "Acme"},
		{ID: "o2", Name: "Globex"},
		{ID: "o3", Name: "NoProjectsHere"},
	}
	projects := []ProjectRef{
		{ID: "p1", Name: "Home", OrgID: "o1"},
		{ID: "p2", Name: "Lab", OrgID: "o1"},
		{ID: "p3", Name: "Main", OrgID: "o2"},
		{ID: "p4", Name: "Orphan", OrgID: "o9"}, // unknown org
		{ID: "p5", Name: "Loose", OrgID: ""},    // org-less
	}
	got := groupProjectsByOrg(projects, orgs)

	if len(got) != 4 {
		t.Fatalf("len = %d, want 4 (Acme, Globex, NoProjectsHere, Other): %+v", len(got), got)
	}
	// group order follows org order; empty org o3 included (project-less orgs
	// stay reachable so the user can open them and create a project)
	if got[0].OrgID != "o1" || got[0].OrgName != "Acme" || projNames(got[0].Projects) != "Home,Lab" {
		t.Errorf("group[0] = %+v, want Acme with Home,Lab in order", got[0])
	}
	if got[1].OrgID != "o2" || got[1].OrgName != "Globex" || projNames(got[1].Projects) != "Main" {
		t.Errorf("group[1] = %+v, want Globex with Main", got[1])
	}
	if got[2].OrgID != "o3" || got[2].OrgName != "NoProjectsHere" || len(got[2].Projects) != 0 {
		t.Errorf("group[2] = %+v, want NoProjectsHere with no projects", got[2])
	}
	// trailing Other: unknown org + org-less, input order preserved
	if got[3].OrgID != "" || got[3].OrgName != "" || projNames(got[3].Projects) != "Orphan,Loose" {
		t.Errorf("group[3] = %+v, want Other with Orphan,Loose", got[3])
	}

	// every project appears exactly once across the groups
	var total int
	for _, g := range got {
		total += len(g.Projects)
	}
	if total != len(projects) {
		t.Errorf("total projects across groups = %d, want %d", total, len(projects))
	}

	// all orgs known → no Other group (empty orgs still yield a header group)
	all := []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o1"}, {ID: "p3", Name: "Main", OrgID: "o2"}}
	got = groupProjectsByOrg(all, orgs)
	if len(got) != 3 || got[1].OrgName == "" {
		t.Errorf("fully-known projects should yield org groups only, got %+v", got)
	}

	// empty input → nil
	if got := groupProjectsByOrg(nil, orgs); got != nil {
		t.Errorf("empty projects should yield nil, got %+v", got)
	}

	// no orgs at all → every project lands in a single Other group
	got = groupProjectsByOrg(projects, nil)
	if len(got) != 1 || got[0].OrgID != "" || got[0].OrgName != "" || len(got[0].Projects) != len(projects) {
		t.Errorf("no orgs should yield one Other group with all projects, got %+v", got)
	}
}

// TestOrgNameFor covers org-id → name resolution (known, unknown, empty).
func TestOrgNameFor(t *testing.T) {
	orgs := []Org{{ID: "o1", Name: "Acme"}, {ID: "o2", Name: "Globex"}}
	for _, c := range []struct {
		orgID string
		want  string
	}{
		{"o1", "Acme"},
		{"o2", "Globex"},
		{"o9", ""},
		{"", ""},
	} {
		if got := orgNameFor(orgs, c.orgID); got != c.want {
			t.Errorf("orgNameFor(%q) = %q, want %q", c.orgID, got, c.want)
		}
	}
}

// TestRenderProjectSwitcherGroups asserts the grouped dropdown: org headers in
// order, each project under its own org, the orphan under "Other", the active
// check on the current project, and the two-line (org over project) trigger label. Also
// keeps the flat-list guarantees (activate forms per project, New-project
// affordance, create-modal org options).
func TestRenderProjectSwitcherGroups(t *testing.T) {
	current, currentOrgName, groups, orgs := switcherTestData()
	html := renderHTML(t, projectSwitcher(current, currentOrgName, nil, groups, orgs, nil, false))

	// trigger shows org on the first line, project on the second, never the placeholder
	trigger := switcherTrigger(html)
	if !strings.Contains(trigger, "Acme") || !strings.Contains(trigger, "Home") {
		t.Errorf("trigger should show org %q and project %q, got %q", "Acme", "Home", trigger)
	}
	if strings.Contains(trigger, "Select project") {
		t.Error("placeholder must not render when a project is active")
	}

	// dropdown: headers + rows, org headers in org order, Other trailing
	menu := switcherMenu(html)
	if menu == "" {
		t.Fatal("menu region not found")
	}
	iAcme := strings.Index(menu, ">Acme<")
	iGlobex := strings.Index(menu, ">Globex<")
	iOther := strings.Index(menu, ">Other<")
	if iAcme < 0 || iGlobex < 0 || iOther < 0 {
		t.Fatalf("org headers missing from menu (Acme@%d Globex@%d Other@%d)", iAcme, iGlobex, iOther)
	}
	if iAcme >= iGlobex || iGlobex >= iOther {
		t.Error("headers must render in org order with Other trailing")
	}

	segAcme := menu[iAcme:iGlobex]
	for _, want := range []string{"Home", "Lab", `href="/o/o1/p/p1/agents"`, `href="/o/o1/p/p2/agents"`} {
		if !strings.Contains(segAcme, want) {
			t.Errorf("Acme group missing %q", want)
		}
	}
	if strings.Contains(segAcme, "Main") || strings.Contains(segAcme, "Orphan") {
		t.Error("Acme group must not contain other orgs' projects")
	}
	segGlobex := menu[iGlobex:iOther]
	if !strings.Contains(segGlobex, "Main") || strings.Contains(segGlobex, "Home") {
		t.Errorf("Globex group should contain Main only, got %q", segGlobex)
	}
	segOther := menu[iOther:]
	if !strings.Contains(segOther, "Orphan") || strings.Contains(segOther, "Main") {
		t.Errorf("Other group should contain the orphan project, got %q", segOther)
	}

	// rows are scoped links (one per project), not activate forms
	if strings.Contains(menu, `action="/projects/activate"`) {
		t.Error("project rows must not use the legacy activate form")
	}
	if n := strings.Count(menu, `role="menuitem"`); n != 4 {
		t.Errorf("want 4 project links (one per project), got %d", n)
	}

	// exactly one active check per rendered list, on the current project row:
	// the desktop menu and the mobile picker render the same shared list, so
	// scope the count to the dropdown.
	if got := strings.Count(menu, "lucide--check"); got != 1 {
		t.Errorf("want exactly 1 active check in the dropdown, got %d", got)
	}
	// create-modal form fields + org options still present
	for _, want := range []string{
		"New project", `action="/projects"`, `name="name"`, `name="orgId"`,
		"Project name", "Organization",
		`<option value="o1">Acme</option>`, `<option value="o2">Globex</option>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("project switcher missing %q", want)
		}
	}
	if strings.Contains(html, "No projects yet") {
		t.Error("'No projects yet' must not render when projects exist")
	}
}

// TestRenderProjectSwitcherEmpty asserts the no-project state: placeholder
// trigger, muted empty line, the New-project affordance still present, and the
// create form hinting at the missing-org case instead of an org dropdown.
func TestRenderProjectSwitcherEmpty(t *testing.T) {
	html := renderHTML(t, projectSwitcher(nil, "", nil, nil, nil, nil, false))

	for _, want := range []string{
		"Select project",
		"No projects yet",
		"New project",
		"Project name",
		"name=\"name\"",
		"action=\"/projects\"",
		"You need an organization first.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("empty project switcher missing %q", want)
		}
	}
	for _, absent := range []string{
		"Main", "value=\"p1\"", `name="orgId"`, "Select an organization",
		">Other<", ">Acme<",
	} {
		if strings.Contains(html, absent) {
			t.Errorf("empty project switcher must not contain %q", absent)
		}
	}
}

// TestRenderProjectSwitcherNoCurrent asserts the placeholder trigger renders
// when there are projects but none is the active one (org headers still show).
func TestRenderProjectSwitcherNoCurrent(t *testing.T) {
	projects := []ProjectRef{{ID: "p1", Name: "Main", OrgID: "o1"}}
	orgs := []Org{{ID: "o1", Name: "Acme"}}
	groups := groupProjectsByOrg(projects, orgs)
	html := renderHTML(t, projectSwitcher(nil, "", nil, groups, orgs, nil, false))
	if !strings.Contains(switcherTrigger(html), "Select project") {
		t.Error("placeholder trigger should render when no project is active")
	}
	if !strings.Contains(switcherMenu(html), ">Acme<") {
		t.Error("org header should render even without an active project")
	}
	if !strings.Contains(html, `<option value="o1">Acme</option>`) {
		t.Error("org dropdown should list the orgs when orgs exist")
	}
	if strings.Contains(html, "No projects yet") {
		t.Error("'No projects yet' must not render when projects exist")
	}
}

// TestRenderProjectSwitcherTriggerOrgKnown asserts the trigger label includes
// the org when the current project's org resolves.
func TestRenderProjectSwitcherTriggerOrgKnown(t *testing.T) {
	projects := []ProjectRef{
		{ID: "p1", Name: "Main", OrgID: "o1"},
		{ID: "p2", Name: "Home", OrgID: "o2"},
	}
	orgs := []Org{{ID: "o1", Name: "Acme"}, {ID: "o2", Name: "Globex"}}
	groups := groupProjectsByOrg(projects, orgs)
	html := renderHTML(t, projectSwitcher(&projects[1], orgNameFor(orgs, "o2"), nil, groups, orgs, nil, false))
	trigger := switcherTrigger(html)
	if !strings.Contains(trigger, "Globex") || !strings.Contains(trigger, "Home") {
		t.Errorf("trigger should show org %q and project %q, got %q", "Globex", "Home", trigger)
	}
	if strings.Contains(trigger, "Select project") {
		t.Error("trigger must not show the placeholder when a project is active")
	}
}

// TestRenderProjectSwitcherTriggerOrgUnknown asserts the trigger falls back to
// the bare project name when the current project's org is unknown.
func TestRenderProjectSwitcherTriggerOrgUnknown(t *testing.T) {
	projects := []ProjectRef{{ID: "p1", Name: "Lone", OrgID: "o9"}}
	orgs := []Org{{ID: "o1", Name: "Acme"}}
	groups := groupProjectsByOrg(projects, orgs)
	html := renderHTML(t, projectSwitcher(&projects[0], orgNameFor(orgs, "o9"), nil, groups, orgs, nil, false))
	trigger := switcherTrigger(html)
	if !strings.Contains(trigger, ">Lone<") {
		t.Errorf("trigger should show the bare project name, got %q", trigger)
	}
	if strings.Contains(trigger, " / ") {
		t.Errorf("trigger must not show an org separator when the org is unknown, got %q", trigger)
	}
	if !strings.Contains(switcherMenu(html), ">Other<") {
		t.Error("unknown-org project should render under the Other header")
	}
}

// TestRenderProjectSwitcherMobilePicker pins the mobile presentation. On a
// 390px viewport the focus dropdown (dropdown-end + min-w-80) started at
// x = -52px, clipping every org header and project name, so below md the
// switcher opens a full-screen dialog instead: a md:hidden trigger, a card with
// a close X, and the same rows/footer as the dropdown (shared components, so
// they cannot drift).
func TestRenderProjectSwitcherMobilePicker(t *testing.T) {
	current, currentOrgName, groups, orgs := switcherTestData()
	html := renderHTML(t, projectSwitcher(current, currentOrgName, nil, groups, orgs, nil, false))

	// desktop keeps the dropdown, but only from md up
	if !strings.Contains(html, "md:block") {
		t.Error("desktop dropdown must be desktop-only (hidden md:block)")
	}
	if !strings.Contains(html, "md:hidden") {
		t.Fatal("mobile trigger must render below md (md:hidden)")
	}
	// one mobile trigger, wired to the picker rather than the focus dropdown
	if n := strings.Count(html, `data-testid="project-switcher-mobile"`); n != 1 {
		t.Fatalf("want exactly 1 mobile switcher trigger, got %d", n)
	}
	if !strings.Contains(html, `onclick="openProjectPicker()"`) {
		t.Error("mobile trigger must call openProjectPicker()")
	}
	if !strings.Contains(html, `aria-controls="project-picker-modal"`) {
		t.Error("mobile trigger must point at the dialog it opens (aria-controls)")
	}

	dialog := pickerDialogHTML(html)
	if dialog == "" {
		t.Fatal("mobile picker dialog missing")
	}
	for _, want := range []string{
		`id="project-picker-modal"`,
		`aria-label="Close project picker"`, // the X has an accessible name
		`data-testid="project-picker-close"`,
		`onclick="closeProjectPicker()"`,  // wired to the helper
		"h-dvh", "w-screen", "max-w-none", // full-page card, not a floating box
		`role="menu"`,
		"Acme", "Globex", "Other", // org headers, Other trailing
		`href="/o/o1/p/p1/agents"`, // scoped row links, same as the dropdown
		"Home", "Lab", "Main", "Orphan",
		"New project", "New org", // shared footer actions
	} {
		if !strings.Contains(dialog, want) {
			t.Errorf("mobile picker missing %q", want)
		}
	}
	// absolute positioning off the trigger is exactly the bug, so the dialog
	// must never inherit the dropdown-content class
	if strings.Contains(dialog, "dropdown-content") {
		t.Error("picker dialog must not use the absolutely-positioned dropdown-content")
	}
	// one active check per list, on the current project
	if got := strings.Count(dialog, "lucide--check"); got != 1 {
		t.Errorf("want exactly 1 active check in the picker, got %d", got)
	}
}

// TestRenderProjectSwitcherEmptyOrgRow pins the per-org empty state: an
// organization with no projects keeps its header and shows an italic "No
// projects" line under it, instead of a bare header that reads like a broken
// section. Same shared list, so the dropdown and the card both get it.
func TestRenderProjectSwitcherEmptyOrgRow(t *testing.T) {
	orgs := []Org{{ID: "o1", Name: "Acme"}, {ID: "o2", Name: "Empty Org"}}
	projects := []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o1"}}
	groups := groupProjectsByOrg(projects, orgs)
	html := renderHTML(t, projectSwitcher(&projects[0], "Acme", nil, groups, orgs, nil, false))

	dialog := pickerDialogHTML(html)
	if dialog == "" {
		t.Fatal("mobile picker dialog missing")
	}
	iOrg := strings.Index(dialog, "Empty Org")
	if iOrg < 0 {
		t.Fatal("org without projects must still render its header")
	}
	rest := dialog[iOrg:]
	if !strings.Contains(rest, "No projects") {
		t.Fatal("org without projects must show a No projects line")
	}
	if !strings.Contains(rest, "italic") {
		t.Error("the No projects line must be italic")
	}
	if n := strings.Count(dialog, ">No projects<"); n != 1 {
		t.Errorf("want exactly 1 No projects line in the picker, got %d", n)
	}
}

// TestRenderProjectSwitcherScriptNotInMenu is the regression test for the
// visible-JS-blob bug: daisyUI's `.menu li > *` selector turns any direct
// <script> child of a menu <li> into a grid item (display:grid overrides the
// UA display:none), painting the function source inside the dropdown. The
// modal open/close functions must therefore live at the END of the component
// — never inside the <ul role="menu">.
func TestRenderProjectSwitcherScriptNotInMenu(t *testing.T) {
	current, currentOrgName, groups, orgs := switcherTestData()
	html := renderHTML(t, projectSwitcher(current, currentOrgName, nil, groups, orgs, nil, false))

	// no <script> anywhere inside the dropdown menu markup
	menu := switcherMenu(html)
	if menu == "" {
		t.Fatal("menu region not found")
	}
	if strings.Contains(menu, "<script") {
		t.Error("menu must not contain a <script> element (daisyUI renders it as a visible grid blob)")
	}
	// the New-project row is a plain anchor child of its <li>
	if !strings.Contains(menu, "<a") || !strings.Contains(menu, "New project") {
		t.Error("New-project row missing from the menu")
	}

	// the functions are defined once, in a script AFTER the dialog closes
	openIdx := strings.Index(html, "function openNewProjectModal()")
	closeIdx := strings.Index(html, "function closeNewProjectModal()")
	if openIdx < 0 || closeIdx < 0 {
		t.Fatal("open/close project-modal functions missing from the component")
	}
	if dialogIdx := strings.LastIndex(html, "</dialog>"); openIdx < dialogIdx {
		t.Error("modal script must be emitted after the dialog, not inside the menu")
	}
	// controls wire to the plain globals via onclick attributes (the New-project
	// row is an <a href="#"> with return false, the close buttons are <button>)
	for _, want := range []string{
		`onclick="openNewProjectModal(); return false;"`,
		`onclick="closeNewProjectModal()"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q wiring", want)
		}
	}
}

// TestProjectSwitcherDevModeActiveProject covers the ui.go fallback: with no
// session context (dev / API-key mode) the shell resolves the active project
// from cfg.MemoryProjectID and renders the two-line org-over-project trigger plus the
// grouped dropdown.
func TestProjectSwitcherDevModeActiveProject(t *testing.T) {
	f := &fakeMemory{
		orgs:     []Org{{ID: "o1", Name: "Acme"}},
		projects: []ProjectRef{{ID: "memory", Name: "memory", OrgID: "o1"}},
	}
	s := &Server{cfg: Config{MemoryProjectID: "memory"}, memory: f}
	e := sessionServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dev /agents = %d, want 200", rec.Code)
	}
	trigger := switcherTrigger(rec.Body.String())
	if !strings.Contains(trigger, "Acme") || !strings.Contains(trigger, "memory") {
		t.Errorf("dev-mode trigger should show org %q and project %q, got %q", "Acme", "memory", trigger)
	}
	if strings.Contains(trigger, "Select project") {
		t.Error("dev-mode trigger must not show the placeholder when MemoryProjectID matches a project")
	}
	if !strings.Contains(rec.Body.String(), ">Acme<") {
		t.Error("grouped dropdown should render the Acme org header for the known-org project")
	}
}

// TestProjectSwitcherSessionStillWins asserts the session's active project
// takes precedence over cfg.MemoryProjectID when both are present (session
// mode).
func TestProjectSwitcherSessionStillWins(t *testing.T) {
	f := &fakeMemory{
		orgs:     []Org{{ID: "o1", Name: "Acme"}},
		projects: []ProjectRef{{ID: "p1", Name: "Main", OrgID: "o1"}},
	}
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret", MemoryProjectID: "p1"}, memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	addSessionCookie(t, req, "test-secret", sessionClaims{
		AccessToken:     "at",
		ActiveProjectID: "p1",
		OrgID:           "o1",
		ExpiresAt:       time.Now().Add(time.Hour).Unix(),
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session /agents = %d, want 200", rec.Code)
	}
	if trigger := switcherTrigger(rec.Body.String()); !strings.Contains(trigger, "Acme") || !strings.Contains(trigger, "Main") {
		t.Errorf("session-mode trigger should show org %q and project %q, got %q", "Acme", "Main", trigger)
	}
}
