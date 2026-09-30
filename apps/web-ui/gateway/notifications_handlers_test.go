package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// notificationsEcho wires the notification JSON API routes onto a fresh echo.
func notificationsEcho(s *Server) *echo.Echo {
	e := echo.New()
	api := e.Group("/api")
	api.GET("/notifications", s.listNotifications)
	api.GET("/notifications/counts", s.notificationCounts)
	api.GET("/notifications/preferences", s.listNotificationPreferences)
	api.PUT("/notifications/preferences", s.setNotificationPreference)
	api.GET("/notifications/stream", s.notificationStream)
	api.POST("/notifications/mark-all-read", s.markAllNotificationsRead)
	api.PATCH("/notifications/:id/read", s.markNotificationRead)
	api.POST("/notifications/:id/unread", s.markNotificationUnread)
	api.DELETE("/notifications/:id/dismiss", s.dismissNotification)
	api.POST("/notifications/:id/snooze", s.snoozeNotification)
	api.POST("/notifications/:id/unsnooze", s.unsnoozeNotification)
	api.POST("/notifications/:id/clear", s.clearNotification)
	api.POST("/notifications/:id/restore", s.restoreNotification)
	api.POST("/notifications/:id/resolve", s.resolveNotification)
	return e
}

func doNotif(t *testing.T, e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestListNotificationsHandler(t *testing.T) {
	f := &fakeMemory{notifications: []Notification{
		{ID: "n1", Title: "a"}, {ID: "n2", Title: "b"},
	}}
	s := &Server{memory: f}
	e := notificationsEcho(s)

	rec := doNotif(t, e, http.MethodGet, "/api/notifications?scope=project&tab=snoozed&project_id=p1&unread_only=true&requires_action=true", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Notifications []Notification `json:"notifications"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Notifications) != 2 {
		t.Fatalf("got %d notifications, want 2", len(out.Notifications))
	}
	if f.lastListParams.Scope != "project" || f.lastListParams.ProjectID != "p1" || f.lastListParams.Tab != "snoozed" || !f.lastListParams.UnreadOnly || !f.lastListParams.RequiresAction {
		t.Errorf("forwarded params = %+v", f.lastListParams)
	}
}

// TestListNotificationsHandlerEmptyList asserts an empty inbox returns an empty
// array (not null) so the client renders an empty state, not an error.
func TestListNotificationsHandlerEmptyList(t *testing.T) {
	s := &Server{memory: &fakeMemory{}}
	e := notificationsEcho(s)

	rec := doNotif(t, e, http.MethodGet, "/api/notifications?scope=account", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"notifications":[]`) {
		t.Errorf("empty list body = %s, want an empty array", rec.Body.String())
	}
}

// TestListNotificationsHandlerScopeDefault asserts a missing scope defaults to
// account (the mandatory inbox).
func TestListNotificationsHandlerScopeDefault(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{memory: f}
	e := notificationsEcho(s)

	doNotif(t, e, http.MethodGet, "/api/notifications", "")
	if f.lastListParams.Scope != "account" {
		t.Errorf("scope = %q, want account", f.lastListParams.Scope)
	}
}

// TestListNotificationsHandlerRequiresAction asserts the action filter is
// forwarded.
func TestListNotificationsHandlerRequiresAction(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{memory: f}
	e := notificationsEcho(s)

	doNotif(t, e, http.MethodGet, "/api/notifications?scope=account&requires_action=true", "")
	if !f.lastListParams.RequiresAction {
		t.Error("requires_action filter not forwarded")
	}
}

func TestNotificationCountsHandler(t *testing.T) {
	f := &fakeMemory{notificationCounts: &NotificationCounts{All: 5, Unread: 2, RequiresAction: 1}}
	s := &Server{memory: f}
	e := notificationsEcho(s)

	rec := doNotif(t, e, http.MethodGet, "/api/notifications/counts?scope=project&project_id=p1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got NotificationCounts
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.All != 5 || got.Unread != 2 || got.RequiresAction != 1 {
		t.Errorf("counts = %+v", got)
	}
	if f.lastCountsScope != "project" || f.lastCountsProject != "p1" {
		t.Errorf("counts scope = %q/%q", f.lastCountsScope, f.lastCountsProject)
	}
}

func TestListNotificationsHandlerUpstreamError(t *testing.T) {
	f := &fakeMemory{notificationListErr: errString("boom")}
	s := &Server{memory: f}
	e := notificationsEcho(s)

	rec := doNotif(t, e, http.MethodGet, "/api/notifications", "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestNotificationMutationHandlers(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		path     string
		body     string
		wantKind string
		wantVerb string
	}{
		{"read", http.MethodPatch, "/api/notifications/n1/read", "", "read", ""},
		{"unread", http.MethodPost, "/api/notifications/n1/unread", "", "unread", ""},
		{"dismiss", http.MethodDelete, "/api/notifications/n1/dismiss", "", "dismiss", ""},
		{"snooze", http.MethodPost, "/api/notifications/n1/snooze", `{"until":"2026-10-01T00:00:00Z"}`, "snooze", "2026-10-01T00:00:00Z"},
		{"unsnooze", http.MethodPost, "/api/notifications/n1/unsnooze", "", "unsnooze", ""},
		{"clear", http.MethodPost, "/api/notifications/n1/clear", "", "clear", ""},
		{"restore", http.MethodPost, "/api/notifications/n1/restore", "", "restore", ""},
		{"resolve", http.MethodPost, "/api/notifications/n1/resolve", `{"action":"accept"}`, "resolve", "accept"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMemory{}
			s := &Server{memory: f}
			e := notificationsEcho(s)

			rec := doNotif(t, e, tc.method, tc.path, tc.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"ok":true`) {
				t.Errorf("body = %s", rec.Body.String())
			}
			want := tc.wantKind + ":n1:" + tc.wantVerb
			if len(f.notificationMutations) != 1 || f.notificationMutations[0] != want {
				t.Errorf("recorded = %v, want %q", f.notificationMutations, want)
			}
		})
	}
}

func TestMarkAllNotificationsReadHandler(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{memory: f}
	e := notificationsEcho(s)

	rec := doNotif(t, e, http.MethodPost, "/api/notifications/mark-all-read?scope=project&project_id=p1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(f.notificationMutations) != 1 || f.notificationMutations[0] != "mark-all-read:project:p1" {
		t.Errorf("recorded = %v", f.notificationMutations)
	}
}

func TestNotificationPreferencesHandlers(t *testing.T) {
	f := &fakeMemory{notificationPrefs: []NotificationPreference{{EventKey: "task.assigned", Enabled: true}}}
	s := &Server{memory: f}
	e := notificationsEcho(s)

	rec := doNotif(t, e, http.MethodGet, "/api/notifications/preferences", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", rec.Code)
	}
	var out struct {
		Preferences []NotificationPreference `json:"preferences"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Preferences) != 1 || out.Preferences[0].EventKey != "task.assigned" {
		t.Errorf("preferences = %+v", out.Preferences)
	}

	// Unknown/empty key rejected before the upstream call.
	if rec := doNotif(t, e, http.MethodPut, "/api/notifications/preferences", `{"enabled":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty key PUT = %d, want 400", rec.Code)
	}

	rec = doNotif(t, e, http.MethodPut, "/api/notifications/preferences", `{"eventKey":"comment.reply","enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(f.notificationMutations) != 1 || f.notificationMutations[0] != "pref:comment.reply:in_app:true" {
		t.Errorf("recorded = %v", f.notificationMutations)
	}
}

// TestNotificationStreamPassThroughWithBearer exercises the SSE proxy against a
// real MemoryClient pointed at a fake backend: it asserts the frame passes
// through and that the session bearer is attached upstream (and, for the
// account stream, no project scoping header).
func TestNotificationStreamPassThroughWithBearer(t *testing.T) {
	var gotAuth, gotProj, gotQuery string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotProj = r.Header.Get("X-Project-ID")
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: notification\ndata: {\"id\":\"n1\"}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer backend.Close()

	s := &Server{memory: NewMemoryClient(backend.URL, "static")}
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := withSessionContext(c.Request().Context(), &sessionContext{Token: "tok-1", ProjectID: "proj-1"})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})
	e.GET("/api/notifications/stream", s.notificationStream)

	srv := httptest.NewServer(e)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/notifications/stream?scope=account")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.Contains(string(body), "event: notification") {
		t.Errorf("stream body = %q", body)
	}
	if gotAuth != "Bearer tok-1" {
		t.Errorf("upstream Authorization = %q, want the session bearer", gotAuth)
	}
	if gotQuery != "" {
		t.Errorf("account stream upstream query = %q, want empty", gotQuery)
	}
	if gotProj != "" {
		t.Errorf("account stream upstream X-Project-ID = %q, want empty", gotProj)
	}
}

// TestNotificationStreamProjectScope asserts project scope forwards the
// projectId to the upstream stream.
func TestNotificationStreamProjectScope(t *testing.T) {
	var gotQuery string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, "event: notification\ndata: {}\n\n")
	}))
	defer backend.Close()

	s := &Server{memory: NewMemoryClient(backend.URL, "static")}
	e := notificationsEcho(s)
	// Route group already registers the stream; just hit it.
	srv := httptest.NewServer(e)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/notifications/stream?scope=project&project_id=proj-9")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)
	if !strings.Contains(gotQuery, "projectId=proj-9") {
		t.Errorf("upstream query = %q, want projectId=proj-9", gotQuery)
	}
}

// errString is a tiny error type for fake failures.
type errString string

func (e errString) Error() string { return string(e) }
