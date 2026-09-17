package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// --- Agent MCP endpoint UI + JSON echo handlers ---
//
// Page surface — on the agent's own configuration surface (the agent Settings
// page), not a separate MCP-sharing page:
//   - create the endpoint  (POST /agents/:id/mcp-endpoint, PRG)
//   - revoke the endpoint  (POST /agents/:id/mcp-endpoint/revoke, PRG)
//   - create a labeled key (POST /agents/:id/mcp-endpoint/keys — renders the
//     one-time secret reveal directly, no redirect: the raw secret exists only
//     in that response and is never persisted)
//   - revoke one key       (POST /agents/:id/mcp-endpoint/keys/:keyId/revoke, PRG)
//   - rotate one key       (POST /agents/:id/mcp-endpoint/keys/:keyId/rotate —
//     renders the new secret once, no redirect)
//   - sessions partial     (GET /agents/:id/mcp-endpoint/sessions — the HTMX
//     status-filter target)
//
// JSON echo surface (/api/...): endpoint get/create/revoke, key list/create/
// revoke/rotate, and session list proxies behind the shared auth boundary. The
// raw secret appears ONLY in create-key and rotate-key responses.

// agentMCPSettingsPath is the agent settings MCP-sharing subpage — the
// configuration surface the MCP section lives on.
func agentMCPSettingsPath(agentID string) string {
	return agentSettingsSectionPath(agentID, sectionMCP)
}

// agentMCPActionErr is a create/revoke/rotate failure surfaced inline in the
// MCP section rather than as an unexpected page error.
type agentMCPActionErr struct {
	message string
}

func (e agentMCPActionErr) Error() string { return e.message }

// agentMCPReveal is the one-time key panel rendered after a key create or
// rotate. It is held only for the lifetime of that one response: the raw token
// is never stored, and no later list or reload can show it again.
type agentMCPReveal struct {
	Heading  string
	Label    string
	Token    string
	MCPURL   string
	Snippets []agentMCPKeySnippet
}

// --- endpoint flows ---

// uiAgentMCPEndpointCreate handles POST /agents/:id/mcp-endpoint (PRG). The
// backend rejects a second active endpoint with 409, which surfaces inline.
func (s *Server) uiAgentMCPEndpointCreate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	if _, err := s.memory.CreateAgentMCPEndpoint(ctx, id); err != nil {
		return s.renderAgentSettingsWithMCPErr(c, id, "Could not create the MCP endpoint", err)
	}
	return c.Redirect(http.StatusSeeOther, agentMCPSettingsPath(id)+"?mcpCreated=1#mcp")
}

// uiAgentMCPEndpointRevoke handles POST /agents/:id/mcp-endpoint/revoke (PRG).
// Revoking the endpoint invalidates every key on it; the backend revoke is
// idempotent.
func (s *Server) uiAgentMCPEndpointRevoke(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	ep, err := s.memory.GetAgentMCPEndpoint(ctx, id)
	if err != nil {
		return s.renderAgentSettingsWithMCPErr(c, id, "Could not revoke the MCP endpoint", err)
	}
	if ep == nil {
		return s.renderAgentSettingsWithMCPErr(c, id, "Could not revoke the MCP endpoint", errors.New("this agent has no active MCP endpoint"))
	}
	if err := s.memory.RevokeAgentMCPEndpoint(ctx, ep.ID); err != nil {
		return s.renderAgentSettingsWithMCPErr(c, id, "Could not revoke the MCP endpoint", err)
	}
	return c.Redirect(http.StatusSeeOther, agentMCPSettingsPath(id)+"?mcpRevoked=1#mcp")
}

// --- key flows ---

// uiAgentMCPKeyCreate handles POST /agents/:id/mcp-endpoint/keys. Success
// renders the settings page with the one-time secret reveal open (no redirect —
// the raw secret exists only in that response). A blank or duplicate label is
// surfaced inline, keeping the draft.
func (s *Server) uiAgentMCPKeyCreate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	label := strings.TrimSpace(c.FormValue("label"))
	if label == "" {
		return s.renderAgentSettingsWithMCPKeyErr(c, id, "Could not create the key", errors.New("a key label is required"), label)
	}
	ep, err := s.memory.GetAgentMCPEndpoint(ctx, id)
	switch {
	case err != nil:
		return s.renderAgentSettingsWithMCPKeyErr(c, id, "Could not create the key", err, label)
	case ep == nil:
		return s.renderAgentSettingsWithMCPKeyErr(c, id, "Could not create the key", errors.New("create the MCP endpoint before adding keys"), label)
	}
	secret, err := s.memory.CreateAgentMCPKey(ctx, ep.ID, label)
	if err != nil {
		return s.renderAgentSettingsWithMCPKeyErr(c, id, "Could not create the key", err, label)
	}
	return s.renderAgentSettingsWithMCP(c, id, agentSettingsData{MCPReveal: &agentMCPReveal{
		Heading:  "Key created",
		Label:    label,
		Token:    secret.Token,
		MCPURL:   cmpOrStr(secret.MCPURL, ep.MCPURL),
		Snippets: agentMCPKeySnippets(*secret),
	}})
}

// uiAgentMCPKeyRevoke handles POST /agents/:id/mcp-endpoint/keys/:keyId/revoke
// (PRG). Revoking one key leaves the endpoint and its other keys intact; the
// backend revoke is idempotent.
func (s *Server) uiAgentMCPKeyRevoke(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	keyID := strings.TrimSpace(c.Param("keyId"))
	if keyID == "" {
		return s.renderAgentSettingsWithMCPErr(c, id, "Could not revoke the key", errors.New("key id is required"))
	}
	if err := s.memory.RevokeAgentMCPKey(ctx, keyID); err != nil {
		return s.renderAgentSettingsWithMCPErr(c, id, "Could not revoke the key", err)
	}
	return c.Redirect(http.StatusSeeOther, agentMCPSettingsPath(id)+"?keyRevoked=1#mcp")
}

// uiAgentMCPKeyRotate handles POST /agents/:id/mcp-endpoint/keys/:keyId/rotate.
// Success renders the new secret once (no redirect); the previous token stops
// working immediately, but the key's sessions survive.
func (s *Server) uiAgentMCPKeyRotate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	keyID := strings.TrimSpace(c.Param("keyId"))
	if keyID == "" {
		return s.renderAgentSettingsWithMCPErr(c, id, "Could not rotate the key", errors.New("key id is required"))
	}
	secret, err := s.memory.RotateAgentMCPKey(ctx, keyID)
	if err != nil {
		return s.renderAgentSettingsWithMCPErr(c, id, "Could not rotate the key", err)
	}
	return s.renderAgentSettingsWithMCP(c, id, agentSettingsData{MCPReveal: &agentMCPReveal{
		Heading:  "Key rotated",
		Label:    secret.Label,
		Token:    secret.Token,
		MCPURL:   secret.MCPURL,
		Snippets: agentMCPKeySnippets(*secret),
	}})
}

// --- sessions partial ---

// uiAgentMCPSessions handles GET /agents/:id/mcp-endpoint/sessions — the HTMX
// target for the section's status filter. It renders only the sessions body
// (filter chips + table) so a filter click swaps in place with no layout shift.
func (s *Server) uiAgentMCPSessions(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	data := agentSettingsData{MCPAgentID: id, MCPSessionStatus: normalizeAgentMCPSessionStatus(c.QueryParam("sessions"))}
	ep, err := s.memory.GetAgentMCPEndpoint(ctx, id)
	switch {
	case err != nil:
		data.MCPEndpointErr = err
	case ep == nil:
		data.MCPEndpointErr = errors.New("this agent has no active MCP endpoint")
	default:
		data.MCPEndpoint = ep
		if sessions, err := s.memory.ListAgentMCPSessions(ctx, ep.ID, data.MCPSessionStatus); err != nil {
			data.MCPSessionsErr = err
		} else {
			data.MCPSessions = sessions
		}
	}
	render.RenderPartial(c.Response().Writer, c.Request(), agentMCPSessionsBody(data))
	return nil
}

// --- JSON echo surface ---

// jsonAgentMCPEndpoint handles GET /api/agents/:id/mcp-endpoint.
func (s *Server) jsonAgentMCPEndpoint(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return agentMCPJSONError(c, http.StatusBadRequest, "agent id is required")
	}
	ep, err := s.memory.GetAgentMCPEndpoint(c.Request().Context(), id)
	if err != nil {
		return agentMCPJSONErr(c, err)
	}
	if ep == nil {
		return agentMCPJSONError(c, http.StatusNotFound, "this agent has no active MCP endpoint")
	}
	return c.JSON(http.StatusOK, ep)
}

// jsonCreateAgentMCPEndpoint handles POST /api/agents/:id/mcp-endpoint.
func (s *Server) jsonCreateAgentMCPEndpoint(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return agentMCPJSONError(c, http.StatusBadRequest, "agent id is required")
	}
	ep, err := s.memory.CreateAgentMCPEndpoint(c.Request().Context(), id)
	if err != nil {
		return agentMCPJSONErr(c, err)
	}
	return c.JSON(http.StatusCreated, ep)
}

// jsonDeleteAgentMCPEndpoint handles DELETE /api/agent-mcp-endpoints/:id.
func (s *Server) jsonDeleteAgentMCPEndpoint(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return agentMCPJSONError(c, http.StatusBadRequest, "endpoint id is required")
	}
	if err := s.memory.RevokeAgentMCPEndpoint(c.Request().Context(), id); err != nil {
		return agentMCPJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "revoked"})
}

// jsonListAgentMCPKeys handles GET /api/agent-mcp-endpoints/:id/keys and never
// carries the secret.
func (s *Server) jsonListAgentMCPKeys(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return agentMCPJSONError(c, http.StatusBadRequest, "endpoint id is required")
	}
	keys, err := s.memory.ListAgentMCPKeys(c.Request().Context(), id)
	if err != nil {
		return agentMCPJSONErr(c, err)
	}
	if keys == nil {
		keys = []AgentMCPKey{}
	}
	return c.JSON(http.StatusOK, map[string]any{"keys": keys, "total": len(keys)})
}

// jsonCreateAgentMCPKey handles POST /api/agent-mcp-endpoints/:id/keys and
// returns the raw token exactly once.
func (s *Server) jsonCreateAgentMCPKey(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return agentMCPJSONError(c, http.StatusBadRequest, "endpoint id is required")
	}
	var in struct {
		Label string `json:"label"`
	}
	if c.Request().ContentLength != 0 {
		if err := c.Bind(&in); err != nil {
			return agentMCPJSONError(c, http.StatusBadRequest, "invalid body: "+err.Error())
		}
	}
	secret, err := s.memory.CreateAgentMCPKey(c.Request().Context(), id, strings.TrimSpace(in.Label))
	if err != nil {
		return agentMCPJSONErr(c, err)
	}
	return c.JSON(http.StatusCreated, secret)
}

// jsonDeleteAgentMCPKey handles DELETE /api/agent-mcp-keys/:id.
func (s *Server) jsonDeleteAgentMCPKey(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return agentMCPJSONError(c, http.StatusBadRequest, "key id is required")
	}
	if err := s.memory.RevokeAgentMCPKey(c.Request().Context(), id); err != nil {
		return agentMCPJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "revoked"})
}

// jsonRotateAgentMCPKey handles POST /api/agent-mcp-keys/:id/rotate and returns
// the new raw token exactly once.
func (s *Server) jsonRotateAgentMCPKey(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return agentMCPJSONError(c, http.StatusBadRequest, "key id is required")
	}
	secret, err := s.memory.RotateAgentMCPKey(c.Request().Context(), id)
	if err != nil {
		return agentMCPJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, secret)
}

// jsonListAgentMCPSessions handles GET /api/agent-mcp-endpoints/:id/sessions.
func (s *Server) jsonListAgentMCPSessions(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return agentMCPJSONError(c, http.StatusBadRequest, "endpoint id is required")
	}
	sessions, err := s.memory.ListAgentMCPSessions(c.Request().Context(), id, normalizeAgentMCPSessionStatus(c.QueryParam("status")))
	if err != nil {
		return agentMCPJSONErr(c, err)
	}
	if sessions == nil {
		sessions = []AgentMCPSession{}
	}
	return c.JSON(http.StatusOK, map[string]any{"sessions": sessions})
}

// --- shared helpers ---

// loadAgentMCP fills the MCP section of a settings payload: the agent's
// endpoint, its labeled keys, and the external sessions those keys created.
// Each fetch failure degrades only its own block (leaving the rest of the page
// intact) rather than failing the whole page.
func (s *Server) loadAgentMCP(ctx context.Context, agentID string, data *agentSettingsData) {
	data.MCPAgentID = agentID
	data.MCPEndpointErr = nil
	data.MCPKeysErr = nil
	data.MCPSessionsErr = nil

	ep, err := s.memory.GetAgentMCPEndpoint(ctx, agentID)
	if err != nil {
		data.MCPEndpointErr = err
		return
	}
	data.MCPEndpoint = ep
	if ep == nil {
		return
	}
	if keys, err := s.memory.ListAgentMCPKeys(ctx, ep.ID); err != nil {
		data.MCPKeysErr = err
	} else {
		data.MCPKeys = agentMCPKeysSortedNewestFirst(keys)
	}
	if sessions, err := s.memory.ListAgentMCPSessions(ctx, ep.ID, data.MCPSessionStatus); err != nil {
		data.MCPSessionsErr = err
	} else {
		data.MCPSessions = sessions
	}
}

// renderAgentSettingsWithMCP renders the settings page for one agent with any
// extra MCP fields already set (a reveal, a draft label). It loads the rest of
// the payload the same way GET /agents/:id/settings does.
func (s *Server) renderAgentSettingsWithMCP(c echo.Context, agentID string, extra agentSettingsData) error {
	ctx := c.Request().Context()
	data := extra
	data.Section = sectionMCP
	if err := s.loadAgentSettings(ctx, agentID, &data); err != nil {
		data.LoadErr = err
		return s.page(c, pageTitle("Agent settings"), AgentSettingsPage(data))
	}
	s.loadAgentMCP(ctx, agentID, &data)
	return s.page(c, pageTitle(data.Agent.Name, "Settings"), AgentSettingsPage(data))
}

// agentMCPErrText returns the human part of a backend error: the message alone
// when the error carries one (so the UI never shows "memory 409 conflict:"
// noise), else the error's own text.
func agentMCPErrText(err error) string {
	var he *memoryHTTPError
	if errors.As(err, &he) && strings.TrimSpace(he.Message) != "" {
		return he.Message
	}
	return err.Error()
}

// renderAgentSettingsWithMCPErr renders the settings page with an inline MCP
// action error (e.g. a duplicate label, a non-admin 403, a cross-project 404)
// so the failure reads as an ordinary message instead of raw JSON.
func (s *Server) renderAgentSettingsWithMCPErr(c echo.Context, agentID, action string, err error) error {
	return s.renderAgentSettingsWithMCP(c, agentID, agentSettingsData{MCPActionErr: agentMCPActionErr{message: action + ": " + agentMCPErrText(err)}})
}

// renderAgentSettingsWithMCPKeyErr is renderAgentSettingsWithMCPErr plus the
// key-label draft, so a failed key create keeps what the user typed.
func (s *Server) renderAgentSettingsWithMCPKeyErr(c echo.Context, agentID, action string, err error, label string) error {
	return s.renderAgentSettingsWithMCP(c, agentID, agentSettingsData{
		MCPKeyDraft:  label,
		MCPActionErr: agentMCPActionErr{message: action + ": " + agentMCPErrText(err)},
	})
}

// normalizeAgentMCPSessionStatus keeps only the statuses the session filter
// offers; anything else means "all".
func normalizeAgentMCPSessionStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "interrupted", "expired":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return ""
	}
}

// agentMCPSessionFilter is one chip in the sessions status filter.
type agentMCPSessionFilter struct {
	Value string // ?sessions= value ("" = all)
	Label string
}

// agentMCPSessionFilters is the canonical session status filter order.
var agentMCPSessionFilters = []agentMCPSessionFilter{
	{"", "All"},
	{"active", "Active"},
	{"interrupted", "Interrupted"},
	{"expired", "Expired"},
}

// agentMCPSessionFilterActive reports whether the filter chip is the current
// selection.
func agentMCPSessionFilterActive(current, value string) bool {
	return normalizeAgentMCPSessionStatus(current) == value
}

// agentMCPJSONErr writes a {"error": msg} JSON response, preserving the
// backend's HTTP status when the memory error carries one (403/404/409 pass
// through) and falling back to 502 otherwise.
func agentMCPJSONErr(c echo.Context, err error) error {
	if err == nil {
		return agentMCPJSONError(c, http.StatusInternalServerError, "unknown error")
	}
	status := memoryStatus(err)
	if status < 400 || status > 599 {
		status = agentMCPUpstreamStatus(err)
	}
	return agentMCPJSONError(c, status, err.Error())
}

// agentMCPUpstreamStatus extracts the status from a "memory <code> ..." error
// string, else returns 502. It backs up memoryStatus for errors that arrive
// without the typed status.
func agentMCPUpstreamStatus(err error) int {
	if rest, ok := strings.CutPrefix(err.Error(), "memory "); ok {
		if i := strings.IndexByte(rest, ' '); i > 0 {
			if code, e := strconv.Atoi(rest[:i]); e == nil && code >= 400 && code < 600 {
				return code
			}
		}
	}
	return http.StatusBadGateway
}

// agentMCPJSONError writes a {"error": msg} JSON response with an explicit
// status.
func agentMCPJSONError(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]string{"error": msg})
}

// cmpOrStr returns a when non-blank, else b.
func cmpOrStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
