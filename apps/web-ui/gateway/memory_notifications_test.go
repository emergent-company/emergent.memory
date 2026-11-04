package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- notification client methods (memory_notifications.go) ---

// notifSessCtx returns a context carrying a session (token + project + org) so
// outbound notification calls carry the bearer and scoping headers.
func notifSessCtx() context.Context {
	return withSessionContext(context.Background(), &sessionContext{
		Token:     "sess-token",
		ProjectID: "sess-project",
		OrgID:     "sess-org",
	})
}

func TestListNotificationsParamsAndDecode(t *testing.T) {
	var gotPath, gotMethod, gotAuth, gotProj, gotOrg string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotAuth = r.Header.Get("Authorization")
		gotProj = r.Header.Get("X-Project-ID")
		gotOrg = r.Header.Get("X-Org-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[
			{"id":"n1","scope":"project","projectId":"p1","title":"Assigned","message":"You were assigned","requiresAction":true,"read":false,"actions":[{"label":"Accept","value":"accept"}],"createdAt":"2026-09-20T10:00:00Z"}
		]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static-project")
	items, err := m.ListNotifications(notifSessCtx(), NotificationListParams{
		Scope:          "project",
		ProjectID:      "p1",
		Tab:            "snoozed",
		Category:       "approval",
		UnreadOnly:     true,
		RequiresAction: true,
		Search:         "assigned",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/notifications" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer sess-token" || gotProj != "sess-project" || gotOrg != "sess-org" {
		t.Errorf("headers auth=%q proj=%q org=%q", gotAuth, gotProj, gotOrg)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	n := items[0]
	if n.ID != "n1" || n.Scope != "project" || !n.RequiresAction || n.Read || len(n.Actions) != 1 || n.Actions[0].Value != "accept" {
		t.Errorf("decoded item = %+v", n)
	}
}

// TestListNotificationsQuery asserts every filter maps to the expected query
// param.
func TestListNotificationsQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static")
	if _, err := m.ListNotifications(context.Background(), NotificationListParams{
		Scope: "project", ProjectID: "p1", Tab: "snoozed", Category: "approval",
		UnreadOnly: true, RequiresAction: true, Search: "x y",
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"scope=project", "project_id=p1", "tab=snoozed", "category=approval",
		"unread_only=true", "requires_action=true", "search=x+y",
	} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestListNotificationsBareArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"n1","title":"t","scope":"account"}]`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static")
	items, err := m.ListNotifications(context.Background(), NotificationListParams{Scope: "account"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "n1" {
		t.Fatalf("bare array decode = %+v", items)
	}
}

func TestNotificationCountsEnvelope(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = io.WriteString(w, `{"data":{"all":9,"important":1,"other":8,"snoozed":2,"cleared":0,"unread":3}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static")
	c, err := m.NotificationCounts(notifSessCtx(), "project", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/notifications/counts" || !strings.Contains(gotQuery, "scope=project") || !strings.Contains(gotQuery, "project_id=p1") {
		t.Errorf("counts request = %s?%s", gotPath, gotQuery)
	}
	if c.All != 9 || c.Unread != 3 || c.Snoozed != 2 {
		t.Errorf("envelope counts = %+v", c)
	}
}

func TestNotificationCountsBareObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"all":4,"unread":2,"requiresAction":1}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static")
	c, err := m.NotificationCounts(context.Background(), "account", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.All != 4 || c.Unread != 2 || c.RequiresAction != 1 {
		t.Fatalf("bare counts = %+v", c)
	}
}

func TestListNotificationPreferences(t *testing.T) {
	var gotPath, gotMethod, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotQuery = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = io.WriteString(w, `{"data":[
			{"eventKey":"task.assigned","label":"Task assigned","required":true,"enabled":true},
			{"eventKey":"comment.reply","label":"Comment reply","required":false,"enabled":false}
		]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static")
	prefs, err := m.ListNotificationPreferences(notifSessCtx(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/notifications/preferences" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotQuery != "project_id=p1" {
		t.Errorf("query = %q, want project_id=p1", gotQuery)
	}
	if len(prefs) != 2 || prefs[0].EventKey != "task.assigned" || !prefs[0].Required || prefs[1].Enabled {
		t.Fatalf("prefs = %+v", prefs)
	}
}

func TestSetNotificationPreference(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static")
	if err := m.SetNotificationPreference(notifSessCtx(), "p1", "comment.reply", "", true); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/notifications/preferences" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatal(err)
	}
	if body["projectId"] != "p1" || body["eventKey"] != "comment.reply" || body["channel"] != "in_app" || body["enabled"] != true {
		t.Errorf("body = %s", gotBody)
	}
}

func TestNotificationMutationPaths(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static")
	cases := []struct {
		name       string
		run        func() error
		wantMethod string
		wantPath   string
		wantBody   string
	}{
		{"read", func() error { return m.MarkNotificationRead(notifSessCtx(), "n-1") }, http.MethodPatch, "/api/notifications/n-1/read", ""},
		{"unread", func() error { return m.MarkNotificationUnread(notifSessCtx(), "n-1") }, http.MethodPost, "/api/notifications/n-1/unread", ""},
		{"dismiss", func() error { return m.DismissNotification(notifSessCtx(), "n-1") }, http.MethodDelete, "/api/notifications/n-1/dismiss", ""},
		{"snooze", func() error { return m.SnoozeNotification(notifSessCtx(), "n-1", "2026-10-01T00:00:00Z") }, http.MethodPost, "/api/notifications/n-1/snooze", `"until"`},
		{"unsnooze", func() error { return m.UnsnoozeNotification(notifSessCtx(), "n-1") }, http.MethodPost, "/api/notifications/n-1/unsnooze", ""},
		{"clear", func() error { return m.ClearNotification(notifSessCtx(), "n-1") }, http.MethodPost, "/api/notifications/n-1/clear", ""},
		{"restore", func() error { return m.RestoreNotification(notifSessCtx(), "n-1") }, http.MethodPost, "/api/notifications/n-1/restore", ""},
		{"resolve", func() error { return m.ResolveNotification(notifSessCtx(), "n-1", "accept") }, http.MethodPost, "/api/notifications/n-1/resolve", `"status":"accept"`},
		{"mark-all", func() error { return m.MarkAllNotificationsRead(notifSessCtx(), "project", "p1") }, http.MethodPost, "/api/notifications/mark-all-read", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotBody = ""
			if err := tc.run(); err != nil {
				t.Fatal(err)
			}
			if gotMethod != tc.wantMethod || gotPath != tc.wantPath {
				t.Errorf("request = %s %s, want %s %s", gotMethod, gotPath, tc.wantMethod, tc.wantPath)
			}
			if tc.wantBody != "" && !strings.Contains(gotBody, tc.wantBody) {
				t.Errorf("body = %q, want to contain %q", gotBody, tc.wantBody)
			}
		})
	}
}

func TestNotificationEventStreamAccountNoProjectHeader(t *testing.T) {
	var gotPath, gotQuery, gotAuth, gotProj string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		gotProj = r.Header.Get("X-Project-ID")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: notification\ndata: {\"id\":\"n1\"}\n\n")
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static")
	body, err := m.NotificationEventStream(notifSessCtx(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	raw, _ := io.ReadAll(body)
	if gotPath != "/api/events/stream/account" || gotQuery != "" {
		t.Errorf("stream request = %s?%s, want /api/events/stream/account with no query", gotPath, gotQuery)
	}
	if gotAuth != "Bearer sess-token" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotProj != "" {
		t.Errorf("X-Project-ID = %q, want empty on the account stream", gotProj)
	}
	if !strings.Contains(string(raw), "event: notification") {
		t.Errorf("stream body = %q", raw)
	}
}

func TestNotificationEventStreamProject(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, "event: notification\ndata: {}\n\n")
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static")
	body, err := m.NotificationEventStream(notifSessCtx(), "proj-9")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	if !strings.Contains(gotQuery, "projectId=proj-9") {
		t.Errorf("query = %q, want projectId=proj-9", gotQuery)
	}
}

func TestNotificationEventStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"code":"down","message":"events offline"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static")
	if _, err := m.NotificationEventStream(notifSessCtx(), ""); err == nil || !strings.Contains(err.Error(), "memory 503") {
		t.Fatalf("error = %v, want memory 503", err)
	}
}
