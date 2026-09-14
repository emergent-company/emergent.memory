package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- Per-agent MCP sharing web UI + JSON handlers ---
//
// Page surface (/agents/:id/mcp-shares), nested under the agent's sub-nav:
// list (GET), create (POST — renders the one-time key reveal directly, no
// redirect), revoke (POST .../:shareId/revoke, PRG), and rotate
// (POST .../:shareId/rotate — renders the new key once, no redirect).
//
// JSON echo surface (/api/...): list (per-agent and project-wide), create,
// revoke (DELETE), and rotate proxies behind the shared auth boundary. The raw
// token appears ONLY in create and rotate responses; list responses never carry
// it.

// agentMCPSharesBase is the management path for one agent's MCP shares.
func agentMCPSharesBase(agentID string) string {
	return "/agents/" + url.PathEscape(agentID) + "/mcp-shares"
}

// agentMCPSharesPageData is the payload for the agent MCP shares page. Reveal
// carries a just-created/rotated one-time key — rendered on this one response
// only and nil on every normal list load.
type agentMCPSharesPageData struct {
	Agent     *AgentDefinition
	Shares    []AgentMCPShare
	SharesErr error // list/backend failure (degrades the section, not the page)
	LoadErr   error // agent fetch failure (whole-page error)
	FlashMsg  string
	FlashErr  error
	Reveal    *agentMCPShareReveal
}

// agentMCPShareReveal is the one-time key panel shown after create/rotate. It
// lives only for the lifetime of that one response.
type agentMCPShareReveal struct {
	Heading  string
	Name     string
	Token    string
	MCPURL   string
	Snippets []agentMCPShareSnippet
}

// --- page: list ---

// uiAgentMCPShares renders one agent's MCP shares (GET /agents/:id/mcp-shares).
func (s *Server) uiAgentMCPShares(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	agent, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Agent"), AgentMCPSharesPage(agentMCPSharesPageData{LoadErr: err}))
	}
	data := agentMCPSharesPageData{Agent: agent}
	switch {
	case c.QueryParam("created") != "":
		data.FlashMsg = "MCP share created."
	case c.QueryParam("revoked") != "":
		data.FlashMsg = "MCP share revoked."
	}
	data.FlashErr = flashError(c)
	data.Shares, data.SharesErr = s.loadAgentMCPShares(ctx, id)
	return s.page(c, pageTitle(agent.Name, "MCP"), AgentMCPSharesPage(data))
}

// renderAgentMCPShares re-renders the list after a create/rotate with an
// optional one-time key reveal open (no redirect — the raw key exists only in
// that response). A list failure still renders the reveal so the key is never
// lost.
func (s *Server) renderAgentMCPShares(c echo.Context, agent *AgentDefinition, reveal *agentMCPShareReveal, flashErr error) error {
	data := agentMCPSharesPageData{Agent: agent, Reveal: reveal, FlashErr: flashErr}
	data.Shares, data.SharesErr = s.loadAgentMCPShares(c.Request().Context(), agent.ID)
	return s.page(c, pageTitle(agent.Name, "MCP"), AgentMCPSharesPage(data))
}

// --- page: create ---

// uiAgentMCPShareCreate handles POST /agents/:id/mcp-shares. On success it
// renders the list with the one-time key reveal (no redirect).
func (s *Server) uiAgentMCPShareCreate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	agent, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Agent"), AgentMCPSharesPage(agentMCPSharesPageData{LoadErr: err}))
	}
	in := &AgentMCPShareInput{
		Name:        strings.TrimSpace(c.FormValue("name")),
		Description: strings.TrimSpace(c.FormValue("description")),
	}
	created, err := s.memory.CreateAgentMCPShare(ctx, id, in)
	if err != nil {
		return s.renderAgentMCPShares(c, agent, nil, err)
	}
	reveal := agentMCPShareRevealFromCreated("MCP share created", created)
	return s.renderAgentMCPShares(c, agent, &reveal, nil)
}

// --- page: revoke ---

// uiAgentMCPShareRevoke handles POST /agents/:id/mcp-shares/:shareId/revoke
// (PRG). The revoked share keeps rendering with a revoked status.
func (s *Server) uiAgentMCPShareRevoke(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	shareID := strings.TrimSpace(c.Param("shareId"))
	if shareID == "" {
		return redirectWithError(c, agentMCPSharesBase(id), errAgentShareIDRequired())
	}
	if err := s.memory.RevokeAgentMCPShare(ctx, shareID); err != nil {
		return redirectWithError(c, agentMCPSharesBase(id), err)
	}
	return c.Redirect(http.StatusSeeOther, agentMCPSharesBase(id)+"?revoked=1")
}

// --- page: rotate ---

// uiAgentMCPShareRotate handles POST /agents/:id/mcp-shares/:shareId/rotate.
// Success renders the new key once (no redirect); the previous key stops
// working immediately.
func (s *Server) uiAgentMCPShareRotate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	shareID := strings.TrimSpace(c.Param("shareId"))

	agent, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return redirectWithError(c, agentMCPSharesBase(id), err)
	}
	if shareID == "" {
		return s.renderAgentMCPShares(c, agent, nil, errAgentShareIDRequired())
	}
	// Best-effort name lookup so the reveal names the share it belongs to.
	name := ""
	if shares, err := s.memory.ListAgentMCPShares(ctx, id); err == nil {
		if sh := agentMCPShareFindByID(shares, shareID); sh != nil {
			name = sh.Name
		}
	}
	created, err := s.memory.RotateAgentMCPShare(ctx, shareID)
	if err != nil {
		return s.renderAgentMCPShares(c, agent, nil, err)
	}
	reveal := agentMCPShareRevealFromCreated("MCP key rotated", created)
	if reveal.Name == "" {
		reveal.Name = name
	}
	return s.renderAgentMCPShares(c, agent, &reveal, nil)
}

// --- JSON surface (behind the shared /api auth boundary) ---

// listAgentMCPShares handles GET /api/agents/:id/mcp-shares (never the token).
func (s *Server) listAgentMCPShares(c echo.Context) error {
	agentID := strings.TrimSpace(c.Param("id"))
	if agentID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "agent id is required"})
	}
	shares, err := s.memory.ListAgentMCPShares(c.Request().Context(), agentID)
	if err != nil {
		return agentMCPShareJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, agentMCPShareListJSON(shares))
}

// listProjectAgentMCPShares handles GET /api/agent-mcp-shares (never the token).
func (s *Server) listProjectAgentMCPShares(c echo.Context) error {
	shares, err := s.memory.ListProjectAgentMCPShares(c.Request().Context())
	if err != nil {
		return agentMCPShareJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, agentMCPShareListJSON(shares))
}

// createAgentMCPShare handles POST /api/agents/:id/mcp-share and returns the
// one-time token alongside the share.
func (s *Server) createAgentMCPShare(c echo.Context) error {
	agentID := strings.TrimSpace(c.Param("id"))
	if agentID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "agent id is required"})
	}
	in := AgentMCPShareInput{}
	if c.Request().ContentLength != 0 {
		if err := c.Bind(&in); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
		}
	}
	created, err := s.memory.CreateAgentMCPShare(c.Request().Context(), agentID, &in)
	if err != nil {
		return agentMCPShareJSONErr(c, err)
	}
	return c.JSON(http.StatusCreated, agentMCPShareCreatedJSON(created))
}

// deleteAgentMCPShare handles DELETE /api/agent-mcp-shares/:id (revoke).
func (s *Server) deleteAgentMCPShare(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errAgentShareIDRequired().Error()})
	}
	if err := s.memory.RevokeAgentMCPShare(c.Request().Context(), id); err != nil {
		return agentMCPShareJSONErr(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// rotateAgentMCPShare handles POST /api/agent-mcp-shares/:id/rotate and returns
// the new one-time token.
func (s *Server) rotateAgentMCPShare(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errAgentShareIDRequired().Error()})
	}
	created, err := s.memory.RotateAgentMCPShare(c.Request().Context(), id)
	if err != nil {
		return agentMCPShareJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, agentMCPShareCreatedJSON(created))
}

// --- shared helpers ---

// loadAgentMCPShares fetches one agent's shares sorted newest-first. The error
// is returned (not captured) so the page can render its section error state.
func (s *Server) loadAgentMCPShares(ctx context.Context, agentID string) ([]AgentMCPShare, error) {
	shares, err := s.memory.ListAgentMCPShares(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return agentMCPSharesSortedNewestFirst(shares), nil
}

// agentMCPShareListJSON normalizes a share list for JSON output (an empty list
// serializes as [] rather than null).
func agentMCPShareListJSON(shares []AgentMCPShare) []AgentMCPShare {
	if shares == nil {
		return []AgentMCPShare{}
	}
	return shares
}

// agentMCPShareCreatedJSON returns the create/rotate response with the one-time
// token and a complete snippet set. Only ever used by create and rotate.
func agentMCPShareCreatedJSON(created *AgentMCPShareCreated) map[string]any {
	if created == nil {
		return map[string]any{}
	}
	return map[string]any{
		"share":    created.Share,
		"token":    created.Secret.Token,
		"mcpUrl":   created.Secret.MCPURL,
		"snippets": agentMCPShareSnippetsMap(created.Secret),
	}
}

// agentMCPShareSnippetsMap returns the four canonical client snippets keyed by
// client, preferring backend-provided text and generating the rest.
func agentMCPShareSnippetsMap(secret AgentMCPShareSecret) map[string]string {
	out := map[string]string{}
	for _, s := range agentMCPShareSnippets(secret) {
		out[s.Key] = s.Code
	}
	return out
}

// agentMCPShareRevealFromCreated builds the reveal payload from a create/rotate
// result.
func agentMCPShareRevealFromCreated(heading string, created *AgentMCPShareCreated) agentMCPShareReveal {
	if created == nil {
		return agentMCPShareReveal{Heading: heading}
	}
	return agentMCPShareReveal{
		Heading:  heading,
		Name:     created.Share.Name,
		Token:    created.Secret.Token,
		MCPURL:   created.Secret.MCPURL,
		Snippets: agentMCPShareSnippets(created.Secret),
	}
}

// agentMCPShareJSONErr writes a {"error": msg} JSON response, preserving the
// backend's HTTP status when the memory error carries one (409/422 pass
// through) and falling back to 502 otherwise.
func agentMCPShareJSONErr(c echo.Context, err error) error {
	if err == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "unknown error"})
	}
	return c.JSON(agentMCPShareUpstreamStatus(err), map[string]string{"error": err.Error()})
}

// agentMCPShareUpstreamStatus extracts the status from a "memory <code> ..."
// error string, else returns 502.
func agentMCPShareUpstreamStatus(err error) int {
	msg := err.Error()
	if rest, ok := strings.CutPrefix(msg, "memory "); ok {
		if i := strings.IndexByte(rest, ' '); i > 0 {
			if code, e := strconv.Atoi(rest[:i]); e == nil && code >= 400 && code < 600 {
				return code
			}
		}
	}
	return http.StatusBadGateway
}

func errAgentShareIDRequired() error { return errors.New("share id is required") }
