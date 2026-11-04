package main

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// --- Inbox presentation helpers (pure, unit-testable) ---

// notificationWhen formats an RFC3339 timestamp for the inbox row. An
// unparseable value passes through unchanged.
func notificationWhen(ts string) string {
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Local().Format("2 Jan 2006, 15:04")
}

// notificationIcon picks a leading lucide icon from the notification's
// category/source, falling back to a bell.
func notificationIcon(n Notification) string {
	switch strings.ToLower(n.Category) {
	case "invite", "invitation", "invites":
		return "lucide--mail"
	case "approval", "approvals":
		return "lucide--shield-check"
	case "mention", "mentions", "comment", "comments":
		return "lucide--at-sign"
	case "task", "tasks", "assignment":
		return "lucide--check-square"
	case "membership", "project", "project_member":
		return "lucide--folder-plus"
	case "permission", "permissions", "role", "access":
		return "lucide--key-round"
	case "budget", "usage", "billing":
		return "lucide--chart-column"
	case "question", "agent_question":
		return "lucide--bot"
	case "extraction", "extractions", "extraction_review", "review":
		return "lucide--git-branch"
	case "document", "documents":
		return "lucide--file-text"
	}
	if n.Scope == "project" {
		return "lucide--activity"
	}
	return "lucide--bell"
}

// notificationProjectLabel returns the originating project's display name for
// an inbox row, or "" when there is nothing to attribute: the row carries no
// project id, the scope is the project inbox (it already labels its project in
// the scope switch), or the id is not in the fetched project list (best-effort
// attribution degrades to no chip). Everything lists rows from several
// projects, so it attributes exactly like Account does.
func notificationProjectLabel(d inboxView, n Notification) string {
	if d.Scope == inboxScopeProject || n.ProjectID == "" {
		return ""
	}
	return d.ProjectNames[n.ProjectID]
}

// notificationMeta builds the muted metadata line: when + category + optional
// source. Empty segments are dropped.
func notificationMeta(n Notification) string {
	parts := make([]string, 0, 3)
	if when := notificationWhen(n.CreatedAt); when != "" {
		parts = append(parts, when)
	}
	if label := humanizeEventKey(cmpOr(n.Category, n.EventKey)); label != "" {
		parts = append(parts, label)
	}
	if n.SourceType != "" {
		parts = append(parts, humanizeEventKey(n.SourceType))
	}
	return strings.Join(parts, " · ")
}

// humanizeEventKey turns a dotted/snake event key ("project.member.added")
// into a readable label ("Project member added"). Empty stays empty.
func humanizeEventKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	replacer := strings.NewReplacer(".", " ", "_", " ", "-", " ")
	words := strings.Fields(replacer.Replace(key))
	if len(words) == 0 {
		return ""
	}
	words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	return strings.Join(words, " ")
}

// notificationActionClass maps an action's verb/label to a button intent:
// destructive/resolving actions outline, affirmative ones fill.
func notificationActionClass(a NotificationAction) string {
	v := strings.ToLower(a.Value + " " + a.Label)
	switch {
	case strings.Contains(v, "decline"), strings.Contains(v, "deny"), strings.Contains(v, "reject"):
		return "btn-outline"
	case strings.Contains(v, "accept"), strings.Contains(v, "approve"), strings.Contains(v, "resolve"):
		return "btn-primary"
	default:
		return "btn-outline"
	}
}

// inboxShowsTabs reports whether the tab strip (and the project filter above it)
// renders for this view. The Project scope without a resolvable project, and a
// failed list load, render only the list region's own state — the page and the
// list fragment both branch on this so a swap cannot add or drop the strip.
func inboxShowsTabs(d inboxView) bool {
	if d.LoadErr != nil {
		return false
	}
	return d.Scope != inboxScopeProject || d.ProjectSelected
}

// inboxTabCount returns the badge count for one tab and whether it should be
// shown. The action-required count is optional (the counts endpoint may omit
// it), so it only shows when non-zero; all other tabs show whenever counts are
// available.
func inboxTabCount(counts *NotificationCounts, key string) (int, bool) {
	if counts == nil {
		return 0, false
	}
	switch key {
	case inboxTabAll:
		return counts.All, true
	case inboxTabUnread:
		return counts.Unread, true
	case inboxTabAction:
		return counts.RequiresAction, counts.RequiresAction > 0
	case inboxTabSnoozed:
		return counts.Snoozed, true
	case inboxTabCleared:
		return counts.Cleared, true
	default:
		return 0, false
	}
}

// inboxProjectAllOptional reports whether the Project inbox is empty because
// nothing has been opted into (no delivered items at all), so the empty state
// can point at the preferences instead of implying there is simply nothing new.
func inboxProjectAllOptional(d inboxView) bool {
	if d.Scope != "project" || d.Counts == nil {
		return false
	}
	if d.Tab == inboxTabSnoozed || d.Tab == inboxTabCleared {
		return false
	}
	return d.Counts.All == 0
}

// inboxEmptyDescription is the tab-aware empty-state copy.
func inboxEmptyDescription(d inboxView) string {
	switch d.Tab {
	case inboxTabUnread:
		return "No unread notifications in this inbox."
	case inboxTabAction:
		return "Nothing needs your action right now."
	case inboxTabSnoozed:
		return "No snoozed notifications."
	case inboxTabCleared:
		return "Nothing has been cleared yet."
	default:
		if d.Scope == "project" {
			return "No project activity to show."
		}
		return "New membership, permission, and invitation events land here."
	}
}

// inboxProjectID returns the active project id for the page's data attribute,
// or "" when none is selected.
func inboxProjectID(d inboxView) string {
	if d.Project == nil {
		return ""
	}
	return d.Project.ID
}

// bellUnreadLabel caps the bell count at "9+".
func bellUnreadLabel(n int) string {
	if n > 9 {
		return "9+"
	}
	return strconv.Itoa(n)
}

// bellAriaLabel describes the bell for assistive tech, including the count.
func bellAriaLabel(n int) string {
	if n > 0 {
		return "Inbox, " + strconv.Itoa(n) + " unread"
	}
	return "Inbox"
}

// preferenceLabel prefers the server-provided label, else humanizes the key.
func preferenceLabel(p NotificationPreference) string {
	if strings.TrimSpace(p.Label) != "" {
		return p.Label
	}
	return humanizeEventKey(p.EventKey)
}

// cmpOr returns a when non-empty, else b.
func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// inboxReviewMerge extracts the extraction-review payload from a review-ready
// notification: the document id and job id whose staging branch the user can
// merge. ok is false when the notification carries no such payload, so the row
// renders no "Review merge" action.
func inboxReviewMerge(n Notification) (documentID, jobID string, ok bool) {
	documentID = notificationDetailsString(n.Details, "document_id")
	jobID = notificationDetailsString(n.Details, "job_id")
	if documentID == "" || jobID == "" {
		return "", "", false
	}
	return documentID, jobID, true
}

// notificationDetailsString reads a string out of a notification's details
// payload, tolerating a nil map and non-string values (both degrade to "").
func notificationDetailsString(details map[string]any, key string) string {
	if details == nil {
		return ""
	}
	if v, ok := details[key].(string); ok {
		return v
	}
	return ""
}

// inboxMergePreviewHref builds the lazy merge-preview URL for a review-ready
// notification's document/job pair.
func inboxMergePreviewHref(documentID, jobID string) string {
	return "/inbox/merge-preview?" + url.Values{"document": {documentID}, "job": {jobID}}.Encode()
}

// inboxMergeActionURL builds the POST target for the inbox merge confirm (the
// actionURL the shared mergePreviewModal posts to).
func inboxMergeActionURL(documentID, jobID string) string {
	return "/inbox/merge?" + url.Values{"document": {documentID}, "job": {jobID}}.Encode()
}
