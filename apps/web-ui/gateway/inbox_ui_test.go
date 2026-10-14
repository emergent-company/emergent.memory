package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/emergent-company/go-daisy/components/layout"
	"github.com/labstack/echo/v4"
)

// --- Inbox page render tests (task 9.x) ---

func TestInboxPageAccountEmpty(t *testing.T) {
	html := renderHTML(t, InboxPage(inboxView{Scope: "account", Tab: "all", Notifications: []Notification{}}))
	for _, want := range []string{
		"data-inbox-page",
		`data-scope="account"`,
		`data-tab="all"`,
		`data-inbox-scope="account"`,
		`data-inbox-scope="project"`,
		`data-inbox-tab="all"`,
		`data-inbox-tab="unread"`,
		`data-inbox-tab="action"`,
		`data-inbox-tab="snoozed"`,
		`data-inbox-tab="cleared"`,
		"All caught up",
		"Mark all read",
		"data-inbox-action=\"mark-all-read\"",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("account empty inbox missing %q", want)
		}
	}
}

func TestInboxPageProjectNoProject(t *testing.T) {
	html := renderHTML(t, InboxPage(inboxView{Scope: "project", Tab: "all"}))
	if !strings.Contains(html, "No project selected") {
		t.Error("project scope without a project must render the select-a-project empty state")
	}
	if !strings.Contains(html, `data-project-id=""`) {
		t.Error("project scope with no project must render an empty data-project-id")
	}
	if strings.Contains(html, "data-inbox-tabs") {
		t.Error("the tab strip must not render when no project is selected")
	}
}

func TestInboxPageProjectOptInEmpty(t *testing.T) {
	d := inboxView{
		Scope:           "project",
		Tab:             "all",
		Project:         &Project{ID: "p1", Name: "Acme"},
		ProjectSelected: true,
		Counts:          &NotificationCounts{All: 0},
		Notifications:   []Notification{},
	}
	html := renderHTML(t, InboxPage(d))
	if !strings.Contains(html, "No project activity yet") {
		t.Error("empty project inbox with no delivered items must point at the opt-in preferences")
	}
	if !strings.Contains(html, `href="/inbox/preferences"`) {
		t.Error("opt-in empty state must link to the preferences page")
	}
	if !strings.Contains(html, `data-project-id="p1"`) {
		t.Error("project inbox must expose the active project id for the stream")
	}
}

func TestNotificationPreferencesPageProjectScoped(t *testing.T) {
	d := notificationPrefsView{
		Project:     &Project{ID: "p1", Name: "Acme"},
		Preferences: []NotificationPreference{{EventKey: "comment.reply", Enabled: true}},
	}
	html := renderHTML(t, NotificationPreferencesPage(d))
	if !strings.Contains(html, `name="project_id"`) || !strings.Contains(html, `value="p1"`) {
		t.Error("preferences form must carry the scoped project id")
	}
	if !strings.Contains(html, `name="pref_comment.reply"`) {
		t.Error("preferences form must render the event toggle")
	}
}

func TestNotificationPreferencesPageNoProject(t *testing.T) {
	html := renderHTML(t, NotificationPreferencesPage(notificationPrefsView{}))
	if !strings.Contains(html, "No project selected") {
		t.Error("preferences page with no active project must render the select-a-project empty state")
	}
	if strings.Contains(html, `name="project_id"`) {
		t.Error("preferences page with no active project must not render a form")
	}
}

func TestInboxPageProjectGenericEmpty(t *testing.T) {
	d := inboxView{
		Scope: "project", Tab: "cleared", Project: &Project{ID: "p1", Name: "Acme"},
		ProjectSelected: true, Counts: &NotificationCounts{All: 4, Cleared: 0}, Notifications: []Notification{},
	}
	html := renderHTML(t, InboxPage(d))
	if strings.Contains(html, "No project activity yet") {
		t.Error("a non-empty project inbox with an empty tab must not show the opt-in empty state")
	}
	if !strings.Contains(html, "Nothing has been cleared yet") {
		t.Errorf("cleared tab empty copy missing")
	}
}

func TestInboxPageActionableRow(t *testing.T) {
	n := Notification{
		ID: "n1", Scope: "project", Title: "Invitation to Acme", Message: "You're invited",
		RequiresAction: true, Read: false,
		Actions:   []NotificationAction{{Label: "Accept", Value: "accept"}, {Label: "Decline", Value: "decline"}},
		CreatedAt: "2026-09-20T10:00:00Z",
	}
	html := renderHTML(t, InboxPage(inboxView{
		Scope: "project", Tab: "action", Project: &Project{ID: "p1"}, ProjectSelected: true,
		Notifications: []Notification{n},
	}))
	for _, want := range []string{
		`data-notification-id="n1"`,
		`data-notification-unread="true"`,
		`data-notif-action="resolve"`,
		`data-notif-verb="accept"`,
		`data-notif-verb="decline"`,
		"Accept",
		"Decline",
		"Action",
		"New",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("actionable row missing %q", want)
		}
	}
}

// TestInboxPageActionableRowDeepLink is the #1362 regression: an agent question
// that needs input must be clickable through to where it can be answered and
// must carry the notification id so the client marks it read on click.
func TestInboxPageActionableRowDeepLink(t *testing.T) {
	n := Notification{
		ID: "n-input", Scope: "project", Title: "Agent needs your input", Message: "Which fruit?",
		RequiresAction: true, Read: false,
		ActionURL: "/chat?c=conv-1", ActionLabel: "Review",
		CreatedAt: "2026-09-30T21:19:00Z",
	}
	html := renderHTML(t, InboxPage(inboxView{
		Scope: "project", Tab: "action", Project: &Project{ID: "p1"}, ProjectSelected: true,
		Notifications: []Notification{n},
	}))
	for _, want := range []string{
		`data-notification-open`,
		`href="/chat?c=conv-1"`,
		`data-notif-id="n-input"`,
		"Agent needs your input",
		"Which fruit?",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("actionable deep-linked row missing %q", want)
		}
	}
	if strings.Count(html, "data-notification-open") != 1 {
		t.Errorf("row must expose exactly one open target, got %d", strings.Count(html, "data-notification-open"))
	}
	if !strings.Contains(html, `data-notif-action="read"`) {
		t.Error("row must keep the explicit mark-read control")
	}
}

func TestInboxPageNonActionableOpenLink(t *testing.T) {
	n := Notification{
		ID: "n2", Scope: "account", Title: "Role changed", Read: true,
		ActionURL: "/settings/members", ActionLabel: "View members",
	}
	html := renderHTML(t, InboxPage(inboxView{Scope: "account", Tab: "all", Notifications: []Notification{n}}))
	if !strings.Contains(html, "data-notification-open") {
		t.Error("non-actionable notification with a target must render an open link")
	}
	if !strings.Contains(html, `href="/settings/members"`) {
		t.Error("open link must use the notification's action URL")
	}
	if strings.Contains(html, `data-notif-action="resolve"`) {
		t.Error("non-actionable notification must not render resolve buttons")
	}
	if strings.Contains(html, ">New<") {
		t.Error("read notification must not render the New badge")
	}
}

func TestInboxPageTabsAndCounts(t *testing.T) {
	d := inboxView{
		Scope: "account", Tab: "all",
		Counts:        &NotificationCounts{All: 5, Unread: 2, Snoozed: 1, Cleared: 3, RequiresAction: 1},
		Notifications: []Notification{{ID: "n1", Title: "x", Read: true}},
	}
	html := renderHTML(t, InboxPage(d))
	for _, want := range []string{">5<", ">2<", ">1<", ">3<"} {
		if !strings.Contains(html, want) {
			t.Errorf("tab counts missing badge %q", want)
		}
	}
}

// TestInboxPageProjectChip asserts account-scope rows attribute their
// originating project when the id resolves, and omit the chip otherwise; the
// project scope never renders the chip (its scope switch already names the
// project).
func TestInboxPageProjectChip(t *testing.T) {
	account := inboxView{
		Scope:        "account",
		Tab:          "all",
		ProjectNames: map[string]string{"p1": "Acme"},
		Notifications: []Notification{
			{ID: "n1", Scope: "account", ProjectID: "p1", Title: "Added to project"},
			{ID: "n2", Scope: "account", ProjectID: "ghost", Title: "Unknown project"},
		},
	}
	html := renderHTML(t, InboxPage(account))
	if !strings.Contains(html, "Acme") {
		t.Error("account row with a resolvable project id must render a project chip")
	}
	if strings.Count(html, "Acme") != 1 {
		t.Errorf("project chip must render exactly once, got %d", strings.Count(html, "Acme"))
	}

	projectScope := inboxView{
		Scope: "project", Tab: "all",
		Project:         &Project{ID: "p1", Name: "Acme"},
		ProjectSelected: true,
		ProjectNames:    map[string]string{"p1": "Acme"},
		Notifications:   []Notification{{ID: "n1", Scope: "project", ProjectID: "p1", Title: "Added to project"}},
	}
	if got := renderHTML(t, InboxPage(projectScope)); strings.Contains(got, "Acme") {
		t.Error("project scope must not repeat the project label as a chip")
	}
}

// TestInboxPageProjectFilter asserts the account scope renders the project
// filter and the project scope does not.
func TestInboxPageProjectFilter(t *testing.T) {
	account := inboxView{
		Scope:          "account",
		Tab:            "all",
		ProjectFilter:  "p1",
		ProjectOptions: []ProjectRef{{ID: "p1", Name: "Acme"}, {ID: "p2", Name: "Globex"}},
		Notifications:  []Notification{},
	}
	html := renderHTML(t, InboxPage(account))
	for _, want := range []string{
		"data-inbox-project-filter",
		`name="project"`,
		">All projects<",
		">Acme<",
		">Globex<",
		`value="p1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("account inbox filter missing %q", want)
		}
	}

	projectScope := inboxView{
		Scope: "project", Tab: "all",
		Project:         &Project{ID: "p1", Name: "Acme"},
		ProjectSelected: true,
		ProjectOptions:  []ProjectRef{{ID: "p1", Name: "Acme"}},
		Notifications:   []Notification{},
	}
	if got := renderHTML(t, InboxPage(projectScope)); strings.Contains(got, "data-inbox-project-filter") {
		t.Error("project scope must not render the project filter")
	}
}

// --- Bell render tests (task 8.1) ---

func TestNotificationsBellCount(t *testing.T) {
	html := renderHTML(t, notificationsBell(3))
	for _, want := range []string{
		`data-testid="notifications-bell"`,
		`data-notifications-bell`,
		`data-unread="3"`,
		`data-notifications-badge`,
		`href="/inbox"`,
		">3<",
		"Inbox, 3 unread",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("bell with count missing %q", want)
		}
	}
}

func TestNotificationsBellNoCount(t *testing.T) {
	html := renderHTML(t, notificationsBell(0))
	if strings.Contains(html, "data-notifications-badge") {
		t.Error("zero unread must not render an indicator badge")
	}
	if !strings.Contains(html, `data-testid="notifications-bell"`) || !strings.Contains(html, `href="/inbox"`) {
		t.Error("bell must still render (and link to /inbox) with zero unread")
	}
}

func TestNotificationsBellCapsAtNinePlus(t *testing.T) {
	html := renderHTML(t, notificationsBell(42))
	if !strings.Contains(html, ">9+<") {
		t.Errorf("large unread counts must cap at 9+, got %s", html)
	}
}

// TestAppShellRendersBell asserts the unread count threads into the shell's
// navbar bell.
func TestAppShellRendersBell(t *testing.T) {
	html := renderHTML(t, appShell(
		"Inbox", nil, false, nil, "", nil, "", nil, nil, nil, nil, false,
		7,
		&currentUser{Name: "Ada", Sub: "sub-a"}, nil, templ.NopComponent, nil,
		"", "", 0, 0, 0, "",
	))
	if !strings.Contains(html, `data-notifications-bell`) {
		t.Fatal("shell is missing the notifications bell")
	}
	if !strings.Contains(html, `data-unread="7"`) || !strings.Contains(html, "data-notifications-badge") {
		t.Error("shell bell must show the unread count")
	}
}

// TestInboxProjectFilter guards the #1342 root cause: the Account inbox is
// global and must not be auto-narrowed to the active project. The defect passed
// the active project id for every scope, so a user with 11 account unread
// spread across 7 projects saw only the 2 belonging to the active project,
// while the bell (all account unread) read 11. The account scope's project id
// is now the user's explicit filter, not the active project.
func TestInboxProjectFilter(t *testing.T) {
	p := &Project{ID: "active-project"}
	for _, tc := range []struct {
		name   string
		scope  string
		proj   *Project
		filter string
		want   string
	}{
		{"account is global", "account", p, "", ""},
		{"account passes explicit filter", "account", p, "proj-x", "proj-x"},
		{"account with no project", "account", nil, "", ""},
		{"project narrows", "project", p, "proj-x", "active-project"},
		{"project with no project", "project", nil, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inboxProjectFilter(tc.scope, tc.proj, tc.filter); got != tc.want {
				t.Errorf("inboxProjectFilter(%q, %v, %q) = %q, want %q", tc.scope, tc.proj, tc.filter, got, tc.want)
			}
		})
	}
}

// TestUIInboxScopeProjectFilter drives the real inbox handler and asserts the
// account scope forwards NO project filter to the list or counts, while the
// project scope forwards the active project. This is the end-to-end guard for
// the #1342 defect (the account inbox must show account notifications from
// every project, not just the active one) and for the bell agreeing with the
// account list.
func TestUIInboxScopeProjectFilter(t *testing.T) {
	f := &fakeMemory{
		project:            &Project{ID: "active-project", Name: "Active"},
		notificationCounts: &NotificationCounts{Unread: 11},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := withSessionContext(c.Request().Context(), &sessionContext{Token: "tok", Sub: "sub-a", ProjectID: "active-project"})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})
	e.GET("/inbox", s.uiInbox)

	serve := func(path string) string {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, body=%s", path, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	// Account scope: global — no project filter, on the list, the counts, or the
	// bell (which must not be narrowed to the active project).
	body := serve("/inbox?scope=account")
	if f.lastListParams.Scope != "account" || f.lastListParams.ProjectID != "" {
		t.Errorf("account list forwarded scope/project = %q/%q, want account/''", f.lastListParams.Scope, f.lastListParams.ProjectID)
	}
	if f.lastCountsScope != "account" || f.lastCountsProject != "" {
		t.Errorf("account counts forwarded scope/project = %q/%q, want account/''", f.lastCountsScope, f.lastCountsProject)
	}
	if !strings.Contains(body, `data-unread="11"`) {
		t.Error("account bell must count all account unread (11), not the active project's slice")
	}

	// Project scope: narrowed to the active project.
	serve("/inbox?scope=project")
	if f.lastListParams.Scope != "project" || f.lastListParams.ProjectID != "active-project" {
		t.Errorf("project list forwarded scope/project = %q/%q, want project/active-project", f.lastListParams.Scope, f.lastListParams.ProjectID)
	}
	if f.lastCountsScope != "project" || f.lastCountsProject != "active-project" {
		t.Errorf("project counts forwarded scope/project = %q/%q, want project/active-project", f.lastCountsScope, f.lastCountsProject)
	}
}

// TestUIInboxAccountProjectFilter drives the real inbox handler and asserts the
// account scope forwards an explicit, validated ?project= filter to the list
// and counts, and ignores unknown values (fail closed).
func TestUIInboxAccountProjectFilter(t *testing.T) {
	f := &fakeMemory{
		project:            &Project{ID: "active-project", Name: "Active"},
		projects:           []ProjectRef{{ID: "p1", Name: "Acme"}, {ID: "p2", Name: "Globex"}},
		notificationCounts: &NotificationCounts{Unread: 3},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// No Sub: page() skips the bell's own counts call, so the recorded
			// counts are exactly the ones uiInbox forwarded for the list.
			ctx := withSessionContext(c.Request().Context(), &sessionContext{Token: "tok"})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})
	e.GET("/inbox", s.uiInbox)

	serve := func(path string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}

	// A known project id narrows the account list and counts to that project.
	serve("/inbox?scope=account&project=p1")
	if f.lastListParams.Scope != "account" || f.lastListParams.ProjectID != "p1" {
		t.Errorf("account list forwarded scope/project = %q/%q, want account/p1", f.lastListParams.Scope, f.lastListParams.ProjectID)
	}
	if f.lastCountsScope != "account" || f.lastCountsProject != "p1" {
		t.Errorf("account counts forwarded scope/project = %q/%q, want account/p1", f.lastCountsScope, f.lastCountsProject)
	}

	// An unknown project id is ignored (fail closed): the account inbox stays
	// unfiltered.
	serve("/inbox?scope=account&project=does-not-exist")
	if f.lastListParams.ProjectID != "" || f.lastCountsProject != "" {
		t.Errorf("unknown project must be ignored; list/counts project = %q/%q, want ''/''", f.lastListParams.ProjectID, f.lastCountsProject)
	}
}

// TestBellUnreadMatchesInboxScope is the #1342 regression: the bell badge must
// equal the unread count of the inbox list for the active scope, never the sum
// of the account and project scopes. With 2 account unread and 9 project unread
// the account inbox shows 2, so its bell must read 2 (the pre-fix code summed
// both and read 11); the project inbox shows 9, so its bell must read 9.
func TestBellUnreadMatchesInboxScope(t *testing.T) {
	f := &fakeMemory{notificationCountsByScope: map[string]*NotificationCounts{
		"account|":   {Unread: 2},
		"project|p1": {Unread: 9},
	}}
	s := &Server{memory: f}

	if got := s.notificationUnread(t.Context(), "account", ""); got != 2 {
		t.Errorf("account-scope bell = %d, want 2 (its inbox unread, not the 11-across-scopes sum)", got)
	}
	if got := s.notificationUnread(t.Context(), "project", "p1"); got != 9 {
		t.Errorf("project-scope bell = %d, want 9 (its inbox unread)", got)
	}
}

// TestNotificationUnreadProjectWithoutProject asserts a project-scoped inbox
// with no resolvable project counts as zero (there is no list to agree with)
// and does not fall back to querying every project's unread.
func TestNotificationUnreadProjectWithoutProject(t *testing.T) {
	f := &fakeMemory{notificationCountsByScope: map[string]*NotificationCounts{
		"project|": {Unread: 9},
	}}
	s := &Server{memory: f}

	if got := s.notificationUnread(t.Context(), "project", ""); got != 0 {
		t.Errorf("project bell without a project = %d, want 0", got)
	}
	if f.lastCountsScope != "" {
		t.Errorf("must not query counts for a projectless project scope; queried %q/%q", f.lastCountsScope, f.lastCountsProject)
	}
}

// TestInboxBellScope asserts the bell follows the inbox's scope on /inbox and
// defaults to the account inbox everywhere else, so the badge always describes
// the inbox the bell links to.
func TestInboxBellScope(t *testing.T) {
	s := &Server{cfg: Config{MemoryProjectID: "p1"}, memory: &fakeMemory{}}
	e := echo.New()
	var scope, project string
	capture := func(c echo.Context) error {
		scope, project = s.inboxBellScope(c)
		return c.NoContent(http.StatusOK)
	}
	e.GET("/inbox", capture)
	e.GET("/agents", capture)

	for _, tc := range []struct {
		path, wantScope, wantProject string
	}{
		{"/inbox", "account", ""},
		{"/inbox?scope=account", "account", ""},
		{"/inbox?scope=project", "project", "p1"},
		{"/agents?scope=project", "account", ""},
	} {
		scope, project = "", ""
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		e.ServeHTTP(httptest.NewRecorder(), req)
		if scope != tc.wantScope || project != tc.wantProject {
			t.Errorf("inboxBellScope(%s) = %q/%q, want %q/%q", tc.path, scope, project, tc.wantScope, tc.wantProject)
		}
	}
}

// --- Nav render test (task 8.3) ---

// TestSidebarGroupsOmitsInbox asserts the Inbox entry was removed from the
// project sidebar: the account inbox is cross-project and is reached from the
// topbar inbox entry, not a project-scoped nav item.
func TestSidebarGroupsOmitsInbox(t *testing.T) {
	for _, admin := range []bool{true, false} {
		for _, g := range sidebarGroups("", admin) {
			for _, item := range g.Items {
				if item.Href == "/inbox" {
					t.Errorf("sidebarGroups(admin=%v) still contains the Inbox entry", admin)
				}
			}
		}
	}
}

// TestLayoutSidebarActiveApprovals asserts the active-state mechanism still
// marks a remaining top-level entry now that the Inbox entry is gone.
func TestLayoutSidebarActiveApprovals(t *testing.T) {
	groups := layout.ActiveSidebarGroups(sidebarGroups("", false), "/settings/approvals")
	found := false
	for _, g := range groups {
		for _, item := range g.Items {
			if item.Href == "/settings/approvals" && item.Active {
				found = true
			}
		}
	}
	if !found {
		t.Error("Approvals entry not marked active on /settings/approvals")
	}
}

// --- pure presentation helpers ---

func TestHumanizeEventKey(t *testing.T) {
	for in, want := range map[string]string{
		"project.member.added": "Project member added",
		"task_assigned":        "Task assigned",
		"invite-received":      "Invite received",
		"":                     "",
	} {
		if got := humanizeEventKey(in); got != want {
			t.Errorf("humanizeEventKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInboxTabCount(t *testing.T) {
	c := &NotificationCounts{All: 5, Unread: 2, Snoozed: 1, Cleared: 3, RequiresAction: 0}
	if n, ok := inboxTabCount(c, inboxTabAll); n != 5 || !ok {
		t.Errorf("all = %d, %v", n, ok)
	}
	if n, ok := inboxTabCount(c, inboxTabUnread); n != 2 || !ok {
		t.Errorf("unread = %d, %v", n, ok)
	}
	if _, ok := inboxTabCount(c, inboxTabAction); ok {
		t.Error("a zero action-required count should not render a badge")
	}
	if _, ok := inboxTabCount(nil, inboxTabAll); ok {
		t.Error("nil counts should not render badges")
	}
	withAction := &NotificationCounts{RequiresAction: 3}
	if n, ok := inboxTabCount(withAction, inboxTabAction); n != 3 || !ok {
		t.Errorf("action = %d, %v", n, ok)
	}
}

func TestBellUnreadLabel(t *testing.T) {
	if bellUnreadLabel(0) != "0" || bellUnreadLabel(9) != "9" || bellUnreadLabel(10) != "9+" {
		t.Errorf("bellUnreadLabel: got %q/%q/%q", bellUnreadLabel(0), bellUnreadLabel(9), bellUnreadLabel(10))
	}
}

func TestNotificationWhen(t *testing.T) {
	if got := notificationWhen("2026-09-20T10:00:00Z"); !strings.Contains(got, "2026") {
		t.Errorf("notificationWhen formatted = %q", got)
	}
	if got := notificationWhen("not-a-date"); got != "not-a-date" {
		t.Errorf("unparseable timestamp must pass through, got %q", got)
	}
	if got := notificationWhen(""); got != "" {
		t.Errorf("empty timestamp = %q", got)
	}
}
