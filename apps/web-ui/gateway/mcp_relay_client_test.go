package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- MCP relay (external nodes) client methods ---
//
// These cover the MemoryClient proxies for the backend MCP relay read API
// (GET /api/mcp-relay/sessions, GET /api/mcp-relay/sessions/:id/tools). Unlike
// every other gateway client method these endpoints return PLAIN JSON (no
// successEnvelope), and they are project-scoped — session X-Project-ID /
// X-Org-ID headers are threaded when a session context is attached.

func TestListRelaySessions(t *testing.T) {
	var gotPath, gotMethod, gotAuth, gotProj, gotOrg string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotAuth = r.Header.Get("Authorization")
		gotProj = r.Header.Get("X-Project-ID")
		gotOrg = r.Header.Get("X-Org-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"sessions":[
			{"instance_id":"macbook-ada","version":"1.2.0","tool_count":3,"connected_at":"2026-09-01T10:00:00Z"},
			{"instance_id":"studio-1","version":"","tool_count":0,"connected_at":"2026-09-01T09:00:00+02:00"}
		]}`)
	}))
	defer srv.Close()

	ctx := withSessionContext(context.Background(), &sessionContext{
		Token:     "sess-token",
		ProjectID: "sess-project",
		OrgID:     "sess-org",
	})
	m := NewMemoryClient(srv.URL, "static-token", "static-project")
	sessions, err := m.ListRelaySessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/mcp-relay/sessions" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/mcp-relay/sessions", gotMethod, gotPath)
	}
	if gotAuth != "Bearer sess-token" {
		t.Errorf("Authorization = %q, want the session bearer token", gotAuth)
	}
	if gotProj != "sess-project" || gotOrg != "sess-org" {
		t.Errorf("headers = proj %q org %q, want sess-project/sess-org", gotProj, gotOrg)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
	if sessions[0].InstanceID != "macbook-ada" || sessions[0].Version != "1.2.0" ||
		sessions[0].ToolCount != 3 || !sessions[0].ConnectedAt.Equal(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("sessions[0] = %+v", sessions[0])
	}
	if sessions[1].InstanceID != "studio-1" || sessions[1].ToolCount != 0 || !sessions[1].ConnectedAt.Equal(time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)) {
		t.Errorf("sessions[1] = %+v (offset timestamp must normalize to UTC)", sessions[1])
	}
}

// TestListRelaySessionsNoSession asserts the API-key path: the static
// server-side token applies and no scoping headers are sent (that token is
// already project-bound) — same semantics as every other MemoryClient method.
func TestListRelaySessionsNoSession(t *testing.T) {
	var gotAuth, gotProj string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotProj = r.Header.Get("X-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"sessions":[]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static-token", "static-project")
	sessions, err := m.ListRelaySessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer static-token" {
		t.Errorf("Authorization = %q, want the static token", gotAuth)
	}
	if gotProj != "" {
		t.Errorf("X-Project-ID = %q, want empty on the API-key path", gotProj)
	}
	if len(sessions) != 0 {
		t.Errorf("got %d sessions, want 0 for an empty body", len(sessions))
	}
}

func TestListRelaySessionsError500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"internal","message":"relay hub unavailable"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.ListRelaySessions(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "memory 500 internal") {
		t.Fatalf("error = %v, want memory 500 internal", err)
	}
}

func TestGetRelaySessionToolsNested(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tools":[
			{"name":"notes_search","description":"Search Apple Notes","inputSchema":{"type":"object"}},
			{"name":"reminders_add"},
			{"tool":{"name":"nested_only","description":"nested shape"}}
		]}`)
	}))
	defer srv.Close()

	ctx := withSessionContext(context.Background(), &sessionContext{Token: "sess-token", ProjectID: "sess-project", OrgID: "sess-org"})
	m := NewMemoryClient(srv.URL, "tok", "proj")
	tools, err := m.GetRelaySessionTools(ctx, "macbook-ada")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/mcp-relay/sessions/macbook-ada/tools" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/mcp-relay/sessions/macbook-ada/tools", gotMethod, gotPath)
	}
	if gotAuth != "Bearer sess-token" {
		t.Errorf("Authorization = %q, want the session bearer token", gotAuth)
	}
	if len(tools) != 3 {
		t.Fatalf("got %d tools, want 3: %+v", len(tools), tools)
	}
	if tools[0].Name != "notes_search" || tools[0].Description != "Search Apple Notes" {
		t.Errorf("tools[0] = %+v", tools[0])
	}
	if tools[1].Name != "reminders_add" || tools[1].Description != "" {
		t.Errorf("tools[1] = %+v (no description must stay empty)", tools[1])
	}
	if tools[2].Name != "nested_only" || tools[2].Description != "nested shape" {
		t.Errorf("tools[2] = %+v (tool.name fallback failed)", tools[2])
	}
}

func TestGetRelaySessionToolsFlat(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		// Bare array shape — some connectors reply with the tools/list list
		// directly instead of wrapping it.
		_, _ = io.WriteString(w, `[
			{"name":"notes_search","description":"Search Apple Notes"},
			{"name":"reminders_add"}
		]`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	tools, err := m.GetRelaySessionTools(context.Background(), "macbook-ada")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/mcp-relay/sessions/macbook-ada/tools" {
		t.Errorf("path = %q", gotPath)
	}
	if len(tools) != 2 || tools[0].Name != "notes_search" || tools[1].Name != "reminders_add" {
		t.Errorf("tools = %+v", tools)
	}
}

// TestGetRelaySessionToolsShapes covers the empty + degraded payload cases:
// an empty tools array and entries with no resolvable name must yield an empty
// list without erroring (a payload shape quirk must never break the page).
func TestGetRelaySessionToolsShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty tools array", `{"tools":[]}`},
		{"bare empty array", `[]`},
		{"null body", `null`},
		{"empty object", `{}`},
		{"entries without names", `{"tools":[{"description":"no name"},{}]}`},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, tc.body)
		}))
		m := NewMemoryClient(srv.URL, "tok", "proj")
		tools, err := m.GetRelaySessionTools(context.Background(), "macbook-ada")
		srv.Close()
		if err != nil {
			t.Fatalf("%s: err = %v", tc.name, err)
		}
		if len(tools) != 0 {
			t.Errorf("%s: got %d tools, want 0", tc.name, len(tools))
		}
	}
}

func TestGetRelaySessionToolsNotFound404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"session not found"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.GetRelaySessionTools(context.Background(), "gone"); err == nil ||
		!strings.Contains(err.Error(), "memory 404 not_found") {
		t.Fatalf("error = %v, want memory 404 not_found", err)
	}
}

func TestGetRelaySessionToolsError500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"internal","message":"relay hub exploded"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.GetRelaySessionTools(context.Background(), "macbook-ada"); err == nil ||
		!strings.Contains(err.Error(), "memory 500 internal") {
		t.Fatalf("error = %v, want memory 500 internal", err)
	}
}

// TestRelayClientInstanceIDEscaping asserts the instance id is path-escaped
// (connector-generated ids may contain characters unsafe in a URL segment).
func TestRelayClientInstanceIDEscaping(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.RequestURI // preserves the raw escaped segment (r.URL.Path is decoded)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tools":[]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.GetRelaySessionTools(context.Background(), "desk 1/edge"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/mcp-relay/sessions/desk%201%2Fedge/tools" {
		t.Errorf("path = %q, want the instance id escaped", gotPath)
	}
}

func TestRelayClientUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	m := NewMemoryClient(url, "tok", "proj")
	if _, err := m.ListRelaySessions(context.Background()); err == nil {
		t.Error("ListRelaySessions: want transport error, got nil")
	}
	if _, err := m.GetRelaySessionTools(context.Background(), "macbook-ada"); err == nil {
		t.Error("GetRelaySessionTools: want transport error, got nil")
	}
}

// TestRelayToolNaming covers the <instance>_<tool> agent-facing derivation.
func TestRelayToolNaming(t *testing.T) {
	if got := relayAgentToolName("macbook-ada", "notes_search"); got != "macbook-ada_notes_search" {
		t.Errorf("relayAgentToolName = %q", got)
	}
	node := relayNode{
		Session: RelaySession{InstanceID: "macbook-ada"},
		Tools:   []RelayTool{{Name: "notes_search"}, {Name: "reminders_add"}},
	}
	names := node.agentToolNames()
	if len(names) != 2 || names[0] != "macbook-ada_notes_search" || names[1] != "macbook-ada_reminders_add" {
		t.Errorf("agentToolNames = %v", names)
	}
	agent := &AgentDefinition{Tools: []string{"macbook-ada_notes_search"}}
	if !relayNodeInUse(node, agent) {
		t.Error("relayNodeInUse should report true when an agent-facing tool is whitelisted")
	}
	if relayNodeInUse(node, &AgentDefinition{}) {
		t.Error("relayNodeInUse must be false when no tool is whitelisted")
	}
	groups := relayPickerGroups([]relayNode{node, {Session: RelaySession{InstanceID: "empty"}}})
	if len(groups) != 1 || groups[0].Session.InstanceID != "macbook-ada" {
		t.Errorf("relayPickerGroups = %+v, want only the node with tools", groups)
	}
}
