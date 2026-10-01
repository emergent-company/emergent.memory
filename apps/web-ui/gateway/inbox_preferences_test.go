package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestSetNotificationPreferenceForwardsProject is the gateway-side regression
// for inert opt-in preferences: the preference a user toggles on the project
// preferences page must be written for the project being managed, so the
// server's project-keyed delivery lookup can match it. The gateway previously
// dropped the project id and wrote project_id NULL.
func TestSetNotificationPreferenceForwardsProject(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{memory: f}
	e := notificationsEcho(s)

	rec := doNotif(t, e, http.MethodPut, "/api/notifications/preferences",
		`{"projectId":"proj-1","eventKey":"comment.reply","enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(f.notificationMutations) != 1 {
		t.Fatalf("mutations = %v", f.notificationMutations)
	}
	got := f.notificationMutations[0]
	if !strings.HasPrefix(got, "pref:proj-1:comment.reply:") {
		t.Errorf("preference write = %q, want project-scoped pref:proj-1:comment.reply:…", got)
	}
}

// TestListNotificationPreferencesForwardsProject asserts the project id the
// page is scoped to reaches the upstream preference lookup.
func TestListNotificationPreferencesForwardsProject(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{memory: f}
	e := notificationsEcho(s)

	rec := doNotif(t, e, http.MethodGet, "/api/notifications/preferences?project_id=proj-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", rec.Code)
	}
	if f.lastPrefsProject != "proj-1" {
		t.Errorf("upstream project = %q, want proj-1", f.lastPrefsProject)
	}
}
