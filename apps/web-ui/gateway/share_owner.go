package main

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// shareLinksPageData is the assembled payload for AgentSharePage.
type shareLinksPageData struct {
	Agent    *AgentDefinition
	LoadErr  error
	FlashMsg string
	Panel    ShareLinksPanelProps
}

// --- page render ---

// uiAgentShare renders the owner-facing share-links page (GET /agents/:id/share).
func (s *Server) uiAgentShare(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()
	agent, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Share links"), AgentSharePage(shareLinksPageData{LoadErr: err}))
	}
	data := shareLinksPageData{Agent: agent}
	if c.QueryParam("revoked") != "" {
		data.FlashMsg = "Share link revoked."
	}
	data.Panel = s.sharePanelProps(ctx, agent, "", "", ShareLinkCreateValues{}, "")
	return s.page(c, pageTitle(agent.Name, "Share links"), AgentSharePage(data))
}

// renderAgentShare re-renders the share page directly (no redirect) after a
// create/rotate so the one-time URL with the key is shown in this response. The
// raw key exists only here and must never round-trip through a URL.
func (s *Server) renderAgentShare(c echo.Context, agentID, revealLinkID, revealToken, flashMsg string) error {
	ctx := c.Request().Context()
	agent, err := s.memory.GetAgentDefinition(ctx, agentID)
	if err != nil {
		return s.page(c, pageTitle("Share links"), AgentSharePage(shareLinksPageData{LoadErr: err}))
	}
	data := shareLinksPageData{Agent: agent, FlashMsg: flashMsg}
	data.Panel = s.sharePanelProps(ctx, agent, revealLinkID, revealToken, ShareLinkCreateValues{}, "")
	return s.page(c, pageTitle(agent.Name, "Share links"), AgentSharePage(data))
}

// renderAgentShareWithValues re-renders the share page after a failed submit,
// preserving the owner's form input.
func (s *Server) renderAgentShareWithValues(c echo.Context, agentID string, values ShareLinkCreateValues, formErr string) error {
	ctx := c.Request().Context()
	agent, err := s.memory.GetAgentDefinition(ctx, agentID)
	if err != nil {
		return s.page(c, pageTitle("Share links"), AgentSharePage(shareLinksPageData{LoadErr: err}))
	}
	data := shareLinksPageData{Agent: agent}
	data.Panel = s.sharePanelProps(ctx, agent, "", "", values, formErr)
	return s.page(c, pageTitle(agent.Name, "Share links"), AgentSharePage(data))
}

// --- form handlers ---

// uiAgentShareCreate handles POST /agents/:id/share-links.
func (s *Server) uiAgentShareCreate(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()
	values := shareLinkCreateValuesFromForm(c)
	label := strings.TrimSpace(c.FormValue("label"))
	if label == "" {
		return s.renderAgentShareWithValues(c, id, values, "Label is required.")
	}
	created, err := s.memory.CreateShareLink(ctx, id, ShareLinkCreateInput{Label: label, Config: shareLinkConfigInputFromForm(c)})
	if err != nil {
		return s.renderAgentShareWithValues(c, id, values, "Could not create link: "+err.Error())
	}
	return s.renderAgentShare(c, id, created.ID, created.Token, "Share link created — copy the URL below.")
}

// uiAgentShareRotate handles POST /agents/:id/share-links/:linkId/rotate. The
// new key is revealed once (no redirect), same as create.
func (s *Server) uiAgentShareRotate(c echo.Context) error {
	agentID := c.Param("id")
	linkID := c.Param("linkId")
	rotated, err := s.memory.RotateShareLink(c.Request().Context(), linkID)
	if err != nil {
		return redirectWithError(c, "/agents/"+url.PathEscape(agentID)+"/share", err)
	}
	return s.renderAgentShare(c, agentID, rotated.ID, rotated.Token, "Share link rotated — copy the new URL below.")
}

// uiAgentShareRevoke handles POST /agents/:id/share-links/:linkId/revoke.
func (s *Server) uiAgentShareRevoke(c echo.Context) error {
	agentID := c.Param("id")
	linkID := c.Param("linkId")
	back := "/agents/" + url.PathEscape(agentID) + "/share"
	if err := s.memory.RevokeShareLink(c.Request().Context(), linkID); err != nil {
		return redirectWithError(c, back, err)
	}
	return c.Redirect(http.StatusSeeOther, back+"?revoked=1")
}

// uiAgentShareReveal handles GET /agents/:id/share-links/:linkId/reveal — the
// on-demand key recovery for an existing link's "Copy link" action. It returns
// the full public URL so the client never needs the base URL. A link whose key
// is not recoverable returns code "not_recoverable", which the client maps to a
// rotate-the-link hint.
func (s *Server) uiAgentShareReveal(c echo.Context) error {
	linkID := c.Param("linkId")
	key, err := s.memory.RevealShareLink(c.Request().Context(), linkID)
	if err != nil {
		if strings.Contains(err.Error(), "not_recoverable") {
			return c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "not_recoverable", "code": "not_recoverable"})
		}
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "could not reveal link", "code": "unknown"})
	}
	return c.JSON(http.StatusOK, map[string]string{"key": key, "url": s.sharePublicURL(key)})
}

// --- panel assembly ---

// sharePanelProps builds the ShareLinksPanelProps for an agent's share-links
// management surface. revealLinkID/revealToken populate the one-time URL on the
// just-created/rotated link; every other link has no URL (the key is only ever
// present at create/rotate time).
func (s *Server) sharePanelProps(ctx context.Context, agent *AgentDefinition, revealLinkID, revealToken string, values ShareLinkCreateValues, formErr string) ShareLinksPanelProps {
	props := ShareLinksPanelProps{
		AgentID:       agent.ID,
		PublicBaseURL: s.cfg.sharePublicBase(),
		CreateAction:  "/agents/" + url.PathEscape(agent.ID) + "/share-links",
		Values:        values,
		Error:         formErr,
	}
	links, err := s.memory.ListShareLinks(ctx, agent.ID)
	if err != nil {
		props.Error = "Failed to load share links: " + err.Error()
	} else {
		sort.SliceStable(links, func(i, j int) bool { return links[i].CreatedAt.After(links[j].CreatedAt) })
		for _, l := range links {
			token := ""
			if l.ID == revealLinkID {
				token = revealToken
			}
			props.Links = append(props.Links, s.shareLinkRow(l, token))
		}
	}
	for _, t := range agent.Tools {
		props.Tools = append(props.Tools, ShareToolOption{ID: t, Label: t})
	}
	return props
}

// shareLinkRow maps a server ShareLink onto the designer's ShareLinkRow.
func (s *Server) shareLinkRow(l ShareLink, token string) ShareLinkRow {
	row := ShareLinkRow{
		ID:              l.ID,
		Label:           l.Label,
		URL:             s.sharePublicURL(token),
		Status:          shareLinkStatus(l),
		RevealURL:       "/agents/" + url.PathEscape(l.AgentDefinitionID) + "/share-links/" + url.PathEscape(l.ID) + "/reveal",
		RotateURL:       "/agents/" + url.PathEscape(l.AgentDefinitionID) + "/share-links/" + url.PathEscape(l.ID) + "/rotate",
		RevokeURL:       "/agents/" + url.PathEscape(l.AgentDefinitionID) + "/share-links/" + url.PathEscape(l.ID) + "/revoke",
		CreatedAt:       l.CreatedAt.Format(time.RFC3339),
		CreatedRelative: relTime(l.CreatedAt.Format(time.RFC3339)),
	}
	if l.LastUsedAt != nil {
		row.LastUsedAt = l.LastUsedAt.Format(time.RFC3339)
		row.LastUsedRelative = relTime(l.LastUsedAt.Format(time.RFC3339))
	}
	if l.ExpiresAt != nil {
		row.ExpiresAt = l.ExpiresAt.Format(time.RFC3339)
		row.ExpiresRelative = relTime(l.ExpiresAt.Format(time.RFC3339))
	} else {
		row.ExpiresNever = true
	}
	if l.Config != nil {
		row.RequireEmail = l.Config.RequireEmail
		row.ShowSessionList = l.Config.ShowSessionList
		row.Sandbox = l.Config.SandboxEnabled
		row.MaxSessionsPerUser = l.Config.MaxActiveSessionsPerUser
		row.BudgetMessages = l.Config.BudgetMaxMessages
		row.BudgetTokens = int(l.Config.BudgetMaxTokens)
	}
	return row
}

// shareLinkStatus maps a server link onto the designer's status enum.
func shareLinkStatus(l ShareLink) ShareLinkStatus {
	if l.RevokedAt != nil {
		return ShareLinkRevoked
	}
	if l.ExpiresAt != nil && time.Now().After(*l.ExpiresAt) {
		return ShareLinkExpired
	}
	return ShareLinkActive
}

// shareLinkCreateValuesFromForm preserves the create form across a failed
// submit so the owner's input survives.
func shareLinkCreateValuesFromForm(c echo.Context) ShareLinkCreateValues {
	expiry := strings.TrimSpace(c.FormValue("expiryDays"))
	days := 30
	if n, err := strconv.Atoi(expiry); err == nil && n >= 0 {
		days = n
	}
	return ShareLinkCreateValues{
		Label:              c.FormValue("label"),
		WelcomeMessage:     c.FormValue("welcomeMessage"),
		ExpiryDays:         days,
		NeverExpires:       expiry == "never",
		ToolIDs:            append([]string(nil), c.Request().Form["toolIds"]...),
		RequireEmail:       c.FormValue("requireEmail") == "true",
		ShowSessionList:    c.FormValue("showSessionList") == "true",
		Sandbox:            c.FormValue("sandbox") == "true",
		MaxSessionsPerUser: c.FormValue("maxSessionsPerUser"),
		BudgetMessages:     c.FormValue("budgetMessages"),
		BudgetTokens:       c.FormValue("budgetTokens"),
	}
}

// shareLinkConfigInputFromForm maps the create form onto the server's config
// input. The three toggles are sent explicitly (the server defaults them true,
// so an unchecked box must become an explicit false). Budget/limit fields are
// omitted when empty so the server defaults apply.
func shareLinkConfigInputFromForm(c echo.Context) *ShareLinkConfigInput {
	in := &ShareLinkConfigInput{
		RequireEmail:    boolPtr(c.FormValue("requireEmail") == "true"),
		ShowSessionList: boolPtr(c.FormValue("showSessionList") == "true"),
		SandboxEnabled:  boolPtr(c.FormValue("sandbox") == "true"),
	}
	expiry := strings.TrimSpace(c.FormValue("expiryDays"))
	days := 30
	switch expiry {
	case "never":
		days = 0
	case "":
		days = 30
	default:
		if n, err := strconv.Atoi(expiry); err == nil && n >= 0 {
			days = n
		}
	}
	in.LinkExpiryDays = shareIntPtr(days)
	if vals, ok := c.Request().Form["toolIds"]; ok {
		in.ToolAllowlist = append([]string(nil), vals...)
	}
	if v := strings.TrimSpace(c.FormValue("maxSessionsPerUser")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			in.MaxActiveSessionsPerUser = shareIntPtr(n)
		}
	}
	if v := strings.TrimSpace(c.FormValue("budgetMessages")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			in.BudgetMaxMessages = shareIntPtr(n)
		}
	}
	if v := strings.TrimSpace(c.FormValue("budgetTokens")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			in.BudgetMaxTokens = shareInt64Ptr(n)
		}
	}
	if v := strings.TrimSpace(c.FormValue("welcomeMessage")); v != "" {
		in.WelcomeMessage = shareStringPtr(v)
	}
	return in
}

func shareIntPtr(n int) *int          { return &n }
func shareInt64Ptr(n int64) *int64    { return &n }
func shareStringPtr(s string) *string { return &s }
