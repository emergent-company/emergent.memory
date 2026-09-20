package main

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// shareLinksPageData is the assembled payload for AgentSharePage (the list).
type shareLinksPageData struct {
	Agent    *AgentDefinition
	LoadErr  error
	FlashMsg string
	Panel    ShareLinksPageProps
	// Reveal is non-nil only on the response that created or rotated a link: it
	// carries the one-time full URL (with key) for that single response.
	Reveal *ShareLinkReveal
}

// shareLinkCreatePageData is the assembled payload for AgentShareCreatePage.
type shareLinkCreatePageData struct {
	Agent   *AgentDefinition
	LoadErr error
	Form    ShareLinkCreatePageProps
}

// --- page render ---

// uiAgentShare renders the owner-facing share-links list page
// (GET /agents/:id/share).
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
	data.Panel = s.shareListPanelProps(ctx, agent, nil)
	return s.page(c, pageTitle(agent.Name, "Share links"), AgentSharePage(data))
}

// uiAgentShareNewPage renders the standalone create page
// (GET /agents/:id/share/new).
func (s *Server) uiAgentShareNewPage(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()
	agent, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("New share link"), AgentShareCreatePage(shareLinkCreatePageData{LoadErr: err}))
	}
	data := shareLinkCreatePageData{Agent: agent}
	data.Form = s.shareCreatePageProps(agent, ShareLinkCreateValues{}, "")
	return s.page(c, pageTitle(agent.Name, "New share link"), AgentShareCreatePage(data))
}

// renderAgentShareList re-renders the list page directly (no redirect) after a
// create/rotate so the one-time URL with the key is shown in this response. The
// raw key exists only here and must never round-trip through a URL.
func (s *Server) renderAgentShareList(c echo.Context, agentID string, reveal *ShareLinkReveal, flashMsg string) error {
	ctx := c.Request().Context()
	agent, err := s.memory.GetAgentDefinition(ctx, agentID)
	if err != nil {
		return s.page(c, pageTitle("Share links"), AgentSharePage(shareLinksPageData{LoadErr: err}))
	}
	data := shareLinksPageData{Agent: agent, FlashMsg: flashMsg, Reveal: reveal}
	data.Panel = s.shareListPanelProps(ctx, agent, reveal)
	return s.page(c, pageTitle(agent.Name, "Share links"), AgentSharePage(data))
}

// renderAgentShareCreate re-renders the create page after a failed submit,
// preserving the owner's form input.
func (s *Server) renderAgentShareCreate(c echo.Context, agentID string, values ShareLinkCreateValues, formErr string) error {
	ctx := c.Request().Context()
	agent, err := s.memory.GetAgentDefinition(ctx, agentID)
	if err != nil {
		return s.page(c, pageTitle("New share link"), AgentShareCreatePage(shareLinkCreatePageData{LoadErr: err}))
	}
	data := shareLinkCreatePageData{Agent: agent}
	data.Form = s.shareCreatePageProps(agent, values, formErr)
	return s.page(c, pageTitle(agent.Name, "New share link"), AgentShareCreatePage(data))
}

// --- form handlers ---

// uiAgentShareCreate handles POST /agents/:id/share/new. Validation failures
// re-render the create page inline; success renders the list with the one-time
// URL revealed (no redirect).
func (s *Server) uiAgentShareCreate(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()
	values := shareLinkCreateValuesFromForm(c)
	label := strings.TrimSpace(c.FormValue("label"))
	if label == "" {
		return s.renderAgentShareCreate(c, id, values, "Label is required.")
	}
	created, err := s.memory.CreateShareLink(ctx, id, ShareLinkCreateInput{Label: label, Config: shareLinkConfigInputFromForm(c)})
	if err != nil {
		return s.renderAgentShareCreate(c, id, values, "Could not create link: "+err.Error())
	}
	reveal := s.shareLinkReveal("Share link created", created, created.Token)
	return s.renderAgentShareList(c, id, reveal, "Share link created — copy the URL below.")
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
	reveal := s.shareLinkReveal("Share link rotated", rotated, rotated.Token)
	return s.renderAgentShareList(c, agentID, reveal, "Share link rotated — copy the new URL below.")
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

// shareListPanelProps builds the list page body for an agent's share links.
// reveal, when non-nil, is the link created/rotated in this same response, so
// its row can carry the one-time URL.
func (s *Server) shareListPanelProps(ctx context.Context, agent *AgentDefinition, reveal *ShareLinkReveal) ShareLinksPageProps {
	props := ShareLinksPageProps{
		NewLinkURL: "/agents/" + url.PathEscape(agent.ID) + "/share/new",
	}
	links, err := s.memory.ListShareLinks(ctx, agent.ID)
	if err != nil {
		props.Error = "Failed to load share links: " + err.Error()
		return props
	}
	slices.SortStableFunc(links, func(a, b ShareLink) int { return b.CreatedAt.Compare(a.CreatedAt) })
	for _, l := range links {
		publicURL := ""
		if reveal != nil && reveal.LinkID == l.ID {
			publicURL = reveal.URL
		}
		props.Links = append(props.Links, s.shareLinkRow(l, publicURL))
	}
	return props
}

// shareCreatePageProps builds the create page body, including the agent's tool
// options and the preserved form values.
func (s *Server) shareCreatePageProps(agent *AgentDefinition, values ShareLinkCreateValues, formErr string) ShareLinkCreatePageProps {
	props := ShareLinkCreatePageProps{
		CreateAction: "/agents/" + url.PathEscape(agent.ID) + "/share/new",
		CancelURL:    "/agents/" + url.PathEscape(agent.ID) + "/share",
		Values:       values,
		Error:        formErr,
	}
	for _, t := range agent.Tools {
		props.Tools = append(props.Tools, ShareToolOption{ID: t, Label: t})
	}
	return props
}

// shareLinkReveal builds the one-time reveal for a just-created/rotated link.
// It returns nil when no token was issued, so the list page carries no reveal.
func (s *Server) shareLinkReveal(heading string, l *ShareLink, token string) *ShareLinkReveal {
	if l == nil || token == "" {
		return nil
	}
	return &ShareLinkReveal{
		LinkID:  l.ID,
		Heading: heading,
		Label:   l.Label,
		URL:     s.sharePublicURL(token),
	}
}

// shareLinkRow maps a server ShareLink onto the designer's ShareLinkRow.
// publicURL is the one-time full URL, empty for every link whose key is not
// present in this response.
func (s *Server) shareLinkRow(l ShareLink, publicURL string) ShareLinkRow {
	linkURL := "/agents/" + url.PathEscape(l.AgentDefinitionID)
	row := ShareLinkRow{
		ID:              l.ID,
		Label:           l.Label,
		URL:             publicURL,
		Status:          shareLinkStatus(l),
		RevealURL:       linkURL + "/share-links/" + url.PathEscape(l.ID) + "/reveal",
		RotateURL:       linkURL + "/share-links/" + url.PathEscape(l.ID) + "/rotate",
		RevokeURL:       linkURL + "/share-links/" + url.PathEscape(l.ID) + "/revoke",
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
		ToolIDs:            slices.Clone(c.Request().Form["toolIds"]),
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
	requireEmail := c.FormValue("requireEmail") == "true"
	showSessionList := c.FormValue("showSessionList") == "true"
	sandbox := c.FormValue("sandbox") == "true"
	in := &ShareLinkConfigInput{
		RequireEmail:    &requireEmail,
		ShowSessionList: &showSessionList,
		SandboxEnabled:  &sandbox,
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
	in.LinkExpiryDays = &days
	if vals, ok := c.Request().Form["toolIds"]; ok {
		in.ToolAllowlist = slices.Clone(vals)
	}
	if v := strings.TrimSpace(c.FormValue("maxSessionsPerUser")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			in.MaxActiveSessionsPerUser = &n
		}
	}
	if v := strings.TrimSpace(c.FormValue("budgetMessages")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			in.BudgetMaxMessages = &n
		}
	}
	if v := strings.TrimSpace(c.FormValue("budgetTokens")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			in.BudgetMaxTokens = &n
		}
	}
	if v := strings.TrimSpace(c.FormValue("welcomeMessage")); v != "" {
		in.WelcomeMessage = &v
	}
	return in
}
