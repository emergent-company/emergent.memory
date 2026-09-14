package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- per-agent MCP share route + handler tests ---

// agentMCPSharesTestServer wires the agent MCP share UI and JSON routes exactly
// as main.go does.
func agentMCPSharesTestServer(s *Server) *echo.Echo {
	e := echo.New()
	e.GET("/agents/:id/mcp-shares", s.uiAgentMCPShares)
	e.POST("/agents/:id/mcp-shares", s.uiAgentMCPShareCreate)
	e.POST("/agents/:id/mcp-shares/:shareId/revoke", s.uiAgentMCPShareRevoke)
	e.POST("/agents/:id/mcp-shares/:shareId/rotate", s.uiAgentMCPShareRotate)
	e.POST("/api/agents/:id/mcp-share", s.createAgentMCPShare)
	e.GET("/api/agents/:id/mcp-shares", s.listAgentMCPShares)
	e.GET("/api/agent-mcp-shares", s.listProjectAgentMCPShares)
	e.DELETE("/api/agent-mcp-shares/:id", s.deleteAgentMCPShare)
	e.POST("/api/agent-mcp-shares/:id/rotate", s.rotateAgentMCPShare)
	return e
}

// agentMCPSharesFixture builds a fake backend with one agent and (optionally)
// one active share.
func agentMCPSharesFixture() *fakeMemory {
	return &fakeMemory{
		defs:        map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}},
		agentShares: []AgentMCPShare{{ID: "sh1", AgentID: "a1", Name: "support", Description: "hand to Claude", Status: "active", CreatedAt: "2026-08-01T10:00:00Z"}},
	}
}

func agentSharePost(e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	return rec
}

func agentShareJSON(e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	return rec
}

// TestCreateAgentMCPShareJSONReturnsToken asserts the create proxy forwards to
// the backend and returns the share plus the one-time token.
func TestCreateAgentMCPShareJSONReturnsToken(t *testing.T) {
	f := agentMCPSharesFixture()
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPSharesTestServer(s)

	rec := agentShareJSON(e, http.MethodPost, "/api/agents/a1/mcp-share", `{"name":"support","description":"hand to Claude"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.lastAgentShareAgent != "a1" || f.lastAgentShareInput == nil || f.lastAgentShareInput.Name != "support" {
		t.Fatalf("create not forwarded: agent=%q input=%+v", f.lastAgentShareAgent, f.lastAgentShareInput)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"share", "token", "mcpUrl", "snippets"} {
		if _, ok := got[key]; !ok {
			t.Errorf("create response missing %q", key)
		}
	}
	if !strings.Contains(rec.Body.String(), "tok_agent") {
		t.Error("create response must carry the one-time token")
	}
}

// TestRotateAgentMCPShareJSONReturnsToken asserts rotate also returns the new
// one-time token.
func TestRotateAgentMCPShareJSONReturnsToken(t *testing.T) {
	f := agentMCPSharesFixture()
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPSharesTestServer(s)

	rec := agentShareJSON(e, http.MethodPost, "/api/agent-mcp-shares/sh1/rotate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.lastRotatedAgentShare != "sh1" {
		t.Fatalf("rotate not forwarded: %q", f.lastRotatedAgentShare)
	}
	if !strings.Contains(rec.Body.String(), "tok_agent") {
		t.Error("rotate response must carry the new one-time token")
	}
}

// TestListAgentMCPShareJSONHasNoToken asserts list responses never contain the
// raw key.
func TestListAgentMCPShareJSONHasNoToken(t *testing.T) {
	f := agentMCPSharesFixture()
	f.agentShares[0].Status = "active"
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPSharesTestServer(s)

	for _, path := range []string{"/api/agents/a1/mcp-shares", "/api/agent-mcp-shares"} {
		rec := agentShareJSON(e, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "tok_agent") || strings.Contains(rec.Body.String(), "\"token\"") {
			t.Errorf("%s leaked a token: %s", path, rec.Body.String())
		}
	}
}

// TestDeleteAgentMCPShareJSON asserts revoke forwards and returns 204.
func TestDeleteAgentMCPShareJSON(t *testing.T) {
	f := agentMCPSharesFixture()
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPSharesTestServer(s)

	rec := agentShareJSON(e, http.MethodDelete, "/api/agent-mcp-shares/sh1", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if f.lastRevokedAgentShare != "sh1" {
		t.Fatalf("revoke not forwarded: %q", f.lastRevokedAgentShare)
	}
}

// TestAgentMCPShareJSONUnauthenticated asserts the shared auth boundary rejects
// an unauthenticated request before any handler runs.
func TestAgentMCPShareJSONUnauthenticated(t *testing.T) {
	f := agentMCPSharesFixture()
	s := &Server{cfg: Config{AuthMode: "session", DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/api/agent-mcp-shares", s.listProjectAgentMCPShares, s.requireSessionOrKey)

	rec := agentShareJSON(e, http.MethodGet, "/api/agent-mcp-shares", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "support") {
		t.Errorf("unauthorized response leaked share data: %s", rec.Body.String())
	}
}

// TestAgentMCPShareJSONBackendErrorPassthrough asserts a backend 403/409/422 is
// surfaced with the backend's status and message (a non-admin create is
// forbidden; duplicates conflict; validation fails as unprocessable).
func TestAgentMCPShareJSONBackendErrorPassthrough(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"forbidden", errors.New("memory 403 forbidden: project admin required"), http.StatusForbidden},
		{"conflict", errors.New(`memory 409 conflict: a share named "support" already exists`), http.StatusConflict},
		{"unprocessable", errors.New("memory 422 validation: name is too long"), http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := agentMCPSharesFixture()
			f.agentShareCreateErr = tc.err
			s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
			e := agentMCPSharesTestServer(s)

			rec := agentShareJSON(e, http.MethodPost, "/api/agents/a1/mcp-share", `{"name":"support"}`)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.status, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "error") {
				t.Errorf("error body missing: %s", rec.Body.String())
			}
		})
	}
}

// TestAgentMCPShareSecretNotLogged asserts create/rotate never write the raw
// key to the standard logger.
func TestAgentMCPShareSecretNotLogged(t *testing.T) {
	f := agentMCPSharesFixture()
	f.agentShareToken = "super-secret-key"
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPSharesTestServer(s)

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	_ = agentShareJSON(e, http.MethodPost, "/api/agents/a1/mcp-share", `{"name":"support"}`)
	_ = agentShareJSON(e, http.MethodPost, "/api/agent-mcp-shares/sh1/rotate", "")

	if strings.Contains(buf.String(), "super-secret-key") {
		t.Fatalf("raw key reached the logger: %s", buf.String())
	}
}

// --- UI flows ---

// TestUIAgentMCPShareCreateRendersReveal asserts a create POST renders the list
// with the one-time reveal (no redirect) carrying the key and snippets.
func TestUIAgentMCPShareCreateRendersReveal(t *testing.T) {
	f := agentMCPSharesFixture()
	f.agentShareToken = "emt_agent_key"
	f.agentShareMCPURL = "https://mem.example/api/mcp/agents/a1"
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPSharesTestServer(s)

	rec := agentSharePost(e, "/agents/a1/mcp-shares", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"data-agent-mcp-share-reveal", "MCP share created", "emt_agent_key",
		"https://mem.example/api/mcp/agents/a1",
		"will not be shown again",
		"Claude Desktop", "Claude Code", "Cursor", "Cloud Code",
		"data-copy-target",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("create reveal missing %q", want)
		}
	}
}

// TestUIAgentMCPShareListRendersShares asserts the list shows status,
// timestamps, actions, and never a key.
func TestUIAgentMCPShareListRendersShares(t *testing.T) {
	f := agentMCPSharesFixture()
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPSharesTestServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1/mcp-shares", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"support", "active", "created", "last used",
		`data-agent-mcp-share-row="sh1"`,
		"/agents/a1/mcp-shares/sh1/revoke", "/agents/a1/mcp-shares/sh1/rotate",
		"Share as MCP tool",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
	if strings.Contains(body, "data-agent-mcp-share-reveal") {
		t.Error("list view must not render the reveal modal")
	}
}

// TestUIAgentMCPShareEmptyState asserts the empty state offers the create CTA.
func TestUIAgentMCPShareEmptyState(t *testing.T) {
	f := &fakeMemory{defs: map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPSharesTestServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1/mcp-shares", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Not shared yet") {
		t.Fatalf("empty state missing: status=%d", rec.Code)
	}
}

// TestUIAgentMCPShareListError asserts a backend list failure renders an error
// state with the rest of the page intact.
func TestUIAgentMCPShareListError(t *testing.T) {
	f := agentMCPSharesFixture()
	f.agentShareErr = errors.New("backend unreachable")
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPSharesTestServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1/mcp-shares", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Failed to load MCP shares") || !strings.Contains(body, "backend unreachable") {
		t.Errorf("error state missing: %s", body)
	}
}

// TestUIAgentMCPShareRevoke asserts revoke PRG-redirects to the list.
func TestUIAgentMCPShareRevoke(t *testing.T) {
	f := agentMCPSharesFixture()
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPSharesTestServer(s)

	rec := agentSharePost(e, "/agents/a1/mcp-shares/sh1/revoke", "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/agents/a1/mcp-shares?revoked=1" {
		t.Errorf("location = %q", loc)
	}
	if f.lastRevokedAgentShare != "sh1" {
		t.Errorf("revoke not forwarded: %q", f.lastRevokedAgentShare)
	}
}

// TestUIAgentMCPShareRotateRendersReveal asserts rotate renders the new key
// once with the share's name in the heading copy.
func TestUIAgentMCPShareRotateRendersReveal(t *testing.T) {
	f := agentMCPSharesFixture()
	f.agentShareToken = "emt_rotated"
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPSharesTestServer(s)

	rec := agentSharePost(e, "/agents/a1/mcp-shares/sh1/rotate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"data-agent-mcp-share-reveal", "MCP key rotated", "emt_rotated", "support"} {
		if !strings.Contains(body, want) {
			t.Errorf("rotate reveal missing %q", want)
		}
	}
	if f.lastRotatedAgentShare != "sh1" {
		t.Errorf("rotate not forwarded: %q", f.lastRotatedAgentShare)
	}
}

// TestUIAgentMCPShareRevokedCannotRotate asserts a revoked share omits the
// rotate action (and, with it, the rotate dialog).
func TestUIAgentMCPShareRevokedCannotRotate(t *testing.T) {
	f := agentMCPSharesFixture()
	f.agentShares[0].Status = "revoked"
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPSharesTestServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1/mcp-shares", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "revoked") {
		t.Error("revoked status not rendered")
	}
	if strings.Contains(body, "/rotate") || strings.Contains(body, "agent-mcp-share-rotate-sh1") {
		t.Error("revoked share must not offer rotation")
	}
}
