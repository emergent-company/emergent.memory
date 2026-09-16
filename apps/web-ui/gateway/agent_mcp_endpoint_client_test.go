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

// --- agent MCP endpoint client methods ---
//
// These cover the MemoryClient proxies for memory's agent-endpoint routes under
// /api/projects/:projectId. The project id rides in both the path (via
// projectIDFor) and the X-Project-ID session header; the raw secret comes back
// only from key create and rotate.

// agentMCPSessionCtx pins the request to proj-1 so the path substitution and
// the X-Project-ID header can be asserted together.
func agentMCPSessionCtx() context.Context {
	return withSessionContext(context.Background(), &sessionContext{Token: "sess", ProjectID: "proj-1"})
}

func TestGetAgentMCPEndpoint(t *testing.T) {
	var gotPath, gotMethod, gotProjectHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotProjectHeader = r.Header.Get("X-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"ep1","agentId":"a1","status":"active","createdAt":"2026-08-01T10:00:00Z","mcpUrl":"https://mem.example/api/mcp/agents/a1"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "fallback")
	ep, err := m.GetAgentMCPEndpoint(agentMCPSessionCtx(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/projects/proj-1/agents/a1/mcp-endpoint" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotProjectHeader != "proj-1" {
		t.Errorf("X-Project-ID = %q, want proj-1", gotProjectHeader)
	}
	if ep == nil || ep.ID != "ep1" || ep.MCPURL != "https://mem.example/api/mcp/agents/a1" {
		t.Fatalf("endpoint = %+v", ep)
	}
}

// TestGetAgentMCPEndpointNotFoundIsNil asserts a 404 (no endpoint yet) is not an
// error — the caller renders its create state.
func TestGetAgentMCPEndpointNotFoundIsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"no active endpoint"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	ep, err := m.GetAgentMCPEndpoint(context.Background(), "a1")
	if err != nil {
		t.Fatalf("404 must not error: %v", err)
	}
	if ep != nil {
		t.Fatalf("endpoint = %+v, want nil", ep)
	}
}

func TestCreateAgentMCPEndpoint(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"success":true,"data":{"id":"ep1","agentId":"a1","status":"active"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	ep, err := m.CreateAgentMCPEndpoint(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/projects/proj-1/agents/a1/mcp-endpoint" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if ep == nil || ep.ID != "ep1" {
		t.Fatalf("endpoint = %+v", ep)
	}
}

func TestRevokeAgentMCPEndpoint(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_, _ = io.WriteString(w, `{"status":"revoked"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	if err := m.RevokeAgentMCPEndpoint(context.Background(), "ep1"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/projects/proj-1/agent-mcp-endpoints/ep1" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
}

func TestListAgentMCPKeysWrappedAndBare(t *testing.T) {
	for name, body := range map[string]string{
		"wrapped": `{"keys":[{"id":"k1","label":"laptop","status":"active"}],"total":1}`,
		"bare":    `[{"id":"k1","label":"laptop","status":"active"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, body)
			}))
			defer srv.Close()

			m := NewMemoryClient(srv.URL, "tok", "proj-1")
			keys, err := m.ListAgentMCPKeys(context.Background(), "ep1")
			if err != nil {
				t.Fatal(err)
			}
			if gotPath != "/api/projects/proj-1/agent-mcp-endpoints/ep1/keys" {
				t.Errorf("path = %q", gotPath)
			}
			if len(keys) != 1 || keys[0].Label != "laptop" {
				t.Fatalf("keys = %+v", keys)
			}
		})
	}
}

func TestCreateAgentMCPKey(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"k1","label":"phone","status":"active","token":"emt_key","mcpUrl":"https://mem.example/api/mcp/agents/a1"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	secret, err := m.CreateAgentMCPKey(context.Background(), "ep1", "phone")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/projects/proj-1/agent-mcp-endpoints/ep1/keys" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatal(err)
	}
	if body["label"] != "phone" {
		t.Errorf("body = %s", gotBody)
	}
	if secret.Token != "emt_key" || secret.Label != "phone" || secret.MCPURL == "" {
		t.Fatalf("secret = %+v", secret)
	}
}

func TestRevokeAgentMCPKey(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_, _ = io.WriteString(w, `{"status":"revoked"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	if err := m.RevokeAgentMCPKey(context.Background(), "k1"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/projects/proj-1/agent-mcp-keys/k1" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
}

func TestRotateAgentMCPKey(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"key":{"id":"k1","label":"laptop","status":"active"},"token":"emt_new","mcpUrl":"https://mem.example/api/mcp/agents/a1"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	secret, err := m.RotateAgentMCPKey(context.Background(), "k1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/projects/proj-1/agent-mcp-keys/k1/rotate" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if secret.Token != "emt_new" || secret.Label != "laptop" {
		t.Fatalf("secret = %+v", secret)
	}
}

func TestListAgentMCPSessions(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"sessions":[{"session_id":"s1","key_id":"k1","key_label":"laptop","status":"active","turn_count":2,"total_steps":7,"created_at":"2026-08-01T10:00:00Z","last_active_at":"2026-08-02T10:00:00Z"}]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	sessions, err := m.ListAgentMCPSessions(context.Background(), "ep1", "active")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj-1/agent-mcp-endpoints/ep1/sessions" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "status=active" {
		t.Errorf("query = %q", gotQuery)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "s1" || sessions[0].TurnCount != 2 || sessions[0].KeyLabel != "laptop" {
		t.Fatalf("sessions = %+v", sessions)
	}
}

// TestAgentMCPKeyListNeverCarriesToken asserts the decoded key model has no
// token field at all — a backend that echoed one still cannot leak it through
// the list path.
func TestAgentMCPKeyListNeverCarriesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"keys":[{"id":"k1","label":"laptop","token":"should_not_decode"}]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	keys, err := m.ListAgentMCPKeys(context.Background(), "ep1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); strings.Contains(got, "should_not_decode") || strings.Contains(got, "token") {
		t.Fatalf("list model must not carry a token: %s", got)
	}
}
