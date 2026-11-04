package main

import "testing"

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
