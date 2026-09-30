package main

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- Inbox page (web-inbox capability, task 9.x) ---

// activeProjectIDFromContext resolves the request's active project: the
// session's project when a session is attached, else the server's static
// MemoryProjectID (dev/API-key mode).
func activeProjectIDFromContext(ctx context.Context, fallback string) string {
	if sc, ok := sessionContextFrom(ctx); ok && sc.ProjectID != "" {
		return sc.ProjectID
	}
	return fallback
}

// Inbox tab keys. The server's lifecycle tabs (all/important/other/snoozed/
// cleared) are adapted to the inbox's own tabs: "unread" and "action" are list
// filters over the all tab, the rest map straight through.
const (
	inboxTabAll     = "all"
	inboxTabUnread  = "unread"
	inboxTabAction  = "action"
	inboxTabSnoozed = "snoozed"
	inboxTabCleared = "cleared"
)

// inboxTabDef is one tab in the inbox tab strip, in display order.
type inboxTabDef struct {
	Key   string
	Label string
	Icon  string
}

// inboxTabs is the ordered tab strip (spec: All / Unread / Action required /
// Snoozed / Cleared).
var inboxTabDefs = []inboxTabDef{
	{Key: inboxTabAll, Label: "All", Icon: "lucide--inbox"},
	{Key: inboxTabUnread, Label: "Unread", Icon: "lucide--circle-dot"},
	{Key: inboxTabAction, Label: "Action required", Icon: "lucide--hand"},
	{Key: inboxTabSnoozed, Label: "Snoozed", Icon: "lucide--alarm-clock"},
	{Key: inboxTabCleared, Label: "Cleared", Icon: "lucide--archive"},
}

// inboxTabFor normalizes a raw tab query value, defaulting to all.
func inboxTabFor(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case inboxTabUnread:
		return inboxTabUnread
	case inboxTabAction:
		return inboxTabAction
	case inboxTabSnoozed:
		return inboxTabSnoozed
	case inboxTabCleared:
		return inboxTabCleared
	default:
		return inboxTabAll
	}
}

// inboxView is the render state for InboxPage.
type inboxView struct {
	Scope string
	Tab   string
	// Notifications is the filtered list for the active scope+tab.
	Notifications []Notification
	// Counts is the per-tab summary for the active scope (nil when unavailable).
	Counts *NotificationCounts
	// Project is the active project (nil when none is selected).
	Project *Project
	// ProjectSelected is true when project scope has a resolvable project.
	ProjectSelected bool
	// LoadErr is a real list-load failure (Memory unreachable); empty states are
	// not errors.
	LoadErr  error
	FlashMsg string
	FlashErr error
}

// inboxListParams maps the inbox tab onto memory's list filters.
func inboxListParams(scope, projectID, tab string) NotificationListParams {
	p := NotificationListParams{Scope: scope, ProjectID: projectID}
	switch tab {
	case inboxTabUnread:
		p.Tab = "all"
		p.UnreadOnly = true
	case inboxTabAction:
		p.Tab = "all"
		p.RequiresAction = true
	case inboxTabSnoozed:
		p.Tab = "snoozed"
	case inboxTabCleared:
		p.Tab = "cleared"
	default:
		p.Tab = "all"
	}
	return p
}

// uiInbox renders the inbox page: scope switch, tabs, list, and empty states.
func (s *Server) uiInbox(c echo.Context) error {
	ctx := c.Request().Context()
	scope := notificationScope(c.QueryParam("scope"))
	tab := inboxTabFor(c.QueryParam("tab"))
	view := inboxView{
		Scope:    scope,
		Tab:      tab,
		FlashMsg: flashFromQuery(c, nil),
		FlashErr: flashError(c),
	}

	// Resolve the active project for the Project scope + its opt-in entry.
	if p, err := s.memory.GetCurrentProject(ctx); err == nil && p != nil {
		view.Project = p
	}
	if scope == "project" {
		if view.Project == nil {
			// No active project: the Project inbox cannot list anything. Render
			// the "select a project" empty state without a list call.
			return s.page(c, pageTitle("Inbox"), InboxPage(view))
		}
		view.ProjectSelected = true
	}

	projectID := ""
	if view.Project != nil {
		projectID = view.Project.ID
	}
	items, err := s.memory.ListNotifications(ctx, inboxListParams(scope, projectID, tab))
	if err != nil {
		view.LoadErr = err
		return s.page(c, pageTitle("Inbox"), InboxPage(view))
	}
	if items == nil {
		items = []Notification{}
	}
	view.Notifications = items

	// Counts are best-effort: a counts failure must not fail the list render.
	if counts, cerr := s.memory.NotificationCounts(ctx, scope, projectID); cerr != nil {
		captureError(cerr)
	} else {
		view.Counts = counts
	}
	return s.page(c, pageTitle("Inbox"), InboxPage(view))
}

// notificationUnreadTotal returns the bell's unread count: the account-scope
// unread plus, when a project is active, that project's unread. Best-effort —
// any failure degrades to 0 so a counts hiccup never blocks a page render.
func (s *Server) notificationUnreadTotal(ctx context.Context, projectID string) int {
	total := 0
	if c, err := s.memory.NotificationCounts(ctx, "account", ""); err == nil && c != nil {
		total += c.Unread
	}
	if projectID != "" {
		if c, err := s.memory.NotificationCounts(ctx, "project", projectID); err == nil && c != nil {
			total += c.Unread
		}
	}
	return total
}

// inboxTabHref builds a tab link preserving the active scope.
func inboxTabHref(scope, tab string) string {
	return "/inbox?" + url.Values{"scope": {scope}, "tab": {tab}}.Encode()
}

// inboxScopeHref builds a scope-switch link (resets to the all tab).
func inboxScopeHref(scope string) string {
	return "/inbox?" + url.Values{"scope": {scope}}.Encode()
}

// inboxScopeCount returns the unread count shown next to the scope switch's
// Project entry (account count is covered by the bell).
func (v inboxView) scopeUnread() int {
	if v.Counts == nil {
		return 0
	}
	return v.Counts.Unread
}

// --- Notification preferences page (project-inbox opt-in, task 9.3) ---

// notificationPrefsView is the render state for NotificationPreferencesPage.
type notificationPrefsView struct {
	Preferences []NotificationPreference
	LoadErr     error
	FlashMsg    string
	FlashErr    error
}

// uiNotificationPreferences renders the project-event opt-in page reachable
// from the Project inbox.
func (s *Server) uiNotificationPreferences(c echo.Context) error {
	view := notificationPrefsView{
		FlashMsg: flashFromQuery(c, []flashParam{{key: "saved", msg: "Notification preferences saved."}}),
		FlashErr: flashError(c),
	}
	prefs, err := s.memory.ListNotificationPreferences(c.Request().Context())
	if err != nil {
		view.LoadErr = err
		return s.page(c, pageTitle("Notification settings"), NotificationPreferencesPage(view))
	}
	sort.SliceStable(prefs, func(i, j int) bool { return prefs[i].Label < prefs[j].Label })
	view.Preferences = prefs
	return s.page(c, pageTitle("Notification settings"), NotificationPreferencesPage(view))
}

// uiNotificationPreferencesSave persists every submitted toggle and redirects
// back to the page (PRG). Each project-event key is submitted as
// "pref_<eventKey>" → "on"|"off"; required keys are never toggled off.
func (s *Server) uiNotificationPreferencesSave(c echo.Context) error {
	ctx := c.Request().Context()
	form, err := c.FormParams()
	if err != nil {
		return redirectWithError(c, "/inbox/preferences", err)
	}
	keys := make([]string, 0, len(form))
	for k := range form {
		if key, ok := strings.CutPrefix(k, "pref_"); ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		enabled := form.Get("pref_"+key) != ""
		if err := s.memory.SetNotificationPreference(ctx, key, "in_app", enabled); err != nil {
			return redirectWithError(c, "/inbox/preferences", err)
		}
	}
	return c.Redirect(http.StatusSeeOther, "/inbox/preferences?saved=1")
}
