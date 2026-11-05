package main

import (
	"context"
	"errors"
	"fmt"
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
	// ProjectNames maps a project id to its display name, for account-scope row
	// attribution (best-effort: an unresolved id renders no chip).
	ProjectNames map[string]string
	// ProjectFilter is the account-scope project filter (?project=), validated
	// against the fetched project list; "" means "all projects".
	ProjectFilter string
	// ProjectOptions is the ordered project list the account filter offers
	// ("All projects" + the caller's projects).
	ProjectOptions []ProjectRef
	// LoadErr is a real list-load failure (Memory unreachable); empty states are
	// not errors.
	LoadErr  error
	FlashMsg string
	// WarnMsg is the optional warning toast (e.g. a partial merge that left
	// objects on the branch for review).
	WarnMsg  string
	FlashErr error
}

// inboxProjectFilter returns the effective project filter for an inbox scope.
// The Project scope narrows to the active project. Everything and Account pass
// through the user's explicit project filter (?project=): "" lists every row in
// scope, a non-empty id narrows to that project. That filter is an explicit
// user choice, not the #1342 auto-narrow — that defect passed the ACTIVE
// project id for every scope, so a user with 11 account unread spread across 7
// projects saw only the 2 belonging to the active project while the bell (all
// account unread) read 11.
func inboxProjectFilter(scope string, project *Project, filter string) string {
	if scope == inboxScopeProject {
		if project == nil {
			return ""
		}
		return project.ID
	}
	return filter
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
//
// A request from the project filter form refreshes only the list region (plus
// the out-of-band tab strip, so counts stay current); the form pushes the URL,
// so the page around the list — and a reload — keep the filter. This reuses the
// same fragment the live inbox uses, so the two can never disagree.
func (s *Server) uiInbox(c echo.Context) error {
	view := s.inboxViewFor(c)
	if targetsResultsRegion(c.Request(), inboxResultsRegion) {
		renderPartial(c.Response().Writer, c.Request(), inboxListPartial(view))
		return nil
	}
	return s.page(c, pageTitle("Inbox"), InboxPage(view))
}

// uiInboxList renders /partial/inbox-list: the fragment the inbox JS swaps into
// #inbox-list on a live event or after an inline action, plus an out-of-band tab
// strip so per-tab counts stay current without replacing the page around them.
//
// It renders from inboxViewFor — the same builder the page uses — so the
// fragment can never disagree with the page it patches.
func (s *Server) uiInboxList(c echo.Context) error {
	renderPartial(c.Response().Writer, c.Request(), inboxListPartial(s.inboxViewFor(c)))
	return nil
}

// inboxViewFor builds the render state for one inbox request (page or list
// fragment): scope, tab, active project, project attribution/filter, the
// filtered list, and per-tab counts. Both render paths share it.
func (s *Server) inboxViewFor(c echo.Context) inboxView {
	ctx := c.Request().Context()
	scope := notificationScope(c.QueryParam("scope"))
	flashMsg, warnMsg, _ := mergeFlashFromQuery(c)
	view := inboxView{
		Scope:    scope,
		Tab:      inboxTabFor(c.QueryParam("tab")),
		FlashMsg: flashMsg,
		WarnMsg:  warnMsg,
		FlashErr: flashError(c),
	}

	// Resolve the active project for the Project scope + its opt-in entry.
	if p, err := s.memory.GetCurrentProject(ctx); err == nil && p != nil {
		view.Project = p
	}
	if scope == inboxScopeProject {
		if view.Project == nil {
			// No active project: the Project inbox cannot list anything. The
			// view carries ProjectSelected=false so both the page and the list
			// fragment render the "select a project" state without a list call.
			return view
		}
		view.ProjectSelected = true
	}

	// Projects for row attribution and the project filter. Everything and
	// Account both list rows from several projects, so both need the name map
	// and the option list. Best-effort: a failure leaves an empty name map and
	// option list, so rows render without a chip and the filter offers only
	// "All projects". The list itself must still render.
	if scope != inboxScopeProject {
		if projects, err := s.memory.ListProjects(ctx); err == nil {
			view.ProjectOptions = projects
			view.ProjectNames = make(map[string]string, len(projects))
			for _, p := range projects {
				view.ProjectNames[p.ID] = p.Name
			}
			// Validate ?project= against the fetched list (fail closed: an
			// unknown id is ignored, leaving the account inbox unfiltered).
			if raw := strings.TrimSpace(c.QueryParam("project")); raw != "" {
				for _, p := range projects {
					if p.ID == raw {
						view.ProjectFilter = raw
						break
					}
				}
			}
		}
	}

	projectID := inboxProjectFilter(scope, view.Project, view.ProjectFilter)
	items, err := s.memory.ListNotifications(ctx, inboxListParams(notificationUpstreamScope(scope), projectID, view.Tab))
	if err != nil {
		view.LoadErr = userFacingError(err)
		return view
	}
	if items == nil {
		items = []Notification{}
	}
	view.Notifications = items

	// Counts are best-effort: a counts failure must not fail the list render.
	if counts, cerr := s.memory.NotificationCounts(ctx, notificationUpstreamScope(scope), projectID); cerr != nil {
		captureError(cerr)
	} else {
		view.Counts = counts
	}
	return view
}

// uiOrgInbox lands an org-scoped inbox entry point on the org's project inbox.
//
// The inbox is a project-scoped page: its rows deep-link into project routes
// (/chat, /settings/approvals, /documents/…), which the render-time scope
// rewriter prefixes with the page base. An org-only scope carries no project
// ref and the org route tree has no inbox, so /o/{org}/inbox — the topbar
// bell's target while an org page is showing — used to resolve to
// /orgs/{org}/inbox and 404. Resolve the org's default project and redirect
// into its inbox instead, so the bell works from every page; an org with no
// projects returns to the org landing, where one can be created.
func (s *Server) uiOrgInbox(c echo.Context) error {
	orgID, projectID, ok := s.orgInboxTarget(c)
	if !ok {
		return s.scopeNotFound(c)
	}
	if projectID == "" {
		return c.Redirect(http.StatusSeeOther, orgScopeURL(orgID))
	}
	target := projectScopeURL(orgID, projectID) + "/inbox"
	if q := c.Request().URL.RawQuery; q != "" {
		target += "?" + q
	}
	return c.Redirect(http.StatusSeeOther, target)
}

// orgInboxTarget resolves the org an org-scoped inbox request names and the
// project it should land on. The org ref is validated against the caller's
// access tree (a legacy, unscoped /orgs/{id}/inbox request never passed
// through applyScope); ok=false is the same fail-closed 404 an unknown scope
// gets. A project id of "" means the org exposes no projects to the caller.
func (s *Server) orgInboxTarget(c echo.Context) (orgID, projectID string, ok bool) {
	tree, err := s.orgAccessTree(c)
	if err != nil {
		captureError(err)
		return "", "", false
	}
	orgID, _, ok = resolveScopeRefs(tree, c.Param("id"), "")
	if !ok {
		return "", "", false
	}
	var recent []string
	if claims, has := s.readRecentProjects(c); has {
		if sc, hasSession := sessionContextFrom(c.Request().Context()); hasSession {
			recent = claims.Recent[sc.Sub]
		}
	}
	return orgID, orgInboxProject(tree, recent, orgID), true
}

// orgInboxProject picks the project an org-scoped inbox lands on: the caller's
// most recently used project that belongs to orgID (skipping recents from other
// orgs and stale ids), else the org's first project in access-tree order. ""
// when the org exposes no projects. Kept pure so the ordering rule is testable
// without an HTTP round trip.
func orgInboxProject(tree []OrgWithProjectsDto, recent []string, orgID string) string {
	var projects []ProjectAccessDto
	for _, o := range tree {
		if o.ID == orgID {
			projects = o.Projects
			break
		}
	}
	if len(projects) == 0 {
		return ""
	}
	for _, id := range recent {
		for _, p := range projects {
			if p.ID == id {
				return p.ID
			}
		}
	}
	return projects[0].ID
}

// inboxBellScope resolves the inbox scope a request is displaying, so the bell
// can count exactly the inbox it links to. Only the inbox route carries a
// scope; every other page defaults to the Everything inbox — the one the bell
// lands on. Project scope resolves the active project and never falls back to a
// different scope.
//
// Two regressions live in this function's history. #1342: the bell summed the
// account and project scopes while the inbox listed only one, so a user with 9
// project unread and 2 account unread saw a bell of 11 over an inbox of 2.
// #document-lifecycle: the default scope was account, so project activity (a
// document uploaded by a teammate) never ticked the bell on any other page.
// Both are fixed by the same invariant — the bell counts the scope the inbox
// lists, and the default inbox now lists everything.
func (s *Server) inboxBellScope(c echo.Context) (scope, projectID string) {
	scope = inboxScopeAll
	if c.Path() == "/inbox" {
		scope = notificationScope(c.QueryParam("scope"))
	}
	if scope == inboxScopeProject {
		projectID = activeProjectIDFromContext(c.Request().Context(), s.cfg.MemoryProjectID)
	}
	return scope, projectID
}

// notificationUnread returns the bell's unread count for one inbox scope: the
// number of unread rows the inbox list for that scope will show. It is the ONLY
// number the bell renders, so the badge can never disagree with the list it
// links to. Best-effort — any failure degrades to 0 so a counts hiccup never
// blocks a page render. Project scope with no resolvable project has no inbox
// to list, so it counts as zero rather than falling back to every project.
func (s *Server) notificationUnread(ctx context.Context, scope, projectID string) int {
	if scope == inboxScopeProject && projectID == "" {
		return 0
	}
	c, err := s.memory.NotificationCounts(ctx, notificationUpstreamScope(scope), projectID)
	if err != nil || c == nil {
		return 0
	}
	return c.Unread
}

// inboxFilterHref builds an inbox link preserving the scope, tab, and account
// project filter. The project param is dropped when empty, so project-scope
// tabs and an unfiltered account inbox keep a clean URL.
func inboxFilterHref(scope, tab, project string) string {
	q := url.Values{"scope": {scope}, "tab": {tab}}
	if project != "" {
		q.Set("project", project)
	}
	return "/inbox?" + q.Encode()
}

// inboxScopeHref builds a scope-switch link (resets to the all tab and drops
// the project filter). Everything is the default scope, so its link is the bare
// inbox URL.
func inboxScopeHref(scope string) string {
	if scope == inboxScopeAll {
		return "/inbox"
	}
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
	// Project is the active project whose preferences are being managed (nil
	// when no project is selected).
	Project     *Project
	Preferences []NotificationPreference
	LoadErr     error
	FlashMsg    string
	FlashErr    error
}

// uiNotificationPreferences renders the project-event opt-in page reachable
// from the Project inbox. Preferences are project-scoped: the page resolves the
// active project and reads that project's effective preferences.
func (s *Server) uiNotificationPreferences(c echo.Context) error {
	ctx := c.Request().Context()
	view := notificationPrefsView{
		FlashMsg: flashFromQuery(c, []flashParam{{key: "saved", msg: "Notification preferences saved."}}),
		FlashErr: flashError(c),
	}
	if p, err := s.memory.GetCurrentProject(ctx); err == nil && p != nil {
		view.Project = p
	}
	if view.Project == nil {
		// No active project: nothing to scope preferences to. Render the
		// select-a-project empty state instead of an inert global toggle list.
		return s.page(c, pageTitle("Notification settings"), NotificationPreferencesPage(view))
	}
	prefs, err := s.memory.ListNotificationPreferences(ctx, view.Project.ID)
	if err != nil {
		view.LoadErr = userFacingError(err)
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
	// Preferences are project-scoped. Prefer the project carried by the form;
	// fall back to the active project so a direct POST still resolves.
	projectID := strings.TrimSpace(form.Get("project_id"))
	if projectID == "" {
		if p, perr := s.memory.GetCurrentProject(ctx); perr == nil && p != nil {
			projectID = p.ID
		}
	}
	if projectID == "" {
		return redirectWithError(c, "/inbox/preferences", errors.New("no active project selected"))
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
		if err := s.memory.SetNotificationPreference(ctx, projectID, key, "in_app", enabled); err != nil {
			return redirectWithError(c, "/inbox/preferences", err)
		}
	}
	return c.Redirect(http.StatusSeeOther, "/inbox/preferences?saved=1")
}

// --- extraction review merge (inbox CTA, task: extraction review UI) ---

// uiInboxMergePreview renders the merge preview modal for a review-ready inbox
// notification: the server's predicted split before the user confirms. The
// document and job ids come from the notification's details payload and travel
// on the lazy preview URL. It shares mergePreviewModal with the document flow.
func (s *Server) uiInboxMergePreview(c echo.Context) error {
	ctx := c.Request().Context()
	documentID := strings.TrimSpace(c.QueryParam("document"))
	jobID := strings.TrimSpace(c.QueryParam("job"))
	actionURL := inboxMergeActionURL(documentID, jobID)

	job, err := s.findExtractionJob(ctx, documentID, jobID)
	if err != nil {
		renderPartial(c.Response().Writer, c.Request(), mergePreviewModal(actionURL, nil, userFacingError(err)))
		return nil
	}
	branchID, _, ok := s.extractionJobBranch(ctx, job)
	if !ok {
		renderPartial(c.Response().Writer, c.Request(), mergePreviewModal(actionURL, nil, fmt.Errorf("this extraction has no staging branch to merge")))
		return nil
	}
	res, err := s.memory.PreviewMergeBranch(ctx, branchID, "partial")
	if err != nil {
		renderPartial(c.Response().Writer, c.Request(), mergePreviewModal(actionURL, nil, userFacingError(err)))
		return nil
	}
	renderPartial(c.Response().Writer, c.Request(), mergePreviewModal(actionURL, res, nil))
	return nil
}

// uiInboxMerge merges the staging branch of a review-ready notification's
// extraction job, resolves the branch's review-ready notifications, and
// PRG-redirects to the inbox with the truthful result query (?merged=N, plus
// ?review=M when the merge was partial).
func (s *Server) uiInboxMerge(c echo.Context) error {
	ctx := c.Request().Context()
	documentID := strings.TrimSpace(c.QueryParam("document"))
	jobID := strings.TrimSpace(c.QueryParam("job"))

	res, branchID, err := s.mergeExtractionBranch(ctx, documentID, jobID)
	if err != nil {
		return redirectWithError(c, "/inbox", err)
	}
	// Resolve the branch's review-ready notifications only when the merge closed
	// the branch (nothing left for review, no conflicts) — the same predicate the
	// server uses to stamp merged_at. On a partial merge the branch stays open
	// with objects still to review, so the reminder must stay actionable and the
	// user retries after handling the conflicts. Best-effort: a resolve failure
	// must not hide the merge result.
	if res != nil && res.LeftForReviewCount == 0 && res.ConflictCount == 0 {
		if err := s.memory.ResolveNotificationsForResource(ctx, "graph_branch", branchID); err != nil {
			captureError(err)
		}
	}
	redirectAfterMutation(c, "/inbox"+mergeResultQuery(res, ""))
	return nil
}
