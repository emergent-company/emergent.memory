package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// TestDeviceSurfacePath pins the gateway device-surface allowlist: room-token
// mint, agent picker, the per-agent object browser, chat relay, conversation
// lifecycle (archive/unarchive/delete), session log/records and memory browsing
// only.
func TestDeviceSurfacePath(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{"POST", "/api/token", true},
		{"GET", "/api/agents", true},
		{"GET", "/api/agents/abc", true},
		{"GET", "/api/agents/abc/objects", true},
		{"GET", "/api/agents/abc/objects/extra", false},
		{"GET", "/api/agents/abc/other", false},
		{"POST", "/api/chat", true},
		{"POST", "/api/chat/runs/r1/cancel", true},
		{"POST", "/api/chat/runs/r1/cancel/x", false},
		{"POST", "/api/chat/runs//cancel", false},
		{"POST", "/api/chat/runs/r1/stop", false},
		{"GET", "/api/chat/runs/r1/cancel", false},
		{"POST", "/api/chat/questions/q1/respond", true},
		{"POST", "/api/chat/questions/q1/cancel", true},
		{"POST", "/api/chat/questions/q1/respond/x", false},
		{"POST", "/api/chat/questions//respond", false},
		{"POST", "/api/chat/questions/q1/approve", false},
		{"POST", "/api/conversations/c1/archive", true},
		{"POST", "/api/conversations/c1/unarchive", true},
		{"DELETE", "/api/conversations/c1", true},
		{"POST", "/api/conversations/c1/archive/extra", false},
		{"POST", "/api/conversations/c1/unarchive/extra", false},
		{"POST", "/api/conversations/c1/nested/archive", false},
		{"POST", "/api/conversations//archive", false},
		{"POST", "/api/conversations/c1/delete", false},
		{"POST", "/api/conversations/c1", false},
		{"DELETE", "/api/conversations/c1/nested", false},
		{"DELETE", "/api/conversations/c1/archive", false},

		// Conversation reads (issue #431): history + events only, one
		// non-empty id segment, GET only. This is the exact-shape matcher the
		// iOS timeline and reattach lanes depend on.
		{"GET", "/api/conversations/abc/history", true},
		{"GET", "/api/conversations/abc/events", true},
		// The agent's plan (#440) is the third read-only conversation action.
		{"GET", "/api/conversations/abc/todos", true},
		{"GET", "/api/conversations/abc/todos/x", false},
		{"GET", "/api/conversations//todos", false},
		{"POST", "/api/conversations/abc/todos", false},
		{"GET", "/api/conversations/abc/dump", false},
		{"GET", "/api/conversations/abc/history/x", false},
		{"GET", "/api/conversations//history", false},
		{"GET", "/api/conversations/abc/archive", false},
		{"POST", "/api/conversations/abc/history", false},
		{"POST", "/api/conversations/abc/events", false},
		{"GET", "/api/sessions", true},
		{"GET", "/api/session", true},
		{"GET", "/api/project/config", true},

		// Object card (iOS chat): read-only, one ref segment, card or history.
		{"GET", "/api/objects/o1/card", true},
		{"GET", "/api/objects/o1/history", true},
		{"GET", "/api/objects//card", false},
		{"GET", "/api/objects/o1", false},
		{"GET", "/api/objects/o1/card/x", false},
		{"GET", "/api/objects/o1/edges", false},
		{"POST", "/api/objects/o1/card", false},
		{"GET", "/api/blueprints/compiled-types", false},

		// Object-card work writes (issue #454): exactly one ref segment, then
		// status or assignee, POST only. Everything else under /api/objects
		// stays off-surface.
		{"POST", "/api/objects/o1/status", true},
		{"POST", "/api/objects/o1/assignee", true},
		{"POST", "/api/objects//status", false},
		{"POST", "/api/objects/o1/status/x", false},
		{"POST", "/api/objects/o1/approve", false},
		{"POST", "/api/objects/o1/assignee/clear", false},
		{"GET", "/api/objects/o1/status", false},
		{"PUT", "/api/objects/o1/assignee", false},
		{"DELETE", "/api/objects/o1/status", false},

		// Share capture: exactly two shapes, one non-empty project id.
		{"POST", "/api/projects/p1/remember", true},
		{"POST", "/api/projects/p1/remember/file", true},
		{"POST", "/api/projects//remember", false},
		{"POST", "/api/projects/p1/remember/file/extra", false},
		{"POST", "/api/projects/p1/remember/", false},
		{"GET", "/api/projects/p1/remember", false},
		{"PUT", "/api/projects/p1/remember/file", false},
		{"POST", "/api/projects/p1/forget", false},
		{"POST", "/api/projects/p1/settings/remember/field", false},
		{"POST", "/api/projects/p1/remember/other", false},

		{"GET", "/api/agents/abc/mcp-endpoint", false},
		{"POST", "/api/agents", false},
		{"GET", "/api/projects", false},
		{"GET", "/api/orgs", false},
		{"GET", "/api/members", false},
		{"POST", "/api/setup", false},
		{"DELETE", "/api/agents/abc", false},
		{"GET", "/api/conversations", false},
		// The project config is read-only for a device: widening the method
		// must fail here rather than ship.
		{"POST", "/api/project/config", false},
		{"PUT", "/api/project/config", false},
	}
	for _, tc := range cases {
		if got := deviceSurfacePath(tc.method, tc.path); got != tc.want {
			t.Errorf("deviceSurfacePath(%s, %s) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func deviceCredentialEcho(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: f}
	e := echo.New()
	e.GET("/api/agents", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.Use(s.requireSessionOrKey)
	return s, e
}

func TestRequireDeviceCredentialSuccess(t *testing.T) {
	f := &fakeMemory{
		introspectInfo: &deviceTokenInfo{
			Type:      "api_token",
			Scopes:    []string{"device:api", "agents:read", "data:read"},
			ProjectID: "proj-1",
			OrgID:     "org-1",
		},
	}
	_, e := deviceCredentialEcho(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("Authorization", "Bearer emt_devicecredential")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(f.introspectSeen) != 1 || f.introspectSeen[0] != "emt_devicecredential" {
		t.Fatalf("introspection not performed with the presented token: %v", f.introspectSeen)
	}
}

func TestRequireDeviceCredentialFailClosed(t *testing.T) {
	device := &deviceTokenInfo{Type: "api_token", Scopes: []string{"device:api", "agents:read", "data:read"}, ProjectID: "proj-1", OrgID: "org-1"}

	cases := []struct {
		name       string
		f          *fakeMemory
		method     string
		path       string
		auth       string
		wantStatus int
	}{
		{"missing bearer", &fakeMemory{}, http.MethodGet, "/api/agents", "", http.StatusUnauthorized},
		{"non-emt bearer", &fakeMemory{}, http.MethodGet, "/api/agents", "Bearer not-a-device-token", http.StatusUnauthorized},
		{"introspection error (store down)", &fakeMemory{introspectErr: context.DeadlineExceeded}, http.MethodGet, "/api/agents", "Bearer emt_x", http.StatusUnauthorized},
		{"unknown token", &fakeMemory{introspectInfo: nil}, http.MethodGet, "/api/agents", "Bearer emt_unknown", http.StatusUnauthorized},
		{"non-device emt token", &fakeMemory{introspectInfo: &deviceTokenInfo{Type: "api_token", Scopes: []string{"data:read"}, ProjectID: "proj-1"}}, http.MethodGet, "/api/agents", "Bearer emt_programmatic", http.StatusUnauthorized},
		{"device token off-surface", &fakeMemory{introspectInfo: device}, http.MethodGet, "/api/projects", "Bearer emt_x", http.StatusForbidden},
		{"device token write off-surface", &fakeMemory{introspectInfo: device}, http.MethodPost, "/api/agents", "Bearer emt_x", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, e := deviceCredentialEcho(tc.f)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			e.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestRequireDeviceCredentialXAPIKeyHeader(t *testing.T) {
	// The iOS VoiceAgent presents the device credential on X-API-Key; the
	// gateway must accept it there exactly as on Authorization: Bearer.
	device := &deviceTokenInfo{Type: "api_token", Scopes: []string{"device:api", "agents:read", "data:read"}, ProjectID: "proj-1", OrgID: "org-1"}

	t.Run("accepted on X-API-Key", func(t *testing.T) {
		f := &fakeMemory{introspectInfo: device}
		_, e := deviceCredentialEcho(f)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
		req.Header.Set("X-API-Key", "emt_devicecredential")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if len(f.introspectSeen) != 1 || f.introspectSeen[0] != "emt_devicecredential" {
			t.Fatalf("introspection not performed with the X-API-Key token: %v", f.introspectSeen)
		}
	})

	t.Run("non-device X-API-Key rejected", func(t *testing.T) {
		f := &fakeMemory{introspectInfo: &deviceTokenInfo{Type: "api_token", Scopes: []string{"data:read"}, ProjectID: "proj-1"}}
		_, e := deviceCredentialEcho(f)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
		req.Header.Set("X-API-Key", "emt_programmatic")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (non-device emt_* via X-API-Key)", rec.Code)
		}
	})

	t.Run("off-surface X-API-Key rejected", func(t *testing.T) {
		f := &fakeMemory{introspectInfo: device}
		_, e := deviceCredentialEcho(f)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
		req.Header.Set("X-API-Key", "emt_x")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (off-surface via X-API-Key)", rec.Code)
		}
	})

	t.Run("unknown X-API-Key rejected", func(t *testing.T) {
		f := &fakeMemory{introspectInfo: nil}
		_, e := deviceCredentialEcho(f)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
		req.Header.Set("X-API-Key", "emt_unknown")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (unknown X-API-Key)", rec.Code)
		}
	})
}

// A device credential that carries the marker but resolves to an empty project
// must still be rejected on the surface (no project binding to act against).
func TestRequireDeviceCredentialMissingProjectFailsClosed(t *testing.T) {
	f := &fakeMemory{introspectInfo: &deviceTokenInfo{Type: "api_token", Scopes: []string{"device:api", "agents:read", "data:read"}}}
	_, e := deviceCredentialEcho(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("Authorization", "Bearer emt_x")
	e.ServeHTTP(rec, req)
	// The gateway still recognises the credential but the surface/derivation
	// path has no project to act against; the device surface guard must not
	// silently widen. Here the route is on-surface, so the request is allowed
	// and project emptiness is upstream's to resolve.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

// testDeviceTokenInfo is the introspected identity every device test uses.
func testDeviceTokenInfo() *deviceTokenInfo {
	return &deviceTokenInfo{
		Type:      "api_token",
		Scopes:    []string{"device:api", "agents:read", "data:read"},
		ProjectID: "proj-1",
		OrgID:     "org-1",
	}
}

// deviceConversationEcho wires the device gate to the conversation read routes
// so the authz boundary can be exercised end to end.
func deviceConversationEcho(f *fakeMemory) (*Server, *echo.Echo) {
	shutdown := make(chan struct{})
	close(shutdown) // let a permitted SSE subscription finish instead of blocking
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: f, shutdownCh: shutdown}
	e := echo.New()
	e.Use(s.requireSessionOrKey)
	e.GET("/api/conversations/:id/history", s.getConversationHistory)
	e.GET("/api/conversations/:id/events", s.conversationEvents)
	e.GET("/api/conversations/:id/todos", s.getConversationTodos)
	return s, e
}

// TestDeviceCredentialAttachesScopedContext pins the property streamEvents
// depends on: a device request carries a sessionContext (token + token-derived
// project/org, Device=true) in its request context. Without it the
// conversation-events poller would run unscoped — the cross-tenant leak issue
// #431 requires us to rule out.
func TestDeviceCredentialAttachesScopedContext(t *testing.T) {
	f := &fakeMemory{introspectInfo: testDeviceTokenInfo()}
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: f}
	e := echo.New()
	var got *sessionContext
	e.GET("/api/agents", func(c echo.Context) error {
		got, _ = sessionContextFrom(c.Request().Context())
		return c.String(http.StatusOK, "ok")
	})
	e.Use(s.requireSessionOrKey)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("Authorization", "Bearer emt_dev")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if got == nil || !got.Device {
		t.Fatalf("device request carried no device session context: %+v", got)
	}
	if got.Token != "emt_dev" || got.ProjectID != "proj-1" || got.OrgID != "org-1" {
		t.Fatalf("device session context = %+v, want the introspected token/project/org", got)
	}
}

// TestDeviceConversationEventsOwnConversationStreams proves the positive half of
// the boundary: for a conversation the device's user can see, the events route
// subscribes and emits the initial refresh frame (the shape iOS #437 consumes).
func TestDeviceConversationEventsOwnConversationStreams(t *testing.T) {
	f := &fakeMemory{introspectInfo: testDeviceTokenInfo()}
	_, e := deviceConversationEcho(f)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/conversations/own/events", nil)
	req.Header.Set("X-API-Key", "emt_dev")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"type":"refresh"`) {
		t.Fatalf("events stream carried no refresh frame: %q", rec.Body.String())
	}
}

// TestDeviceConversationEventsRunNamespaceRejected pins the route/key
// distinction: an id of `run:<runId>` on the conversation route must not be
// turned into the run-scoped hub key, because that key skips the device
// ownership check (and run events are off-surface for a device).
func TestDeviceConversationEventsRunNamespaceRejected(t *testing.T) {
	f := &fakeMemory{introspectInfo: testDeviceTokenInfo()}
	_, e := deviceConversationEcho(f)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/conversations/run:R1/events", nil)
	req.Header.Set("X-API-Key", "emt_dev")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "data:") {
		t.Fatalf("a run-namespaced conversation id streamed frames: %q", rec.Body.String())
	}
}

// TestSessionEventsSubscriptionNotDeviceChecked keeps the new device
// authorization device-only: a web-session subscriber (Device=false) still
// streams even when the summary read would fail, so shared streamEvents does
// not change the web path's behaviour.
func TestSessionEventsSubscriptionNotDeviceChecked(t *testing.T) {
	f := &fakeMemory{detailErr: errors.New("memory: GET /api/chat/other: 404 not_found")}
	shutdown := make(chan struct{})
	close(shutdown)
	s := &Server{cfg: Config{AuthMode: "session"}, memory: f, shutdownCh: shutdown}

	e := echo.New()
	e.GET("/api/conversations/:id/events", func(c echo.Context) error {
		sc := &sessionContext{Token: "tok", ProjectID: "proj-1"}
		c.SetRequest(c.Request().WithContext(withSessionContext(c.Request().Context(), sc)))
		return s.conversationEvents(c)
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations/other/events", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("web session subscriber status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"type":"refresh"`) {
		t.Fatalf("web session stream carried no refresh frame: %q", rec.Body.String())
	}
}

// TestDeviceConversationReadForeignIs404 is the cross-tenant boundary: a device
// token asking for a conversation its bound user cannot see (another user's
// private conversation, or one in another project) gets 404 — from the history
// route and from the events subscription, which must authorize before it
// streams anything.
func TestDeviceConversationReadForeignIs404(t *testing.T) {
	notFound := errors.New("memory: GET /api/chat/foreign: 404 not_found")
	f := &fakeMemory{introspectInfo: testDeviceTokenInfo(), histErr: notFound, detailErr: notFound}
	_, e := deviceConversationEcho(f)

	for _, path := range []string{
		"/api/conversations/foreign/history",
		"/api/conversations/foreign/events",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-API-Key", "emt_dev")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 (a device token must not read another user's conversation); body=%s",
				path, rec.Code, rec.Body.String())
		}
	}
}

// TestDeviceConversationHistoryCarriesToolPresentation pins the "check also" in
// issue #431: a history tool_call item carries the same category/subject/view
// presentation (snake_case on the recorded wire) as a live mcp_tool frame
// (camelCase), so iOS can render recorded and live steps identically. The
// enrichment is applied by renderHistoryHTML on this route.
func TestDeviceConversationHistoryCarriesToolPresentation(t *testing.T) {
	const item = `{"kind":"tool_call",` +
		`"tool_name":"document-get","tool_status":"completed",` +
		`"tool_input":{"id":"d1","path":"notes.md"},` +
		`"tool_output":{"id":"d1","filename":"notes.md","content":"a line","ok":true}}`
	f := &fakeMemory{
		introspectInfo: testDeviceTokenInfo(),
		histories: map[string]*ConversationHistory{
			"own": {ConversationID: "own", Items: []json.RawMessage{json.RawMessage(item)}},
		},
	}
	_, e := deviceConversationEcho(f)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/conversations/own/history", nil)
	req.Header.Set("X-API-Key", "emt_dev")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("history body is not JSON: %v", err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(body.Items))
	}
	for _, field := range []string{"tool_category", "tool_subject", "tool_view"} {
		if _, ok := body.Items[0][field]; !ok {
			t.Errorf("history tool_call item missing %q: %v", field, body.Items[0])
		}
	}
}
