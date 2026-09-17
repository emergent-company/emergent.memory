package main

import (
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- agent MCP endpoint UI + JSON handler tests ---

// agentMCPTestServer wires the agent MCP UI and JSON routes exactly as main.go
// does (agent_mcp_endpoint_handlers.go).
func agentMCPTestServer(s *Server) *echo.Echo {
	e := echo.New()
	e.GET("/agents/:id/settings/:section", s.uiAgentSettingsSection)
	e.POST("/agents/:id/mcp-endpoint", s.uiAgentMCPEndpointCreate)
	e.POST("/agents/:id/mcp-endpoint/revoke", s.uiAgentMCPEndpointRevoke)
	e.POST("/agents/:id/mcp-endpoint/keys", s.uiAgentMCPKeyCreate)
	e.POST("/agents/:id/mcp-endpoint/keys/:keyId/revoke", s.uiAgentMCPKeyRevoke)
	e.POST("/agents/:id/mcp-endpoint/keys/:keyId/rotate", s.uiAgentMCPKeyRotate)
	e.GET("/agents/:id/mcp-endpoint/sessions", s.uiAgentMCPSessions)
	e.GET("/api/agents/:id/mcp-endpoint", s.jsonAgentMCPEndpoint)
	e.POST("/api/agents/:id/mcp-endpoint", s.jsonCreateAgentMCPEndpoint)
	e.DELETE("/api/agent-mcp-endpoints/:id", s.jsonDeleteAgentMCPEndpoint)
	e.GET("/api/agent-mcp-endpoints/:id/keys", s.jsonListAgentMCPKeys)
	e.POST("/api/agent-mcp-endpoints/:id/keys", s.jsonCreateAgentMCPKey)
	e.GET("/api/agent-mcp-endpoints/:id/sessions", s.jsonListAgentMCPSessions)
	e.DELETE("/api/agent-mcp-keys/:id", s.jsonDeleteAgentMCPKey)
	e.POST("/api/agent-mcp-keys/:id/rotate", s.jsonRotateAgentMCPKey)
	return e
}

// agentMCPFixture seeds one agent with one active endpoint, one labeled key,
// and one active session.
func agentMCPFixture() *fakeMemory {
	return &fakeMemory{
		defs:        map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}},
		mcpEndpoint: &AgentMCPEndpoint{ID: "ep1", AgentID: "a1", Status: "active", CreatedAt: "2026-08-01T10:00:00Z", MCPURL: "https://mem.example/api/mcp/agents/a1"},
		mcpKeys: []AgentMCPKey{
			{ID: "k1", EndpointID: "ep1", Label: "laptop", Status: "active", CreatedAt: "2026-08-01T10:00:00Z", LastUsedAt: strPtr("2026-08-02T10:00:00Z")},
		},
		mcpSessions: []AgentMCPSession{
			{SessionID: "s1", KeyID: "k1", KeyLabel: "laptop", Status: "active", TurnCount: 3, TotalSteps: 12, CreatedAt: "2026-08-01T10:00:00Z", LastActiveAt: "2026-08-02T10:00:00Z"},
		},
	}
}

func agentMCPGet(e *echo.Echo, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func agentMCPPost(e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	return rec
}

func agentMCPJSONReq(e *echo.Echo, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	e.ServeHTTP(rec, req)
	return rec
}

// --- settings page rendering ---

// TestAgentSettingsRendersMCPEndpoint asserts the agent's configuration surface
// carries its own MCP section: endpoint status + URL, the key label, the session
// with its key and turn count, and the create-key form — and never a secret.
func TestAgentSettingsRendersMCPEndpoint(t *testing.T) {
	f := agentMCPFixture()
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPGet(e, "/agents/a1/settings/mcp")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="agent-mcp-section"`,
		"MCP endpoint",
		`data-testid="agent-mcp-endpoint"`,
		"https://mem.example/api/mcp/agents/a1",
		`data-testid="agent-mcp-keys"`,
		"laptop",
		`data-testid="agent-mcp-key-row"`,
		`data-testid="agent-mcp-create-key"`,
		`data-testid="agent-mcp-sessions"`,
		`data-testid="agent-mcp-session-row"`,
		"turns",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page missing %q", want)
		}
	}
	if strings.Contains(body, "emt_key") || strings.Contains(body, `data-agent-mcp-reveal`) {
		t.Error("settings list must not render the reveal or a secret")
	}
}

// TestAgentSettingsMCPEmptyState asserts an agent without an endpoint renders
// the first-step create action instead of a list.
func TestAgentSettingsMCPEmptyState(t *testing.T) {
	f := &fakeMemory{defs: map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	body := agentMCPGet(e, "/agents/a1/settings/mcp").Body.String()
	for _, want := range []string{"No MCP endpoint yet", `data-testid="agent-mcp-create-endpoint"`, "/agents/a1/mcp-endpoint"} {
		if !strings.Contains(body, want) {
			t.Errorf("empty endpoint state missing %q", want)
		}
	}
	if strings.Contains(body, `data-testid="agent-mcp-keys"`) {
		t.Error("keys block must not render before an endpoint exists")
	}
}

// TestAgentSettingsMCPKeysAndSessionsEmptyStates asserts the per-block empty
// states render when the endpoint has no keys and no sessions.
func TestAgentSettingsMCPKeysAndSessionsEmptyStates(t *testing.T) {
	f := agentMCPFixture()
	f.mcpKeys = nil
	f.mcpSessions = nil
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	body := agentMCPGet(e, "/agents/a1/settings/mcp").Body.String()
	for _, want := range []string{"No keys yet", "No sessions yet", `data-testid="agent-mcp-sessions-empty"`} {
		if !strings.Contains(body, want) {
			t.Errorf("empty state missing %q", want)
		}
	}
	if strings.Contains(body, `data-testid="agent-mcp-key-row"`) {
		t.Error("no key rows should render in the keys empty state")
	}
}

// TestAgentSettingsMCPLoadErrorsAreInline asserts each MCP fetch failure
// degrades only its own block with a readable message.
func TestAgentSettingsMCPLoadErrorsAreInline(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*fakeMemory)
		want   []string
	}{
		{"endpoint", func(f *fakeMemory) { f.mcpEndpointErr = errors.New("backend unreachable") }, []string{"Failed to load this agent", "backend unreachable"}},
		{"keys", func(f *fakeMemory) { f.mcpKeyErr = errors.New("keys down") }, []string{"Failed to load keys: keys down"}},
		{"sessions", func(f *fakeMemory) { f.mcpSessionErr = errors.New("sessions down") }, []string{"Failed to load sessions: sessions down"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := agentMCPFixture()
			tc.mutate(f)
			s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
			e := agentMCPTestServer(s)

			rec := agentMCPGet(e, "/agents/a1/settings/mcp")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			body := rec.Body.String()
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Errorf("inline error %q missing", want)
				}
			}
		})
	}
}

// --- key create / rotate reveal ---

// TestAgentMCPKeyCreateRendersSecretOnce asserts a key create renders the
// one-time reveal with the raw secret, the endpoint, and the warning — and that
// a later list load never shows the secret again.
func TestAgentMCPKeyCreateRendersSecretOnce(t *testing.T) {
	f := agentMCPFixture()
	f.mcpToken = "emt_secret_once"
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPPost(e, "/agents/a1/mcp-endpoint/keys", "label=phone")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-agent-mcp-reveal`,
		"Key created",
		"emt_secret_once",
		"https://mem.example/api/mcp/agents/a1",
		"will not be shown again",
		"Claude Desktop", "Claude Code", "Cursor", "Cloud Code",
		"data-copy-target",
		"phone",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("create reveal missing %q", want)
		}
	}
	if f.lastMCPEndpointID != "ep1" || f.lastMCPLabel != "phone" {
		t.Errorf("create not forwarded: endpoint=%q label=%q", f.lastMCPEndpointID, f.lastMCPLabel)
	}

	// The secret is a one-response value: a fresh settings load must not carry it.
	list := agentMCPGet(e, "/agents/a1/settings/mcp").Body.String()
	if strings.Contains(list, "emt_secret_once") {
		t.Error("secret leaked into a later list load")
	}
}

// TestAgentMCPKeyCreateDuplicateLabelIsInline asserts a duplicate-label 409
// surfaces as a readable inline message (not raw JSON) and keeps the draft.
func TestAgentMCPKeyCreateDuplicateLabelIsInline(t *testing.T) {
	f := agentMCPFixture()
	f.mcpKeyCreateErr = errors.New(`memory 409 conflict: an active key labelled "laptop" already exists`)
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPPost(e, "/agents/a1/mcp-endpoint/keys", "label=laptop")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="agent-mcp-action-error"`) || !strings.Contains(body, "already exists") {
		t.Errorf("duplicate-label error not surfaced inline: %s", body)
	}
	if strings.Contains(body, `data-agent-mcp-reveal`) {
		t.Error("a failed create must not render a reveal")
	}
	if !strings.Contains(body, `value="laptop"`) {
		t.Error("failed create must keep the typed label")
	}
}

// TestAgentMCPKeyCreateBlankLabelIsInline asserts the client-side required
// guard is also enforced server-side with a readable message.
func TestAgentMCPKeyCreateBlankLabelIsInline(t *testing.T) {
	f := agentMCPFixture()
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPPost(e, "/agents/a1/mcp-endpoint/keys", "label=")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "a key label is required") {
		t.Errorf("blank-label error missing: %s", body)
	}
}

// TestAgentMCPKeyRotateRendersSecret asserts rotate renders the new secret once
// and names the rotated key.
func TestAgentMCPKeyRotateRendersSecret(t *testing.T) {
	f := agentMCPFixture()
	f.mcpToken = "emt_rotated"
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPPost(e, "/agents/a1/mcp-endpoint/keys/k1/rotate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Key rotated", "emt_rotated", "laptop"} {
		if !strings.Contains(body, want) {
			t.Errorf("rotate reveal missing %q", want)
		}
	}
	if f.lastMCPKeyID != "k1" {
		t.Errorf("rotate not forwarded: %q", f.lastMCPKeyID)
	}
}

// TestAgentMCPKeyRevokeRedirects asserts a revoke POST redirects back to the
// MCP section (PRG) and forwards the key id.
func TestAgentMCPKeyRevokeRedirects(t *testing.T) {
	f := agentMCPFixture()
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPPost(e, "/agents/a1/mcp-endpoint/keys/k1/revoke", "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/agents/a1/settings/mcp?keyRevoked=1#mcp" {
		t.Errorf("location = %q", loc)
	}
	if f.lastMCPKeyID != "k1" {
		t.Errorf("revoke not forwarded: %q", f.lastMCPKeyID)
	}
}

// TestAgentMCPKeyRevokedHasNoActions asserts a revoked key renders read-only
// (no rotate/revoke affordances).
func TestAgentMCPKeyRevokedHasNoActions(t *testing.T) {
	f := agentMCPFixture()
	f.mcpKeys[0].Status = "revoked"
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	body := agentMCPGet(e, "/agents/a1/settings/mcp").Body.String()
	if !strings.Contains(body, "revoked") {
		t.Error("revoked status not rendered")
	}
	if strings.Contains(body, "agent-mcp-key-rotate-k1") || strings.Contains(body, "agent-mcp-key-revoke-k1") {
		t.Error("revoked key must not offer rotate/revoke")
	}
}

// --- endpoint create / revoke ---

// TestAgentMCPEndpointCreateRedirects asserts create-endpoint PRG-redirects and
// forwards the agent id.
func TestAgentMCPEndpointCreateRedirects(t *testing.T) {
	f := &fakeMemory{defs: map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPPost(e, "/agents/a1/mcp-endpoint", "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/agents/a1/settings/mcp?mcpCreated=1#mcp" {
		t.Errorf("location = %q", loc)
	}
	if f.lastMCPAgentID != "a1" {
		t.Errorf("create not forwarded: %q", f.lastMCPAgentID)
	}
}

// TestAgentMCPEndpointCreateConflictIsInline asserts a 409 (active endpoint
// already exists) surfaces inline.
func TestAgentMCPEndpointCreateConflictIsInline(t *testing.T) {
	f := agentMCPFixture()
	f.mcpEndpointCreateErr = errors.New("memory 409 conflict: an active endpoint already exists for this agent")
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPPost(e, "/agents/a1/mcp-endpoint", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "already exists") || !strings.Contains(body, `data-testid="agent-mcp-action-error"`) {
		t.Errorf("conflict not surfaced inline: %s", body)
	}
}

// TestAgentMCPEndpointRevokeRedirects asserts revoke PRG-redirects and forwards
// the resolved endpoint id.
func TestAgentMCPEndpointRevokeRedirects(t *testing.T) {
	f := agentMCPFixture()
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPPost(e, "/agents/a1/mcp-endpoint/revoke", "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/agents/a1/settings/mcp?mcpRevoked=1#mcp" {
		t.Errorf("location = %q", loc)
	}
	if f.lastMCPEndpointID != "ep1" {
		t.Errorf("revoke not forwarded: %q", f.lastMCPEndpointID)
	}
}

// --- sessions partial ---

// TestAgentMCPSessionsPartialFiltersByStatus asserts the HTMX partial renders
// only the requested status and forwards the filter.
func TestAgentMCPSessionsPartialFiltersByStatus(t *testing.T) {
	f := agentMCPFixture()
	f.mcpSessions = append(f.mcpSessions, AgentMCPSession{SessionID: "s2", KeyID: "k1", KeyLabel: "laptop", Status: "expired", TurnCount: 1, LastActiveAt: "2026-08-01T09:00:00Z"})
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPGet(e, "/agents/a1/mcp-endpoint/sessions?sessions=expired")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if f.lastMCPSessionStatus != "expired" {
		t.Errorf("status filter not forwarded: %q", f.lastMCPSessionStatus)
	}
	if !strings.Contains(body, "expired") {
		t.Error("expired session not rendered")
	}
	if strings.Contains(body, ">3 turns<") {
		t.Error("active session must be filtered out")
	}
	if strings.Contains(body, `data-testid="agent-mcp-sessions"`) {
		t.Error("partial must render only the swappable body, not the section wrapper")
	}
	if !strings.Contains(body, "hx-target=\"#agent-mcp-sessions-body\"") {
		t.Error("partial must carry the hx-target for in-place filtering")
	}
}

// --- JSON echo surface ---

// TestAgentMCPJSONEndpointRoundTrip asserts get 404s without an endpoint,
// create returns 201, and get returns it once present.
func TestAgentMCPJSONEndpointRoundTrip(t *testing.T) {
	f := &fakeMemory{defs: map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPJSONReq(e, http.MethodGet, "/api/agents/a1/mcp-endpoint", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("empty get status = %d, want 404", rec.Code)
	}

	rec = agentMCPJSONReq(e, http.MethodPost, "/api/agents/a1/mcp-endpoint", "", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ep_new") {
		t.Errorf("create body = %s", rec.Body.String())
	}

	rec = agentMCPJSONReq(e, http.MethodGet, "/api/agents/a1/mcp-endpoint", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ep_new") {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAgentMCPJSONKeyListNeverCarriesSecret asserts the list response has no
// token field.
func TestAgentMCPJSONKeyListNeverCarriesSecret(t *testing.T) {
	f := agentMCPFixture()
	f.mcpToken = "emt_should_not_list"
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPJSONReq(e, http.MethodGet, "/api/agent-mcp-endpoints/ep1/keys", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "emt_should_not_list") || strings.Contains(body, `"token"`) {
		t.Errorf("key list leaked a secret: %s", body)
	}
	if !strings.Contains(body, `"total":1`) {
		t.Errorf("key list missing total: %s", body)
	}
}

// TestAgentMCPJSONCreateKeyReturnsSecretOnce asserts the create-key response
// carries the raw token.
func TestAgentMCPJSONCreateKeyReturnsSecretOnce(t *testing.T) {
	f := agentMCPFixture()
	f.mcpToken = "emt_json_once"
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPJSONReq(e, http.MethodPost, "/api/agent-mcp-endpoints/ep1/keys", `{"label":"desktop"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "emt_json_once") {
		t.Errorf("create response must carry the one-time token: %s", rec.Body.String())
	}
	if f.lastMCPLabel != "desktop" {
		t.Errorf("label not forwarded: %q", f.lastMCPLabel)
	}
}

// TestAgentMCPJSONSessions asserts the session list proxy returns the frozen
// {"sessions":[...]} shape and forwards the status filter.
func TestAgentMCPJSONSessions(t *testing.T) {
	f := agentMCPFixture()
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	rec := agentMCPJSONReq(e, http.MethodGet, "/api/agent-mcp-endpoints/ep1/sessions?status=active", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if f.lastMCPSessionStatus != "active" {
		t.Errorf("status not forwarded: %q", f.lastMCPSessionStatus)
	}
	if !strings.Contains(rec.Body.String(), `"session_id":"s1"`) {
		t.Errorf("sessions body = %s", rec.Body.String())
	}
}

// TestAgentMCPJSONErrorPassthrough asserts backend 403/404/409 keep their status
// and read as a message.
func TestAgentMCPJSONErrorPassthrough(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"forbidden", errors.New("memory 403 forbidden: project admin required"), http.StatusForbidden},
		{"notfound", errors.New("memory 404 not_found: endpoint not found"), http.StatusNotFound},
		{"conflict", errors.New(`memory 409 conflict: an active key labelled "laptop" already exists`), http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := agentMCPFixture()
			f.mcpKeyCreateErr = tc.err
			s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
			e := agentMCPTestServer(s)

			rec := agentMCPJSONReq(e, http.MethodPost, "/api/agent-mcp-endpoints/ep1/keys", `{"label":"laptop"}`, nil)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.status, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "error") {
				t.Errorf("error body missing: %s", rec.Body.String())
			}
		})
	}
}

// TestAgentMCPKeySecretNotLogged asserts create/rotate never write the raw
// secret to the standard logger.
func TestAgentMCPKeySecretNotLogged(t *testing.T) {
	f := agentMCPFixture()
	f.mcpToken = "super-secret-key"
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := agentMCPTestServer(s)

	var buf strings.Builder
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	_ = agentMCPPost(e, "/agents/a1/mcp-endpoint/keys", "label=phone")
	_ = agentMCPPost(e, "/agents/a1/mcp-endpoint/keys/k1/rotate", "")

	if strings.Contains(buf.String(), "super-secret-key") {
		t.Fatalf("raw secret reached the logger: %s", buf.String())
	}
}
