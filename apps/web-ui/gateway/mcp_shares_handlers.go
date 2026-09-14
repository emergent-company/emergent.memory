package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- MCP sharing web UI + JSON echo handlers ---
//
// Page surface (/settings/mcp-servers/shares): list (GET), standalone create
// (GET/POST /new), edit (GET /:id/edit, POST /:id/update), rotate (POST
// /:id/rotate), and revoke (POST /:id/revoke). Create and rotate render the
// one-time key reveal directly (no redirect — the raw key exists only in that
// response and is never persisted).
//
// JSON echo surface (/api/mcp-shares/...): list / create / get / update /
// delete (revoke) / rotate / tool-catalog proxies behind the shared auth
// boundary. The raw token appears only in create and rotate responses.

// mcpShareSettingsBase is the base path for the MCP Sharing management area,
// nested under the MCP Servers settings page.
const mcpShareSettingsBase = mcpServerSettingsBase + "/shares"

// mcpSharesPageData is the payload for the list page. Reveal carries a
// just-created/rotated one-time key — it is rendered in a modal on this one
// response and is nil on every normal list load.
type mcpSharesPageData struct {
	Shares   []MCPShareInstance
	LoadErr  error
	FlashMsg string
	FlashErr error
	Reveal   *mcpShareReveal
}

// mcpShareFieldErrs carries one inline error message per form field group.
type mcpShareFieldErrs struct {
	Name  string
	Tools string
}

// mcpShareFormData is the payload shared by the create and edit pages and by
// their inline validation re-renders. Catalog + AgentsList are loaded from the
// backend; the rest is the user's draft or the instance being edited.
type mcpShareFormData struct {
	ID          string
	Name        string
	Description string
	Tools       []string
	Agents      []string
	Catalog     []MCPShareTool
	AgentsList  []AgentDefinitionSummary

	Legacy     bool
	NotFound   bool
	LoadErr    error
	FieldErrs  mcpShareFieldErrs
	GeneralErr error
	FlashErr   error
}

// mcpShareReveal is the one-time key panel rendered after create/rotate. It is
// held only for the lifetime of that one response.
type mcpShareReveal struct {
	Heading  string
	Name     string
	Token    string
	MCPURL   string
	Snippets []mcpShareSnippet
}

// --- list page ---

// uiMCPShares renders the share list (GET /settings/mcp-servers/shares).
func (s *Server) uiMCPShares(c echo.Context) error {
	ctx := c.Request().Context()
	data := mcpSharesPageData{}
	switch {
	case c.QueryParam("created") != "":
		data.FlashMsg = "Share created."
	case c.QueryParam("updated") != "":
		data.FlashMsg = "Share updated."
	case c.QueryParam("revoked") != "":
		data.FlashMsg = "Share revoked."
	}
	data.FlashErr = flashError(c)
	shares, err := s.memory.ListMCPShareInstances(ctx)
	if err != nil {
		data.LoadErr = err
	} else {
		data.Shares = mcpSharesSortedNewestFirst(shares)
	}
	return s.page(c, pageTitle("MCP Sharing"), MCPSharesPage(data))
}

// renderMCPShareListWithReveal re-renders the list after a create/rotate with
// the one-time key modal open (no redirect — the raw key exists only in this
// response). On list failure the page still renders the reveal so the key is
// never lost.
func (s *Server) renderMCPShareListWithReveal(c echo.Context, reveal *mcpShareReveal) error {
	ctx := c.Request().Context()
	data := mcpSharesPageData{Reveal: reveal}
	shares, err := s.memory.ListMCPShareInstances(ctx)
	if err != nil {
		data.LoadErr = err
	} else {
		data.Shares = mcpSharesSortedNewestFirst(shares)
	}
	return s.page(c, pageTitle("MCP Sharing"), MCPSharesPage(data))
}

// --- create page + create flow ---

// uiMCPShareNewPage renders the create form (GET .../shares/new). ?agent=
// preselects an agent in the picker (the agent view's "Share via MCP" deep
// link).
func (s *Server) uiMCPShareNewPage(c echo.Context) error {
	ctx := c.Request().Context()
	data := mcpShareFormData{FlashErr: flashError(c)}
	if err := s.loadMCPShareFormData(ctx, &data); err != nil {
		data.LoadErr = err
	}
	if id := strings.TrimSpace(c.QueryParam("agent")); id != "" {
		data.Agents = []string{id}
	}
	return s.page(c, pageTitle("New MCP share"), MCPShareNewPage(data))
}

// renderMCPShareNewPage re-renders the create page with inline errors and the
// user's draft intact (no partial save).
func (s *Server) renderMCPShareNewPage(c echo.Context, data mcpShareFormData) error {
	return s.page(c, pageTitle("New MCP share"), MCPShareNewPage(data))
}

// uiMCPShareCreate handles POST .../shares/new. Validation failures re-render
// the form inline; success renders the one-time key reveal (no redirect).
func (s *Server) uiMCPShareCreate(c echo.Context) error {
	ctx := c.Request().Context()
	draft := mcpShareFormDataFromRequest(c)
	if err := s.loadMCPShareFormData(ctx, &draft); err != nil {
		draft.LoadErr = err
		return s.renderMCPShareNewPage(c, draft)
	}
	fe := draft.validate()
	if fe.Name == "" && s.mcpShareNameTaken(ctx, draft.Name, "") {
		fe.Name = fmt.Sprintf("A share named %q already exists.", draft.Name)
	}
	if mcpShareFieldErrsNonEmpty(fe) {
		draft.FieldErrs = fe
		return s.renderMCPShareNewPage(c, draft)
	}
	created, err := s.memory.CreateMCPShareInstance(ctx, &MCPShareInput{
		Name:        draft.Name,
		Description: draft.Description,
		Tools:       draft.Tools,
		Agents:      draft.Agents,
	})
	if err != nil {
		if mcpShareIsNameConflict(err) {
			fe.Name = err.Error()
			draft.FieldErrs = fe
		} else {
			draft.GeneralErr = err
		}
		return s.renderMCPShareNewPage(c, draft)
	}
	return s.renderMCPShareListWithReveal(c, mcpShareRevealPtr(mcpShareRevealFromCreated("Share created", created)))
}

// --- edit page + update flow ---

// uiMCPShareEditPage renders the edit form pre-filled from the instance
// (GET .../shares/:id/edit). Legacy instances are not editable and redirect
// back to the list with the reason.
func (s *Server) uiMCPShareEditPage(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, mcpShareSettingsBase, errShareIDRequired())
	}
	inst, err := s.memory.GetMCPShareInstance(ctx, id)
	if err != nil {
		if isMemoryNotFound(err) {
			data := mcpShareFormData{ID: id, NotFound: true, FlashErr: flashError(c)}
			return s.page(c, pageTitle("Edit share"), MCPShareEditPage(data))
		}
		return redirectWithError(c, mcpShareSettingsBase, err)
	}
	if inst.IsLegacy {
		return redirectWithError(c, mcpShareSettingsBase, legacyShareEditErr())
	}
	data := mcpShareFormData{
		ID:          inst.ID,
		Name:        inst.Name,
		Description: inst.Description,
		Tools:       inst.Tools,
		Agents:      inst.Agents,
		FlashErr:    flashError(c),
	}
	if err := s.loadMCPShareFormData(ctx, &data); err != nil {
		data.LoadErr = err
	}
	return s.page(c, pageTitle("Edit share"), MCPShareEditPage(data))
}

// renderMCPShareEditPage re-renders the edit page after a validation failure.
func (s *Server) renderMCPShareEditPage(c echo.Context, data mcpShareFormData) error {
	return s.page(c, pageTitle("Edit share"), MCPShareEditPage(data))
}

// uiMCPShareUpdate handles POST .../shares/:id/update. The key is unchanged.
func (s *Server) uiMCPShareUpdate(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, mcpShareSettingsBase, errShareIDRequired())
	}
	current, err := s.memory.GetMCPShareInstance(ctx, id)
	if err != nil || current == nil {
		return redirectWithError(c, mcpShareSettingsBase, err)
	}
	if current.IsLegacy {
		return redirectWithError(c, mcpShareSettingsBase, legacyShareEditErr())
	}
	draft := mcpShareFormDataFromRequest(c)
	draft.ID = id
	if err := s.loadMCPShareFormData(ctx, &draft); err != nil {
		draft.LoadErr = err
		return s.renderMCPShareEditPage(c, draft)
	}
	fe := draft.validate()
	if fe.Name == "" && s.mcpShareNameTaken(ctx, draft.Name, id) {
		fe.Name = fmt.Sprintf("A share named %q already exists.", draft.Name)
	}
	if mcpShareFieldErrsNonEmpty(fe) {
		draft.FieldErrs = fe
		return s.renderMCPShareEditPage(c, draft)
	}
	if _, err := s.memory.UpdateMCPShareInstance(ctx, id, &MCPShareInput{
		Name:        draft.Name,
		Description: draft.Description,
		Tools:       draft.Tools,
		Agents:      draft.Agents,
	}); err != nil {
		if mcpShareIsNameConflict(err) {
			fe.Name = err.Error()
			draft.FieldErrs = fe
		} else {
			draft.GeneralErr = err
		}
		return s.renderMCPShareEditPage(c, draft)
	}
	return c.Redirect(http.StatusSeeOther, mcpShareSettingsBase+"?updated=1")
}

// --- revoke flow ---

// uiMCPShareRevoke handles POST .../shares/:id/revoke (PRG). Legacy instances
// cannot be revoked through this UI.
func (s *Server) uiMCPShareRevoke(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, mcpShareSettingsBase, errShareIDRequired())
	}
	if inst, err := s.memory.GetMCPShareInstance(ctx, id); err == nil && inst != nil && inst.IsLegacy {
		return redirectWithError(c, mcpShareSettingsBase, legacyShareRevokeErr())
	}
	if err := s.memory.RevokeMCPShareInstance(ctx, id); err != nil {
		return redirectWithError(c, mcpShareSettingsBase, err)
	}
	return c.Redirect(http.StatusSeeOther, mcpShareSettingsBase+"?revoked=1")
}

// --- rotate flow ---

// uiMCPShareRotate handles POST .../shares/:id/rotate. Success renders the
// new key once (no redirect); the previous key is invalid.
func (s *Server) uiMCPShareRotate(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, mcpShareSettingsBase, errShareIDRequired())
	}
	if inst, err := s.memory.GetMCPShareInstance(ctx, id); err == nil && inst != nil && inst.IsLegacy {
		return redirectWithError(c, mcpShareSettingsBase, legacyShareRotateErr())
	}
	created, err := s.memory.RotateMCPShareInstance(ctx, id)
	if err != nil {
		return redirectWithError(c, mcpShareSettingsBase, err)
	}
	return s.renderMCPShareListWithReveal(c, mcpShareRevealPtr(mcpShareRevealFromCreated("Share key rotated", created)))
}

// --- JSON echo surface ---

// listMCPShares handles GET /api/mcp-shares.
func (s *Server) listMCPShares(c echo.Context) error {
	shares, err := s.memory.ListMCPShareInstances(c.Request().Context())
	if err != nil {
		return mcpShareJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, mcpSharesSortedNewestFirst(shares))
}

// createMCPShare handles POST /api/mcp-shares and returns the one-time token.
func (s *Server) createMCPShare(c echo.Context) error {
	var in MCPShareInput
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
	}
	created, err := s.memory.CreateMCPShareInstance(c.Request().Context(), &in)
	if err != nil {
		return mcpShareJSONErr(c, err)
	}
	return c.JSON(http.StatusCreated, mcpShareCreatedJSON(created))
}

// getMCPShare handles GET /api/mcp-shares/:id (never carries the token).
func (s *Server) getMCPShare(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errShareIDRequired().Error()})
	}
	inst, err := s.memory.GetMCPShareInstance(c.Request().Context(), id)
	if err != nil {
		return mcpShareJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, inst)
}

// updateMCPShare handles PATCH /api/mcp-shares/:id.
func (s *Server) updateMCPShare(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errShareIDRequired().Error()})
	}
	var in MCPShareInput
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
	}
	inst, err := s.memory.UpdateMCPShareInstance(c.Request().Context(), id, &in)
	if err != nil {
		return mcpShareJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, inst)
}

// deleteMCPShare handles DELETE /api/mcp-shares/:id (revoke).
func (s *Server) deleteMCPShare(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errShareIDRequired().Error()})
	}
	if err := s.memory.RevokeMCPShareInstance(c.Request().Context(), id); err != nil {
		return mcpShareJSONErr(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// rotateMCPShare handles POST /api/mcp-shares/:id/rotate and returns the new
// one-time token.
func (s *Server) rotateMCPShare(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errShareIDRequired().Error()})
	}
	created, err := s.memory.RotateMCPShareInstance(c.Request().Context(), id)
	if err != nil {
		return mcpShareJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, mcpShareCreatedJSON(created))
}

// listMCPShareTools handles GET /api/mcp-shares/tools (the tool catalog).
func (s *Server) listMCPShareTools(c echo.Context) error {
	tools, err := s.memory.ListMCPShareTools(c.Request().Context())
	if err != nil {
		return mcpShareJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, tools)
}

// --- shared helpers ---

// loadMCPShareFormData fills the tool catalog and agent list on a form
// payload, preserving any draft/instance fields already set. A catalog failure
// is returned so the page renders its error state; an agent-list failure
// degrades to an empty picker (the share can still be created).
func (s *Server) loadMCPShareFormData(ctx context.Context, data *mcpShareFormData) error {
	catalog, err := s.memory.ListMCPShareTools(ctx)
	if err != nil {
		return err
	}
	data.Catalog = catalog
	agents, aerr := s.memory.ListAgentDefinitions(ctx)
	if aerr != nil {
		captureError(aerr)
		return nil
	}
	data.AgentsList = agents
	return nil
}

// mcpShareNameTaken reports whether another share already holds name.
func (s *Server) mcpShareNameTaken(ctx context.Context, name, exceptID string) bool {
	if name == "" {
		return false
	}
	shares, err := s.memory.ListMCPShareInstances(ctx)
	if err != nil {
		return false
	}
	for _, sh := range shares {
		if sh.ID != exceptID && strings.EqualFold(sh.Name, name) {
			return true
		}
	}
	return false
}

// mcpShareCreatedJSON returns the create/rotate response with the one-time
// token and a complete snippet set.
func mcpShareCreatedJSON(created *MCPShareCreated) map[string]any {
	if created == nil {
		return map[string]any{}
	}
	return map[string]any{
		"instance": created.MCPShareInstance,
		"token":    created.Token,
		"mcpUrl":   created.MCPURL,
		"snippets": mcpShareSnippetsMap(created.MCPShareSecret),
	}
}

// mcpShareRevealFromCreated builds the reveal payload from a create/rotate
// result.
func mcpShareRevealFromCreated(heading string, created *MCPShareCreated) mcpShareReveal {
	if created == nil {
		return mcpShareReveal{Heading: heading}
	}
	return mcpShareReveal{
		Heading:  heading,
		Name:     created.Name,
		Token:    created.Token,
		MCPURL:   created.MCPURL,
		Snippets: mcpShareSnippets(created.MCPShareSecret),
	}
}

// mcpShareRevealPtr returns a pointer to a reveal payload (for the list page's
// optional modal).
func mcpShareRevealPtr(r mcpShareReveal) *mcpShareReveal { return &r }

// mcpShareFieldErrsNonEmpty reports whether any inline field error is set.
func mcpShareFieldErrsNonEmpty(fe mcpShareFieldErrs) bool {
	return fe.Name != "" || fe.Tools != ""
}

// mcpShareFormDataFromRequest parses the create/edit draft from the submitted
// form (name, description, tools[], agents[]).
func mcpShareFormDataFromRequest(c echo.Context) mcpShareFormData {
	values, _ := c.FormParams()
	tools := mcpDedupeStrings(values["tools"])
	agents := mcpDedupeStrings(values["agents"])
	return mcpShareFormData{
		Name:        strings.TrimSpace(c.FormValue("name")),
		Description: strings.TrimSpace(c.FormValue("description")),
		Tools:       tools,
		Agents:      agents,
	}
}

// validate enforces the create/edit rules: a non-empty name and at least one
// tool.
func (d mcpShareFormData) validate() mcpShareFieldErrs {
	var fe mcpShareFieldErrs
	if d.Name == "" {
		fe.Name = "Share name is required."
	}
	if len(d.Tools) == 0 {
		fe.Tools = "Select at least one tool to expose."
	}
	return fe
}

// mcpDedupeStrings drops blank and repeated values, preserving order.
func mcpDedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// mcpShareIsNameConflict reports whether a backend error is a duplicate-name
// rejection.
func mcpShareIsNameConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "conflict") ||
		strings.Contains(msg, "duplicate")
}

// mcpShareJSONErr writes a {"error": msg} JSON response, preserving the
// backend's HTTP status when the memory error carries one (409/422 pass
// through) and falling back to 502 otherwise.
func mcpShareJSONErr(c echo.Context, err error) error {
	if err == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "unknown error"})
	}
	return c.JSON(mcpShareUpstreamStatus(err), map[string]string{"error": err.Error()})
}

// mcpShareUpstreamStatus extracts the status from a "memory <code> ..." error
// string, else returns 502.
func mcpShareUpstreamStatus(err error) int {
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

func errShareIDRequired() error { return errors.New("share id is required") }

func legacyShareEditErr() error {
	return errors.New("legacy shares cannot be edited — recreate the share to change its tools or agents")
}

func legacyShareRevokeErr() error {
	return errors.New("legacy shares cannot be revoked from this UI")
}

func legacyShareRotateErr() error {
	return errors.New("legacy shares do not support key rotation")
}

// --- snippet map helper ---

// mcpShareSnippetsMap returns the four canonical client snippets keyed by
// client, preferring backend-provided text and generating the rest.
func mcpShareSnippetsMap(secret MCPShareSecret) map[string]string {
	out := map[string]string{}
	for _, s := range mcpShareSnippets(secret) {
		out[s.Key] = s.Code
	}
	return out
}
