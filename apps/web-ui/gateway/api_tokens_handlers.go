package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emergent-company/go-daisy/components/ui"
	"github.com/labstack/echo/v4"
)

// --- API tokens web UI (project at /settings/tokens + account at /profile/tokens) ---
//
// Two parallel surfaces share one management model: the active project's
// tokens (project-scoped Memory calls) and the signed-in user's account tokens
// (account-scoped Memory calls). Both now use the same page shapes: a TABLE
// list on the base path, a standalone create page at <base>/new, and a
// per-token edit-scopes page at <base>/:tokenId/edit. Create / regenerate
// render the plaintext exactly once in a one-shot panel and do NOT redirect
// (the secret would be lost). Revoke and scope saves are PRG flows that
// redirect back with a flash.

// apiTokensPageData is the payload for the list pages (project APITokensPage,
// account AccountTokensPage): the surface's tokens, a whole-page LoadErr when
// the list is unreachable, PRG flash feedback, and an optional one-shot
// plaintext panel (regenerate responses — see apiTokenCreatePageData for the
// create-flow variant, which renders on the create page instead).
type apiTokensPageData struct {
	Tokens   []APIToken
	LoadErr  error
	FlashMsg string
	FlashErr error
	Reveal   *apiTokenReveal
}

// apiTokenCreatePageData is the payload for the standalone create pages
// (/settings/tokens/new and /profile/tokens/new): the create form plus PRG
// error feedback, and — after a successful create POST — a one-shot plaintext
// panel carrying the secret (the create page re-renders, no redirect).
type apiTokenCreatePageData struct {
	Reveal   *apiTokenReveal
	FlashErr error
}

// apiTokenEditPageData is the payload for the per-token edit-scopes pages
// (/settings/tokens/:tokenId/edit and /profile/tokens/:tokenId/edit): the
// token (metadata only — scopes pre-check the picker) plus PRG error feedback
// from the scope-save flow. NotFound / LoadErr render their own states.
type apiTokenEditPageData struct {
	Token    *APIToken
	LoadErr  error
	NotFound bool
	FlashErr error
}

// apiTokenReveal is the one-shot secret panel: the plaintext value of a just
// created / regenerated token. It is rendered on the response page and never
// stored anywhere.
type apiTokenReveal struct {
	Heading     string // "Token created" / "Token regenerated"
	Name        string
	Token       string // plaintext emt_* value
	DismissHref string // link that drops the panel (plain list GET)
}

// apiTokenScopeOption is one selectable scope in the picker (wire value + a
// short human label).
type apiTokenScopeOption struct {
	Value string
	Label string
}

// apiTokenScopeGroup is one labelled bucket of the scope picker. Buckets are
// AREAS of the product (Schemas, Graph, …), so a scope is found by what it acts
// on rather than by how coarse it is. admin:all is intentionally NOT offered:
// only org admins can hold it, and the 403 is surfaced if Memory rejects it.
type apiTokenScopeGroup struct {
	Label   string
	Hint    string
	Options []apiTokenScopeOption
}

// Scope areas: the human-facing buckets shared by the token-table badges and
// the picker groups. Other is the fallback for scopes the gateway does not
// recognise, so an unknown scope is still surfaced rather than hidden.
const (
	apiTokenAreaSchemas   = "Schemas"
	apiTokenAreaData      = "Data"
	apiTokenAreaDocuments = "Documents"
	apiTokenAreaGraph     = "Graph"
	apiTokenAreaBranches  = "Branches"
	apiTokenAreaAgents    = "Agents"
	apiTokenAreaProjects  = "Projects"
	apiTokenAreaJournal   = "Journal"
	apiTokenAreaSkills    = "Skills"
	apiTokenAreaAdmin     = "Admin"
	apiTokenAreaOther     = "Other"
)

// apiTokenAreas is the canonical taxonomy order, used by both the token-table
// badges and the picker groups.
var apiTokenAreas = []string{
	apiTokenAreaSchemas, apiTokenAreaData, apiTokenAreaDocuments,
	apiTokenAreaGraph, apiTokenAreaBranches, apiTokenAreaAgents,
	apiTokenAreaProjects, apiTokenAreaJournal, apiTokenAreaSkills,
	apiTokenAreaAdmin,
}

// apiTokenScopeAreas is the single source of truth mapping every known token
// scope to its area. admin:all shares the Admin area with admin even though the
// picker omits it.
var apiTokenScopeAreas = map[string]string{
	"schema:read": apiTokenAreaSchemas, "schema:write": apiTokenAreaSchemas, "schema:migrate": apiTokenAreaSchemas,
	"data:read": apiTokenAreaData, "data:write": apiTokenAreaData,
	"documents:read": apiTokenAreaDocuments, "documents:write": apiTokenAreaDocuments,
	"graph:read": apiTokenAreaGraph, "graph:write": apiTokenAreaGraph, "search": apiTokenAreaGraph,
	"branches:read": apiTokenAreaBranches, "branches:write": apiTokenAreaBranches,
	"agents:read": apiTokenAreaAgents, "agents:write": apiTokenAreaAgents, "chat:use": apiTokenAreaAgents,
	"projects:read": apiTokenAreaProjects, "projects:write": apiTokenAreaProjects,
	"journal:read": apiTokenAreaJournal, "journal:write": apiTokenAreaJournal,
	"skills:read": apiTokenAreaSkills, "skills:write": apiTokenAreaSkills,
	"admin": apiTokenAreaAdmin, "admin:all": apiTokenAreaAdmin,
}

// apiTokenScopeArea returns the human area a scope belongs to, falling back to
// Other so an unrecognised scope stays visible.
func apiTokenScopeArea(scope string) string {
	if area, ok := apiTokenScopeAreas[scope]; ok {
		return area
	}
	return apiTokenAreaOther
}

// apiTokenScopeMutates reports whether a scope can change state — a write, a
// migration, or an execution like chat/search — rather than only read it.
func apiTokenScopeMutates(scope string) bool {
	switch scope {
	case "admin", "admin:all", "schema:migrate", "chat:use", "search":
		return true
	}
	return strings.HasSuffix(scope, ":write")
}

// apiTokenScopeAreaIntent derives an area badge's colour from the scopes the
// token holds in it: Admin always warns; any mutating scope makes the area
// BadgeInfo; an area held with read-only scopes stays BadgeNeutral.
func apiTokenScopeAreaIntent(area string, scopes []string) ui.BadgeIntent {
	if area == apiTokenAreaAdmin {
		return ui.BadgeWarning
	}
	for _, s := range scopes {
		if apiTokenScopeMutates(s) {
			return ui.BadgeInfo
		}
	}
	return ui.BadgeNeutral
}

// apiTokenAreaBadge is one composed area badge in the token table: the area's
// human name, the exact granted scopes that fall in it (the tooltip detail),
// and the intent derived from those scopes.
type apiTokenAreaBadge struct {
	Area   string
	Scopes []string
	Intent ui.BadgeIntent
}

// apiTokenAreaBadges groups a token's scopes into one badge per area, in
// canonical taxonomy order with the Other bucket last. Every granted scope
// lands in exactly one badge, so nothing is silently hidden.
func apiTokenAreaBadges(scopes []string) []apiTokenAreaBadge {
	grouped := make(map[string][]string, len(apiTokenAreas)+1)
	for _, s := range scopes {
		area := apiTokenScopeArea(s)
		grouped[area] = append(grouped[area], s)
	}
	out := make([]apiTokenAreaBadge, 0, len(grouped))
	for _, area := range apiTokenAreas {
		if held, ok := grouped[area]; ok {
			out = append(out, apiTokenAreaBadge{Area: area, Scopes: held, Intent: apiTokenScopeAreaIntent(area, held)})
		}
	}
	if held, ok := grouped[apiTokenAreaOther]; ok {
		out = append(out, apiTokenAreaBadge{Area: apiTokenAreaOther, Scopes: held, Intent: apiTokenScopeAreaIntent(apiTokenAreaOther, held)})
	}
	return out
}

// scopePickerGroups orders the area buckets (excluding admin:all) for the
// create and edit-scope forms. Checkbox values stay the raw scope strings.
var scopePickerGroups = []apiTokenScopeGroup{
	{
		Label: apiTokenAreaSchemas, Hint: "Schema definitions and migrations.",
		Options: []apiTokenScopeOption{
			{"schema:read", "Read schemas"}, {"schema:write", "Edit schemas"},
			{"schema:migrate", "Run schema migrations"},
		},
	},
	{
		Label: apiTokenAreaData, Hint: "Stored data records.",
		Options: []apiTokenScopeOption{
			{"data:read", "Read data"}, {"data:write", "Write data"},
		},
	},
	{
		Label: apiTokenAreaDocuments, Hint: "Documents and their content.",
		Options: []apiTokenScopeOption{
			{"documents:read", "Read documents"}, {"documents:write", "Write documents"},
		},
	},
	{
		Label: apiTokenAreaGraph, Hint: "Knowledge graph objects, links, and search.",
		Options: []apiTokenScopeOption{
			{"graph:read", "Read knowledge graph"}, {"graph:write", "Write knowledge graph"},
			{"search", "Search memory"},
		},
	},
	{
		Label: apiTokenAreaBranches, Hint: "Branches and merges.",
		Options: []apiTokenScopeOption{
			{"branches:read", "Read branches"}, {"branches:write", "Create & merge branches"},
		},
	},
	{
		Label: apiTokenAreaAgents, Hint: "Agents and chat.",
		Options: []apiTokenScopeOption{
			{"agents:read", "Read agents"}, {"agents:write", "Create & edit agents"},
			{"chat:use", "Use chat"},
		},
	},
	{
		Label: apiTokenAreaProjects, Hint: "Project settings and membership.",
		Options: []apiTokenScopeOption{
			{"projects:read", "Read projects"}, {"projects:write", "Manage projects"},
		},
	},
	{
		Label: apiTokenAreaJournal, Hint: "Journal entries.",
		Options: []apiTokenScopeOption{
			{"journal:read", "Read journal"}, {"journal:write", "Write journal"},
		},
	},
	{
		Label: apiTokenAreaSkills, Hint: "Skills.",
		Options: []apiTokenScopeOption{
			{"skills:read", "Read skills"}, {"skills:write", "Write skills"},
		},
	},
	{
		Label: apiTokenAreaAdmin, Hint: "Administrative powers.",
		Options: []apiTokenScopeOption{
			{"admin", "Administer project"},
		},
	},
}

// apiTokenLastUsedLabel renders a token's last-used time ("never" when the
// token has never been used; the server sends no value in that case).
func apiTokenLastUsedLabel(t *time.Time) string {
	if t == nil {
		return "never"
	}
	return relTime(t.Format(time.RFC3339))
}

// apiTokenFormScopes reads the checked scope checkboxes (name="scopes") from a
// submitted create/edit form, validates each against the supported reference
// set, and requires at least one scope. Scopes outside the set are rejected so
// the gateway never forwards a scope Memory does not know.
func apiTokenFormScopes(c echo.Context) ([]string, error) {
	if err := c.Request().ParseForm(); err != nil {
		return nil, err
	}
	raw := c.Request().Form["scopes"]
	var out []string
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !validAPITokenScope(s) {
			return nil, fmt.Errorf("unsupported scope %q", s)
		}
		dup := false
		for _, have := range out {
			if have == s {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("select at least one scope")
	}
	return out, nil
}

// apiTokenFormName reads and trims the create form's name field.
func apiTokenFormName(c echo.Context) (string, error) {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return "", fmt.Errorf("token name is required")
	}
	if len(name) > 255 {
		return "", fmt.Errorf("token name must be at most 255 characters")
	}
	return name, nil
}

// apiTokenPathBase returns the token-area base path for a surface: the project
// surface at /settings/tokens or the account surface at /profile/tokens. All
// page/form targets are derived from it.
func apiTokenPathBase(account bool) string {
	if account {
		return "/profile/tokens"
	}
	return "/settings/tokens"
}

// apiTokenID reads and validates the :tokenId path parameter.
func apiTokenID(c echo.Context) (string, error) {
	tokenID := strings.TrimSpace(c.Param("tokenId"))
	if tokenID == "" {
		return "", fmt.Errorf("token id is required")
	}
	return tokenID, nil
}

// --- shared list/create/edit machinery (project + account) ---

// loadTokenList fills the surface's tokens (project or account list) into data,
// setting LoadErr on failure (rendered as the whole-page error state).
func (s *Server) loadTokenList(ctx context.Context, data *apiTokensPageData, account bool) {
	var (
		tokens []APIToken
		err    error
	)
	if account {
		tokens, err = s.memory.ListAccountAPITokens(ctx)
	} else {
		tokens, err = s.memory.ListAPITokens(ctx)
	}
	if err != nil {
		data.LoadErr = err
		return
	}
	data.Tokens = tokens
}

// findTokenByID returns a pointer to the token with the given id, or nil. The
// list carries metadata only — never the plaintext.
func findTokenByID(tokens []APIToken, id string) *APIToken {
	for i := range tokens {
		if tokens[i].ID == id {
			return &tokens[i]
		}
	}
	return nil
}

// uiTokenList renders the token list page for a surface (GET <base>). The list
// carries a TABLE of token metadata with row actions and a "New token" action
// linking to the standalone create page at <base>/new. ?updated=1 / ?err=
// surface PRG feedback from the revoke and scope-save flows.
func (s *Server) uiTokenList(c echo.Context, account bool) error {
	ctx := c.Request().Context()
	data := apiTokensPageData{}
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "API token updated."
	}
	data.FlashErr = flashError(c)
	s.loadTokenList(ctx, &data, account)
	if account {
		return s.page(c, pageTitle("Account API tokens"), AccountTokensPage(data))
	}
	return s.page(c, pageTitle("API Tokens"), APITokensPage(data))
}

// renderTokenListWithReveal re-renders a surface's list page after a regenerate
// POST (no redirect — the one-shot plaintext panel must reach the browser). A
// list failure after a successful write degrades to a page error beneath the
// panel so the secret is never lost.
func (s *Server) renderTokenListWithReveal(c echo.Context, reveal *apiTokenReveal, account bool) error {
	ctx := c.Request().Context()
	data := apiTokensPageData{Reveal: reveal}
	s.loadTokenList(ctx, &data, account)
	if account {
		return s.page(c, pageTitle("Account API tokens"), AccountTokensPage(data))
	}
	return s.page(c, pageTitle("API Tokens"), APITokensPage(data))
}

// uiTokenNewPage renders the standalone create-token page (GET <base>/new):
// name + scope picker posting to POST <base>/new. ?err= surfaces PRG feedback.
func (s *Server) uiTokenNewPage(c echo.Context, account bool) error {
	flash := apiTokenCreatePageData{FlashErr: flashError(c)}
	if account {
		return s.page(c, pageTitle("New account token"), AccountTokenCreatePage(flash))
	}
	return s.page(c, pageTitle("New API token"), APITokenCreatePage(flash))
}

// renderTokenCreatePage re-renders a surface's create page after a successful
// create POST (no redirect — the plaintext panel must reach the browser).
func (s *Server) renderTokenCreatePage(c echo.Context, reveal *apiTokenReveal, account bool) error {
	data := apiTokenCreatePageData{Reveal: reveal}
	if account {
		return s.page(c, pageTitle("New account token"), AccountTokenCreatePage(data))
	}
	return s.page(c, pageTitle("New API token"), APITokenCreatePage(data))
}

// uiTokenCreate handles the create form (POST <base>/new; fields name,
// scopes[]). On success the create page re-renders with the plaintext shown
// exactly once; on validation or Memory errors it PRG-redirects back to
// <base>/new with the error.
func (s *Server) uiTokenCreate(c echo.Context, account bool) error {
	ctx := c.Request().Context()
	base := apiTokenPathBase(account)
	name, err := apiTokenFormName(c)
	if err != nil {
		return redirectWithError(c, base+"/new", err)
	}
	scopes, err := apiTokenFormScopes(c)
	if err != nil {
		return redirectWithError(c, base+"/new", err)
	}
	var resp *APITokenCreateResponse
	if account {
		resp, err = s.memory.CreateAccountAPIToken(ctx, name, scopes)
	} else {
		resp, err = s.memory.CreateAPIToken(ctx, name, scopes)
	}
	if err != nil {
		return redirectWithError(c, base+"/new", err)
	}
	heading := "API token created"
	if account {
		heading = "Account token created"
	}
	return s.renderTokenCreatePage(c, &apiTokenReveal{
		Heading:     heading,
		Name:        resp.Name,
		Token:       resp.Token,
		DismissHref: base,
	}, account)
}

// uiTokenEditPage renders the per-token edit-scopes page (GET
// <base>/:tokenId/edit): the token name and its scope checkboxes pre-checked
// from the token's current scopes. The token is resolved by re-fetching the
// metadata list and matching the id (never the plaintext); an unknown id
// renders the not-found state.
func (s *Server) uiTokenEditPage(c echo.Context, account bool) error {
	ctx := c.Request().Context()
	base := apiTokenPathBase(account)
	tokenID, err := apiTokenID(c)
	if err != nil {
		return redirectWithError(c, base, err)
	}
	data := apiTokenEditPageData{FlashErr: flashError(c)}
	var tokens []APIToken
	if account {
		tokens, err = s.memory.ListAccountAPITokens(ctx)
	} else {
		tokens, err = s.memory.ListAPITokens(ctx)
	}
	if err != nil {
		data.LoadErr = err
	} else if tok := findTokenByID(tokens, tokenID); tok != nil {
		data.Token = tok
	} else {
		data.NotFound = true
	}
	if account {
		return s.page(c, pageTitle("Edit scopes"), AccountTokenEditPage(data))
	}
	return s.page(c, pageTitle("Edit scopes"), APITokenEditPage(data))
}

// uiTokenScopes handles one scope-save form (POST <base>/:tokenId/scopes,
// PRG). Only the submitted scopes are sent — the name is immutable and never
// re-submitted. Success redirects to the list with ?updated=1; failure back to
// the edit page with the error.
func (s *Server) uiTokenScopes(c echo.Context, account bool) error {
	ctx := c.Request().Context()
	base := apiTokenPathBase(account)
	tokenID, err := apiTokenID(c)
	if err != nil {
		return redirectWithError(c, base, err)
	}
	scopes, err := apiTokenFormScopes(c)
	if err != nil {
		return redirectWithError(c, base+"/"+url.PathEscape(tokenID)+"/edit", err)
	}
	if account {
		_, err = s.memory.UpdateAccountAPITokenScopes(ctx, tokenID, scopes)
	} else {
		_, err = s.memory.UpdateAPITokenScopes(ctx, tokenID, scopes)
	}
	if err != nil {
		return redirectWithError(c, base+"/"+url.PathEscape(tokenID)+"/edit", err)
	}
	return c.Redirect(http.StatusSeeOther, base+"?updated=1")
}

// uiTokenRevoke handles one revoke form (POST <base>/:tokenId/revoke, PRG).
// Revoking invalidates the token immediately.
func (s *Server) uiTokenRevoke(c echo.Context, account bool) error {
	base := apiTokenPathBase(account)
	tokenID, err := apiTokenID(c)
	if err != nil {
		return redirectWithError(c, base, err)
	}
	if account {
		err = s.memory.RevokeAccountAPIToken(c.Request().Context(), tokenID)
	} else {
		err = s.memory.RevokeAPIToken(c.Request().Context(), tokenID)
	}
	if err != nil {
		return redirectWithError(c, base, err)
	}
	return c.Redirect(http.StatusSeeOther, base+"?updated=1")
}

// uiTokenRegenerate handles one regenerate form (POST <base>/:tokenId/
// regenerate). The old token is revoked atomically and the replacement's
// plaintext is rendered exactly once on the list page (no redirect).
func (s *Server) uiTokenRegenerate(c echo.Context, account bool) error {
	ctx := c.Request().Context()
	base := apiTokenPathBase(account)
	tokenID, err := apiTokenID(c)
	if err != nil {
		return redirectWithError(c, base, err)
	}
	var resp *APITokenCreateResponse
	if account {
		resp, err = s.memory.RegenerateAccountAPIToken(ctx, tokenID)
	} else {
		resp, err = s.memory.RegenerateAPIToken(ctx, tokenID)
	}
	if err != nil {
		return redirectWithError(c, base, err)
	}
	heading := "API token regenerated"
	if account {
		heading = "Account token regenerated"
	}
	return s.renderTokenListWithReveal(c, &apiTokenReveal{
		Heading:     heading,
		Name:        resp.Name,
		Token:       resp.Token,
		DismissHref: base,
	}, account)
}

// --- project surface (/settings/tokens) ---

func (s *Server) uiAPITokens(c echo.Context) error         { return s.uiTokenList(c, false) }
func (s *Server) uiAPITokensNewPage(c echo.Context) error  { return s.uiTokenNewPage(c, false) }
func (s *Server) uiAPITokensCreate(c echo.Context) error   { return s.uiTokenCreate(c, false) }
func (s *Server) uiAPITokensEditPage(c echo.Context) error { return s.uiTokenEditPage(c, false) }
func (s *Server) uiAPITokensScopes(c echo.Context) error   { return s.uiTokenScopes(c, false) }
func (s *Server) uiAPITokensRevoke(c echo.Context) error   { return s.uiTokenRevoke(c, false) }
func (s *Server) uiAPITokensRegenerate(c echo.Context) error {
	return s.uiTokenRegenerate(c, false)
}

// --- account surface (/profile/tokens) ---

func (s *Server) uiProfileTokens(c echo.Context) error          { return s.uiTokenList(c, true) }
func (s *Server) uiProfileTokensNewPage(c echo.Context) error   { return s.uiTokenNewPage(c, true) }
func (s *Server) uiProfileAPITokensCreate(c echo.Context) error { return s.uiTokenCreate(c, true) }
func (s *Server) uiProfileAPITokensEditPage(c echo.Context) error {
	return s.uiTokenEditPage(c, true)
}
func (s *Server) uiProfileAPITokensScopes(c echo.Context) error { return s.uiTokenScopes(c, true) }
func (s *Server) uiProfileAPITokensRevoke(c echo.Context) error { return s.uiTokenRevoke(c, true) }
func (s *Server) uiProfileAPITokensRegenerate(c echo.Context) error {
	return s.uiTokenRegenerate(c, true)
}
