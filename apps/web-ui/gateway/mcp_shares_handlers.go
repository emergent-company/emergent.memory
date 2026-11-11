package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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
	Name   string
	Tools  string
	Emails string
}

// mcpShareFormData is the payload shared by the create and edit pages and by
// their inline validation re-renders. Catalog is loaded from the backend; the
// rest is the user's draft or the instance being edited. Emails is the raw
// invite list typed into the create/rotate form (kept verbatim on a failed
// submit so the user does not retype it).
type mcpShareFormData struct {
	ID          string
	Name        string
	Description string
	Tools       []string
	Emails      string
	Catalog     []MCPShareTool
	Templates   []MCPShareTemplate

	// CopyFrom is the name of the share this draft was seeded from (the
	// "Duplicate share" flow), empty on a blank create form. It drives the page
	// copy only: the submission is an ordinary create that mints a fresh key, so
	// nothing about the source share changes.
	CopyFrom string

	Legacy     bool
	NotFound   bool
	LoadErr    error
	FieldErrs  mcpShareFieldErrs
	GeneralErr error
	FlashErr   error
}

// mcpShareReveal is the one-time key panel rendered after create/rotate. It is
// held only for the lifetime of that one response. EmailsSent lists the invite
// recipients the key was queued to; EmailWarn reports a delivery problem.
type mcpShareReveal struct {
	Heading   string
	Name      string
	Token     string
	MCPURL    string
	BundleURL string
	// InstallURL is the private claude:// install redirect for this key.
	InstallURL string
	EmailsSent []string
	EmailWarn  string
	Snippets   []mcpShareSnippet
}

// --- list page ---

// uiMCPShares renders the share list (GET /settings/mcp-servers/shares).
func (s *Server) uiMCPShares(c echo.Context) error {
	ctx := c.Request().Context()
	data := mcpSharesPageData{
		FlashMsg: flashFromQuery(c, []flashParam{
			{key: "created", msg: "Share created."},
			{key: "updated", msg: "Share updated."},
			{key: "revoked", msg: "Share revoked."},
		}),
	}
	data.FlashErr = flashError(c)
	// The invite flow carries its recipient count, so its flash is built here
	// rather than from the static flashFromQuery table.
	if n, convErr := strconv.Atoi(c.QueryParam("invited")); convErr == nil && n > 0 {
		data.FlashMsg = fmt.Sprintf("Invite emailed to %s.", countLabel(n, "recipient", "recipients"))
	}
	shares, err := s.memory.ListMCPShareInstances(ctx)
	if err != nil {
		data.LoadErr = userFacingError(err)
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
		data.LoadErr = userFacingError(err)
	} else {
		data.Shares = mcpSharesSortedNewestFirst(shares)
	}
	return s.page(c, pageTitle("MCP Sharing"), MCPSharesPage(data))
}

// --- create page + create flow ---

// uiMCPShareNewPage renders the create form (GET .../shares/new). Only this
// path applies the default preset: a submitted draft must reach validation
// exactly as the user left it.
func (s *Server) uiMCPShareNewPage(c echo.Context) error {
	ctx := c.Request().Context()
	data := mcpShareFormData{FlashErr: flashError(c)}
	if err := s.loadMCPShareFormData(ctx, &data); err != nil {
		data.LoadErr = userFacingError(err)
	} else {
		data.Tools = mcpShareApplyDefaultTemplate(&MCPShareCatalog{Tools: data.Catalog, Templates: data.Templates}, data.Tools)
	}
	return s.page(c, pageTitle(mcpShareNewPageHeading(data)), MCPShareNewPage(data))
}

// renderMCPShareNewPage re-renders the create page with inline errors and the
// user's draft intact (no partial save).
func (s *Server) renderMCPShareNewPage(c echo.Context, data mcpShareFormData) error {
	return s.page(c, pageTitle(mcpShareNewPageHeading(data)), MCPShareNewPage(data))
}

// --- duplicate flow ---

// mcpShareNameMaxLen mirrors the backend's name limit: `normalizeInstanceName`
// rejects a trimmed name whose `len` exceeds 255, so the limit is in bytes and a
// derived copy name is clamped the same way.
const mcpShareNameMaxLen = 255

// uiMCPShareDuplicatePage renders the create form seeded from an existing share
// (GET .../shares/:id/duplicate), so a new share can start from a known tool
// allowlist instead of a blank form. The submission is an ordinary create — it
// posts to .../shares/new and mints a brand-new key — and the source share is
// never modified, which is why legacy instances are copyable even though this UI
// cannot edit them. Invite recipients are deliberately NOT carried over: a copy
// means a new key, and nobody should be emailed one implicitly.
func (s *Server) uiMCPShareDuplicatePage(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, mcpShareSettingsBase, errShareIDRequired())
	}
	src, err := s.memory.GetMCPShareInstance(ctx, id)
	if err != nil {
		if isMemoryNotFound(err) {
			data := mcpShareFormData{NotFound: true, FlashErr: flashError(c)}
			return s.page(c, pageTitle(mcpShareNewPageHeading(data)), MCPShareNewPage(data))
		}
		return redirectWithError(c, mcpShareSettingsBase, err)
	}
	data := mcpShareFormData{
		Name:        s.mcpShareDuplicateName(ctx, src.Name),
		Description: src.Description,
		Tools:       append([]string(nil), src.Tools...),
		CopyFrom:    src.Name,
		FlashErr:    flashError(c),
	}
	if err := s.loadMCPShareFormData(ctx, &data); err != nil {
		data.LoadErr = userFacingError(err)
	}
	return s.page(c, pageTitle(mcpShareNewPageHeading(data)), MCPShareNewPage(data))
}

// mcpShareDuplicateName derives a free name for a copy: "<name> (copy)", then
// "<name> (copy 2)", "<name> (copy 3)", … Uniqueness is case-insensitive per
// project (the backend enforces it), so candidates are compared the same way.
// The stem is shortened when needed so the suffix never pushes the name past
// mcpShareNameMaxLen. The candidate space is unbounded and the taken set is
// finite, so the loop always terminates.
func (s *Server) mcpShareDuplicateName(ctx context.Context, base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "share"
	}
	taken := map[string]bool{}
	if shares, err := s.memory.ListMCPShareInstances(ctx); err == nil {
		for _, sh := range shares {
			taken[strings.ToLower(strings.TrimSpace(sh.Name))] = true
		}
	}
	for i := 1; ; i++ {
		suffix := " (copy)"
		if i > 1 {
			suffix = fmt.Sprintf(" (copy %d)", i)
		}
		name := mcpShareTruncateName(base, mcpShareNameMaxLen-len(suffix)) + suffix
		if !taken[strings.ToLower(name)] {
			return name
		}
	}
}

// mcpShareTruncateName cuts s to at most maxBytes UTF-8 bytes without splitting
// a rune, so a copy name derived from a non-ASCII source still fits the
// backend's byte-measured limit.
func mcpShareTruncateName(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	var b strings.Builder
	b.Grow(maxBytes)
	used := 0
	for _, r := range s {
		n := utf8.RuneLen(r)
		if used+n > maxBytes {
			break
		}
		b.WriteRune(r)
		used += n
	}
	return b.String()
}

// mcpShareNewPageHeading titles the create page. A duplicate names itself as
// such so the user never mistakes the seeded draft for a blank form; the
// not-found state belongs to the duplicate route (a blank create cannot 404).
func mcpShareNewPageHeading(data mcpShareFormData) string {
	if data.CopyFrom != "" || data.NotFound {
		return "Duplicate MCP share"
	}
	return "New MCP share"
}

// mcpShareNewPageSubtitle explains where a duplicated draft came from, that the
// copy gets its own key, and that the source keeps working.
func mcpShareNewPageSubtitle(data mcpShareFormData) string {
	if data.CopyFrom != "" {
		return "Starts from “" + data.CopyFrom + "”. Its tools and description are carried over — adjust them and save to mint a key for the new share; the original is untouched."
	}
	return "Name the share and choose what an outside MCP client can reach."
}

// uiMCPShareCreate handles POST .../shares/new. Validation failures re-render
// the form inline; success renders the one-time key reveal (no redirect).
func (s *Server) uiMCPShareCreate(c echo.Context) error {
	ctx := c.Request().Context()
	draft := mcpShareFormDataFromRequest(c)
	if err := s.loadMCPShareFormData(ctx, &draft); err != nil {
		draft.LoadErr = userFacingError(err)
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
	emails := mcpShareParseEmails(draft.Emails)
	created, err := s.memory.CreateMCPShareInstance(ctx, &MCPShareInput{
		Name:        draft.Name,
		Description: draft.Description,
		Tools:       draft.Tools,
		Emails:      emails,
	})
	if err != nil {
		if mcpShareIsNameConflict(err) {
			fe.Name = err.Error()
			draft.FieldErrs = fe
		} else if mcpShareIsInvalidEmail(err) {
			fe.Emails = mcpShareInvalidEmailMessage(err)
			draft.FieldErrs = fe
		} else {
			draft.GeneralErr = userFacingError(err)
		}
		return s.renderMCPShareNewPage(c, draft)
	}
	return s.renderMCPShareListWithReveal(c, mcpShareRevealPtr(s.mcpShareRevealFromCreated("Share created", created)))
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
		FlashErr:    flashError(c),
	}
	if err := s.loadMCPShareFormData(ctx, &data); err != nil {
		data.LoadErr = userFacingError(err)
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
	// The invite list only exists where a key is minted; the edit form never
	// renders it, so a stray value must not fail validation on an edit.
	draft.Emails = ""
	if err := s.loadMCPShareFormData(ctx, &draft); err != nil {
		draft.LoadErr = userFacingError(err)
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
	}); err != nil {
		if mcpShareIsNameConflict(err) {
			fe.Name = err.Error()
			draft.FieldErrs = fe
		} else {
			draft.GeneralErr = userFacingError(err)
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
	rawEmails := strings.TrimSpace(c.FormValue("emails"))
	if invalid := mcpShareInvalidEmails(rawEmails); len(invalid) > 0 {
		return redirectWithError(c, mcpShareSettingsBase, fmt.Errorf("not a valid email address: %s", invalid[0]))
	}
	created, err := s.memory.RotateMCPShareInstance(ctx, id, mcpShareParseEmails(rawEmails))
	if err != nil {
		return redirectWithError(c, mcpShareSettingsBase, err)
	}
	return s.renderMCPShareListWithReveal(c, mcpShareRevealPtr(s.mcpShareRevealFromCreated("Share key rotated", created)))
}

// --- invite flow (ad-hoc, no rotation) ---

// uiMCPShareInvite handles POST .../shares/:id/invite (PRG). It emails the
// share's EXISTING key and setup instructions to the listed recipients: the key
// is not rotated, so everyone already using the share keeps working. When the
// deployment cannot recover the key, the flash points at "Issue a new key
// instead" rather than leaving the user at a dead end.
func (s *Server) uiMCPShareInvite(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, mcpShareSettingsBase, errShareIDRequired())
	}
	if inst, err := s.memory.GetMCPShareInstance(ctx, id); err == nil && inst != nil && inst.IsLegacy {
		return redirectWithError(c, mcpShareSettingsBase, legacyShareInviteErr())
	}
	rawEmails := strings.TrimSpace(c.FormValue("emails"))
	if invalid := mcpShareInvalidEmails(rawEmails); len(invalid) > 0 {
		return redirectWithError(c, mcpShareSettingsBase, fmt.Errorf("not a valid email address: %s", invalid[0]))
	}
	emails := mcpShareParseEmails(rawEmails)
	if len(emails) == 0 {
		return redirectWithError(c, mcpShareSettingsBase, errShareInviteNeedsRecipients())
	}
	res, err := s.memory.InviteMCPShareInstance(ctx, id, emails)
	if err != nil {
		if mcpShareIsNotRecoverable(err) {
			return redirectWithError(c, mcpShareSettingsBase, errShareKeyNotRecoverable())
		}
		return redirectWithError(c, mcpShareSettingsBase, err)
	}
	if res.EmailError != "" {
		// The key is still the share's key and it did not change; only the
		// queue failed. Say both so the user knows nothing was broken.
		return redirectWithError(c, mcpShareSettingsBase,
			fmt.Errorf("the share's key was not changed, but the invite could not be queued: %s", res.EmailError))
	}
	return c.Redirect(http.StatusSeeOther, fmt.Sprintf("%s?invited=%d", mcpShareSettingsBase, len(res.EmailsSent)))
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
	return c.JSON(http.StatusCreated, s.mcpShareCreatedJSON(created))
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
// one-time token. An optional {"emails":[...]} body emails the new key.
func (s *Server) rotateMCPShare(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errShareIDRequired().Error()})
	}
	var in struct {
		Emails []string `json:"emails,omitempty"`
	}
	if c.Request().ContentLength > 0 {
		if err := c.Bind(&in); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
		}
	}
	created, err := s.memory.RotateMCPShareInstance(c.Request().Context(), id, in.Emails)
	if err != nil {
		return mcpShareJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, s.mcpShareCreatedJSON(created))
}

// inviteMCPShare handles POST /api/mcp-shares/:id/invite: it emails the share's
// existing key to the listed recipients and reports the queued addresses.
func (s *Server) inviteMCPShare(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errShareIDRequired().Error()})
	}
	var in struct {
		Emails []string `json:"emails,omitempty"`
	}
	if err := c.Bind(&in); err != nil && !errors.Is(err, io.EOF) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
	}
	if len(in.Emails) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errShareInviteNeedsRecipients().Error()})
	}
	res, err := s.memory.InviteMCPShareInstance(c.Request().Context(), id, in.Emails)
	if err != nil {
		return mcpShareJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, res)
}

// listMCPShareTools handles GET /api/mcp-shares/tools (the tool catalog). The
// presets are served alongside it as `templates`; this endpoint keeps returning
// the catalog object so both arrive in one call.
func (s *Server) listMCPShareTools(c echo.Context) error {
	catalog, err := s.memory.ListMCPShareCatalog(c.Request().Context())
	if err != nil {
		return mcpShareJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, catalog)
}

// --- shared helpers ---

// loadMCPShareFormData fills the tool catalog and presets on a form payload,
// preserving any draft/instance fields already set. A catalog failure is
// returned so the page renders its error state. It never touches Tools: the
// create-page default is applied by the GET handler, so a submitted (possibly
// emptied) selection reaches validation unchanged.
func (s *Server) loadMCPShareFormData(ctx context.Context, data *mcpShareFormData) error {
	catalog, err := s.memory.ListMCPShareCatalog(ctx)
	if err != nil {
		return err
	}
	if catalog == nil {
		return nil
	}
	data.Catalog = catalog.Tools
	data.Templates = catalog.Templates
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
// token and a complete snippet set, with the endpoint URL rewritten to the
// memory server's externally-reachable origin.
func (s *Server) mcpShareCreatedJSON(created *MCPShareCreated) map[string]any {
	if created == nil {
		return map[string]any{}
	}
	created.MCPURL = s.externalMCPURL(created.MCPURL)
	out := map[string]any{
		"instance": created.MCPShareInstance,
		"token":    created.Token,
		"mcpUrl":   created.MCPURL,
		"snippets": mcpShareSnippetsMap(created.MCPShareSecret),
	}
	if len(created.EmailsSent) > 0 {
		out["emailsSent"] = created.EmailsSent
	}
	if created.EmailError != "" {
		out["emailError"] = created.EmailError
	}
	return out
}

// mcpShareRevealFromCreated builds the reveal payload from a create/rotate
// result, rewriting the endpoint URL to the memory server's externally-reachable
// origin and exposing the one-click Claude Desktop bundle download link.
func (s *Server) mcpShareRevealFromCreated(heading string, created *MCPShareCreated) mcpShareReveal {
	if created == nil {
		return mcpShareReveal{Heading: heading}
	}
	created.MCPURL = s.externalMCPURL(created.MCPURL)
	return mcpShareReveal{
		Heading:    heading,
		Name:       created.Name,
		Token:      created.Token,
		MCPURL:     created.MCPURL,
		BundleURL:  s.serverPublicBaseURL() + "/api/mcp/bundle?token=" + url.QueryEscape(created.Token),
		InstallURL: mcpInstallURL(s.serverPublicBaseURL(), created.MCPURL, created.Token),
		EmailsSent: created.EmailsSent,
		EmailWarn:  created.EmailError,
		Snippets:   mcpShareSnippets(created.MCPShareSecret),
	}
}

// mcpShareRevealPtr returns a pointer to a reveal payload (for the list page's
// optional modal).
func mcpShareRevealPtr(r mcpShareReveal) *mcpShareReveal { return &r }

// mcpShareDeliveryNote renders the reveal's invite confirmation for the
// recipients the key was queued to. Long lists are truncated so the alert stays
// readable.
func mcpShareDeliveryNote(emails []string) string {
	switch len(emails) {
	case 0:
		return ""
	case 1:
		return "Key emailed to " + emails[0] + "."
	default:
		const inlineMax = 3
		if len(emails) <= inlineMax {
			return fmt.Sprintf("Key emailed to %d recipients: %s.", len(emails), strings.Join(emails, ", "))
		}
		return fmt.Sprintf("Key emailed to %d recipients: %s and %d more.",
			len(emails), strings.Join(emails[:inlineMax], ", "), len(emails)-inlineMax)
	}
}

// mcpShareDeliveryWarn renders the reveal's delivery problem, if any, with the
// actionable framing that the share itself is fine.
func mcpShareDeliveryWarn(warn string) string {
	if warn == "" {
		return ""
	}
	return "The share exists and the key above works, but the invite email did not go out: " + warn
}

// mcpShareFieldErrsNonEmpty reports whether any inline field error is set.
func mcpShareFieldErrsNonEmpty(fe mcpShareFieldErrs) bool {
	return fe.Name != "" || fe.Tools != "" || fe.Emails != ""
}

// mcpShareFormDataFromRequest parses the create/edit draft from the submitted
// form (name, description, tools[], and the optional invite list).
func mcpShareFormDataFromRequest(c echo.Context) mcpShareFormData {
	values, _ := c.FormParams()
	tools := mcpDedupeStrings(values["tools"])
	return mcpShareFormData{
		Name:        strings.TrimSpace(c.FormValue("name")),
		Description: strings.TrimSpace(c.FormValue("description")),
		Tools:       tools,
		Emails:      strings.TrimSpace(c.FormValue("emails")),
		// copyFrom rides along as a hidden field so an inline validation
		// re-render keeps the "duplicated from X" framing; it carries no
		// behaviour of its own.
		CopyFrom: strings.TrimSpace(c.FormValue("copyFrom")),
	}
}

// validate enforces the create/edit rules: a non-empty name, at least one tool,
// and only valid invite addresses (when any are given).
func (d mcpShareFormData) validate() mcpShareFieldErrs {
	var fe mcpShareFieldErrs
	if d.Name == "" {
		fe.Name = "Share name is required."
	}
	if len(d.Tools) == 0 {
		fe.Tools = "Select at least one tool to expose."
	}
	if invalid := mcpShareInvalidEmails(d.Emails); len(invalid) > 0 {
		fe.Emails = "Not a valid email address: " + invalid[0]
	}
	return fe
}

// mcpShareEmailPattern mirrors the server's invite-address rule so a typo is
// caught inline instead of after a round trip. The server remains authoritative.
var mcpShareEmailPattern = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)

// mcpShareParseEmails splits a raw invite list on commas, semicolons and
// whitespace, trims each address, drops blanks, and removes case-insensitive
// duplicates.
func mcpShareParseEmails(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || unicode.IsSpace(r)
	})
	out := make([]string, 0, len(fields))
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		addr := strings.TrimSpace(f)
		if addr == "" {
			continue
		}
		key := strings.ToLower(addr)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, addr)
	}
	return out
}

// mcpShareInvalidEmails lists the invite addresses that are not valid targets.
func mcpShareInvalidEmails(raw string) []string {
	var invalid []string
	for _, addr := range mcpShareParseEmails(raw) {
		if !mcpShareEmailPattern.MatchString(addr) {
			invalid = append(invalid, addr)
		}
	}
	return invalid
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

// mcpShareIsInvalidEmail reports whether a backend error is an invite-address
// rejection, so the create form can point at the email field instead of showing
// a generic failure.
func mcpShareIsInvalidEmail(err error) bool {
	var he *memoryHTTPError
	if !errors.As(err, &he) {
		return false
	}
	return he.Code == "invalid_email" ||
		strings.Contains(strings.ToLower(he.Message), "invalid email address")
}

// mcpShareInvalidEmailMessage extracts the offending address from a backend
// rejection into a form-field message, using the backend's own copy rather than
// the "memory <status> <code>:" rendering.
func mcpShareInvalidEmailMessage(err error) string {
	const marker = "invalid email address:"
	var he *memoryHTTPError
	if errors.As(err, &he) && strings.Contains(he.Message, marker) {
		if addr := strings.TrimSpace(he.Message[strings.Index(he.Message, marker)+len(marker):]); addr != "" {
			return "Not a valid email address: " + addr
		}
	}
	return "One of the email addresses is not valid."
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

func errShareInviteNeedsRecipients() error {
	return errors.New("add at least one recipient email address")
}

// errShareKeyNotRecoverable explains the one dead end of the ad-hoc invite: a
// key stored before token encryption was enabled cannot be read back, so the
// only way to email it is to issue a new one.
func errShareKeyNotRecoverable() error {
	return errors.New("this share's key was stored before key encryption was enabled, so it cannot be emailed. Use “Issue a new key instead”, or rotate the share to issue a new one")
}

func legacyShareInviteErr() error {
	return errors.New("legacy shares cannot be emailed from this UI — create a named share instead")
}

// mcpShareIsNotRecoverable reports whether a backend error means the share key
// has no stored ciphertext (422 not_recoverable).
func mcpShareIsNotRecoverable(err error) bool {
	var he *memoryHTTPError
	if !errors.As(err, &he) {
		return false
	}
	return he.Code == "not_recoverable" ||
		strings.Contains(strings.ToLower(he.Message), "cannot be recovered") ||
		strings.Contains(strings.ToLower(he.Message), "not_recoverable")
}

func legacyShareEditErr() error {
	return errors.New("legacy shares cannot be edited — recreate the share to change its tools")
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
