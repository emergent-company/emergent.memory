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

// --- per-agent MCP share client methods ---
//
// These cover the MemoryClient proxies for memory's per-agent share routes
// under /api/projects/:projectId. The project id rides in both the path (via
// projectIDFor) and the X-Project-ID session header; the raw key comes back
// only from create and rotate.

// agentShareSessionCtx returns a session-scoped context pinned to proj-1 so the
// path substitution and the X-Project-ID header can be asserted together.
func agentShareSessionCtx() context.Context {
	return withSessionContext(context.Background(), &sessionContext{Token: "sess", ProjectID: "proj-1"})
}

func TestListAgentMCPShares(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":[{"id":"sh1","agentId":"a1","name":"support","status":"active","createdAt":"2026-08-01T10:00:00Z"}]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	shares, err := m.ListAgentMCPShares(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/projects/proj-1/agents/a1/mcp-shares" {
		t.Errorf("request = %s %s, want GET /api/projects/proj-1/agents/a1/mcp-shares", gotMethod, gotPath)
	}
	if len(shares) != 1 || shares[0].ID != "sh1" || shares[0].Name != "support" {
		t.Fatalf("shares = %+v", shares)
	}
}

func TestListProjectAgentMCPSharesAcceptsBareArrayAndWrapper(t *testing.T) {
	for name, body := range map[string]string{
		"bare":    `[{"id":"sh1","agentId":"a1","name":"support","status":"active"}]`,
		"wrapper": `{"success":true,"data":{"shares":[{"id":"sh1","agentId":"a1","name":"support","status":"active"}]}}`,
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
			shares, err := m.ListProjectAgentMCPShares(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if gotPath != "/api/projects/proj-1/agent-mcp-shares" {
				t.Errorf("path = %q", gotPath)
			}
			if len(shares) != 1 || shares[0].ID != "sh1" {
				t.Fatalf("shares = %+v", shares)
			}
		})
	}
}

func TestCreateAgentMCPShare(t *testing.T) {
	var gotPath, gotMethod, gotProjectHeader, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotProjectHeader = r.Header.Get("X-Project-ID")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"success":true,"data":{"share":{"id":"sh1","agentId":"a1","name":"support","status":"active"},"token":"emt_key","mcpUrl":"https://mem.example/api/mcp/agents/a1"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "fallback")
	created, err := m.CreateAgentMCPShare(agentShareSessionCtx(), "a1", &AgentMCPShareInput{Name: "support", Description: "hand to Claude"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/projects/proj-1/agents/a1/mcp-share" {
		t.Errorf("request = %s %s, want POST /api/projects/proj-1/agents/a1/mcp-share", gotMethod, gotPath)
	}
	if gotProjectHeader != "proj-1" {
		t.Errorf("X-Project-ID = %q, want proj-1", gotProjectHeader)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "support" || body["description"] != "hand to Claude" {
		t.Errorf("body = %s", gotBody)
	}
	if created.Secret.Token != "emt_key" || created.Share.ID != "sh1" {
		t.Fatalf("created = %+v", created)
	}
}

func TestRotateAgentMCPShare(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"token":"emt_new","mcpUrl":"https://mem.example/api/mcp/agents/a1"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	created, err := m.RotateAgentMCPShare(context.Background(), "sh1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/projects/proj-1/agent-mcp-shares/sh1/rotate" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if created.Secret.Token != "emt_new" || created.Secret.MCPURL != "https://mem.example/api/mcp/agents/a1" {
		t.Fatalf("rotate = %+v", created)
	}
}

func TestRevokeAgentMCPShare(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	if err := m.RevokeAgentMCPShare(context.Background(), "sh1"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/projects/proj-1/agent-mcp-shares/sh1" {
		t.Errorf("request = %s %s, want DELETE /api/projects/proj-1/agent-mcp-shares/sh1", gotMethod, gotPath)
	}
}

// TestAgentMCPShareListNeverCarriesToken asserts the decoded list model has no
// token field at all — a backend that echoed one still cannot leak it through
// the list path.
func TestAgentMCPShareListNeverCarriesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"sh1","agentId":"a1","name":"support","token":"should_not_decode"}]`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	shares, err := m.ListAgentMCPShares(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(shares)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); strings.Contains(got, "should_not_decode") || strings.Contains(got, "token") {
		t.Fatalf("list model must not carry a token: %s", got)
	}
}
