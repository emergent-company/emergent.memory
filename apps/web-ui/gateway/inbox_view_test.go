package main

import (
	"errors"
	"testing"
)

// --- Inbox presentation helper tests (inbox_view.go) ---

func TestNotificationProjectLabel(t *testing.T) {
	d := inboxView{
		Scope:        "account",
		ProjectNames: map[string]string{"p1": "Acme"},
	}
	for _, tc := range []struct {
		name   string
		scope  string
		projID string
		want   string
	}{
		{"account resolves the project name", "account", "p1", "Acme"},
		{"account unknown id omits the chip", "account", "ghost", ""},
		{"account no project id omits the chip", "account", "", ""},
		{"everything resolves the project name", "all", "p1", "Acme"},
		{"everything unknown id omits the chip", "all", "ghost", ""},
		{"project scope never attributes", "project", "p1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := d
			d.Scope = tc.scope
			if got := notificationProjectLabel(d, Notification{ProjectID: tc.projID}); got != tc.want {
				t.Errorf("notificationProjectLabel(scope=%q, projectId=%q) = %q, want %q", tc.scope, tc.projID, got, tc.want)
			}
		})
	}
}

// TestInboxShowsTabs pins the shared page/partial branch: the strip renders
// only for a scope that can list (not Project-without-a-project) and only when
// the list load succeeded, so a fragment swap cannot add or drop it.
func TestInboxShowsTabs(t *testing.T) {
	errBoom := errors.New("boom")
	cases := []struct {
		name string
		view inboxView
		want bool
	}{
		{"everything", inboxView{Scope: inboxScopeAll}, true},
		{"account", inboxView{Scope: inboxScopeAccount}, true},
		{"project selected", inboxView{Scope: inboxScopeProject, ProjectSelected: true}, true},
		{"project without project", inboxView{Scope: inboxScopeProject}, false},
		{"load error", inboxView{Scope: inboxScopeAll, LoadErr: errBoom}, false},
	}
	for _, tc := range cases {
		if got := inboxShowsTabs(tc.view); got != tc.want {
			t.Errorf("%s: inboxShowsTabs = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestInboxTabCountActionRequired asserts the Action-required badge reads the
// count the API now returns; before the server sent requiresAction the badge
// was permanently hidden over a non-empty tab.
func TestInboxTabCountActionRequired(t *testing.T) {
	counts := &NotificationCounts{All: 4, RequiresAction: 3}
	if n, show := inboxTabCount(counts, inboxTabAction); !show || n != 3 {
		t.Errorf("action tab count = (%d,%v), want (3,true)", n, show)
	}
	empty := &NotificationCounts{All: 4}
	if _, show := inboxTabCount(empty, inboxTabAction); show {
		t.Error("action tab must not render a badge when the count is zero")
	}
}
