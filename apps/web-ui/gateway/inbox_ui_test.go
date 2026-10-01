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

func TestSidebarGroupsIncludesInbox(t *testing.T) {
	for _, admin := range []bool{true, false} {
		found := false
		for _, g := range sidebarGroups(admin) {
			for _, item := range g.Items {
				if item.Href == "/inbox" {
					found = true
					if item.Icon != "lucide--inbox" {
						t.Errorf("inbox icon = %q, want lucide--inbox", item.Icon)
					}
				}
			}
		}
		if !found {
			t.Errorf("sidebarGroups(admin=%v) missing the Inbox entry", admin)
		}
	}
}

// TestLayoutSidebarActiveInbox asserts the active-state mechanism marks the
// inbox entry on /inbox.
func TestLayoutSidebarActiveInbox(t *testing.T) {
	groups := layout.ActiveSidebarGroups(sidebarGroups(false), "/inbox")
	for _, g := range groups {
		for _, item := range g.Items {
			if item.Href == "/inbox" && !item.Active {
				// A missing Active flag here means the nav would not highlight.
				t.Error("Inbox entry not marked active on /inbox")
			}
		}
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
