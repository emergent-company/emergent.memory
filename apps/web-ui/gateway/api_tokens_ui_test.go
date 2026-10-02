package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	ui "github.com/emergent-company/go-daisy/components/ui"
)

// apiTokenRenderFixture returns the token payloads used by the render tests:
// one live project token and one revoked project token.
func apiTokenRenderFixture() []APIToken {
	return []APIToken{
		{
			ID: "t1", Name: "CI deploy", TokenPrefix: "emt_cideployx",
			Scopes:     []string{"data:read", "data:write"},
			CreatedAt:  "2026-09-01T10:00:00Z",
			LastUsedAt: timePtr(time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)),
		},
		{
			ID: "t2", Name: "old token", TokenPrefix: "emt_oldtoken",
			Scopes: []string{"schema:read"}, CreatedAt: "2026-08-01T10:00:00Z",
			IsRevoked: true,
		},
	}
}

func timePtr(t time.Time) *time.Time { return &t }

// accountTokenRenderFixture returns the account-token payloads used by the
// account render tests.
func accountTokenRenderFixture() []APIToken {
	return []APIToken{
		{
			ID: "a1", Name: "personal script", TokenPrefix: "emt_personalx",
			Scopes: []string{"search", "journal:read"}, CreatedAt: "2026-09-01T10:00:00Z",
		},
	}
}

// TestRenderAPITokensPage asserts the project list renders the token TABLE
// (name/prefix/area scopes/created/last used/actions + revoked state), the
// count badge, the "New token" action — and never any plaintext or inline form.
func TestRenderAPITokensPage(t *testing.T) {
	data := apiTokensPageData{Tokens: apiTokenRenderFixture()}
	html := renderHTML(t, APITokensPage(data))
	for _, want := range []string{
		"API Tokens", "CI deploy", "old token", "emt_cideployx", "emt_oldtoken",
		// one composed AREA badge per area, with the granted scopes in the title
		// and the same detail exposed as an accessible name (role=img + aria-label)
		`data-testid="token-scope-area"`, ">Data<", ">Schemas<",
		`title="data:read, data:write"`, `title="schema:read"`,
		`role="img"`,
		`aria-label="Data: data:read, data:write"`, `aria-label="Schemas: schema:read"`,
		"Revoked", "1 token",
		"New token", `href="/settings/tokens/new"`,
		// revoked tokens live in a collapsed disclosure with its own count
		`data-testid="revoked-tokens-toggle"`, `data-testid="revoked-tokens-section"`,
		// table chrome
		`<table class="table table-sm">`, "<th>Name</th>", "<th>Prefix</th>",
		"<th>Scopes</th>", "<th>Created</th>", "<th>Last used</th>",
		// per-token actions: regenerate/revoke forms + edit link
		`action="/settings/tokens/t1/revoke"`,
		`action="/settings/tokens/t1/regenerate"`, `href="/settings/tokens/t1/edit"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("APITokensPage missing %q", want)
		}
	}
	// revoked tokens render only inside the collapsed disclosure: the primary
	// table (everything before the revoked section) must not carry them, and the
	// disclosure must be closed (no open attribute) and hold the revoked row.
	revokedIdx := strings.Index(html, `data-testid="revoked-tokens-toggle"`)
	if revokedIdx < 0 {
		t.Fatal("APITokensPage must render the revoked disclosure")
	}
	primary, revokedSection := html[:revokedIdx], html[revokedIdx:]
	if strings.Contains(primary, "old token") || strings.Contains(primary, "emt_oldtoken") || strings.Contains(primary, `data-testid="token-revoked-badge"`) {
		t.Error("active table must not contain revoked tokens")
	}
	if !strings.Contains(revokedSection, "old token") || !strings.Contains(revokedSection, `data-testid="token-revoked-badge"`) {
		t.Error("revoked token must render inside the revoked disclosure with its badge")
	}
	if tag := detailsTagFor(html, "revoked-tokens-section"); strings.Contains(tag, "open") {
		t.Errorf("revoked disclosure must be collapsed by default, got %q", tag)
	}
	// a token with a write scope in an area gets the info tone; a read-only
	// area stays neutral
	if !strings.Contains(html, "badge-info") || !strings.Contains(html, "badge-neutral") {
		t.Errorf("area badges should carry area-level intent tones, html=%q", html)
	}
	// the create form no longer lives on the list page
	if strings.Contains(html, `action="/settings/tokens/new"`) || strings.Contains(html, `name="name"`) {
		t.Error("list page must not embed the create form (it moved to /settings/tokens/new)")
	}
	// revoked tokens get no actions
	if strings.Contains(html, `action="/settings/tokens/t2/revoke"`) || strings.Contains(html, `href="/settings/tokens/t2/edit"`) {
		t.Error("revoked token must not offer destructive actions or scope editing")
	}
	// no plaintext anywhere, no plaintext panel by default
	if strings.Contains(html, "api-token-secret") || strings.Contains(html, "shown only once") {
		t.Error("plaintext panel must not render on the plain list page")
	}
}

// TestRenderAPITokensPageOnlyRevoked asserts that when every token is revoked
// the page shows the "no active tokens" state (not the "No API tokens yet"
// empty state) while still exposing the revoked disclosure.
func TestRenderAPITokensPageOnlyRevoked(t *testing.T) {
	tokens := []APIToken{{
		ID: "t2", Name: "old token", TokenPrefix: "emt_oldtoken",
		Scopes: []string{"data:read"}, CreatedAt: "2026-08-01T10:00:00Z", IsRevoked: true,
	}}
	html := renderHTML(t, APITokensPage(apiTokensPageData{Tokens: tokens}))
	if !strings.Contains(html, "No active tokens") {
		t.Error("all-revoked page must show the no-active-tokens state")
	}
	if strings.Contains(html, "No API tokens yet") {
		t.Error("all-revoked page must not claim no tokens exist")
	}
	if !strings.Contains(html, "0 tokens") {
		t.Errorf("all-revoked page should show the 0 active count, html=%q", html)
	}
	if !strings.Contains(html, `data-testid="revoked-tokens-toggle"`) || !strings.Contains(html, "old token") {
		t.Error("all-revoked page must keep the revoked disclosure with the revoked token")
	}
}

// detailsTagFor returns the opening <details ...> tag carrying the given
// data-testid, so tests can assert the disclosure is collapsed (no open attr).
func detailsTagFor(html, testid string) string {
	idx := strings.Index(html, `data-testid="`+testid+`"`)
	if idx < 0 {
		return ""
	}
	start := strings.LastIndex(html[:idx], "<details")
	if start < 0 {
		return ""
	}
	end := strings.Index(html[start:], ">")
	if end < 0 {
		return ""
	}
	return html[start : start+end+1]
}

// TestRenderAPITokensPageUnknownScopeShowsOther asserts an unmapped scope is
// surfaced in the fallback Other area badge rather than silently dropped.
func TestRenderAPITokensPageUnknownScopeShowsOther(t *testing.T) {
	tokens := []APIToken{{
		ID: "t1", Name: "legacy", TokenPrefix: "emt_legacy",
		Scopes: []string{"mystery:scope", "data:read"}, CreatedAt: "2026-09-01T10:00:00Z",
	}}
	html := renderHTML(t, APITokensPage(apiTokensPageData{Tokens: tokens}))
	for _, want := range []string{`data-testid="token-scope-area"`, ">Other<", `title="mystery:scope"`, ">Data<"} {
		if !strings.Contains(html, want) {
			t.Errorf("unknown scope render missing %q", want)
		}
	}
}

// TestAPITokenScopeAreaMapping pins every known scope to its area and asserts
// an unmapped scope falls back to Other.
func TestAPITokenScopeAreaMapping(t *testing.T) {
	cases := map[string]string{
		"schema:read": apiTokenAreaSchemas, "schema:write": apiTokenAreaSchemas, "schema:migrate": apiTokenAreaSchemas,
		"data:read": apiTokenAreaData, "data:write": apiTokenAreaData,
		"documents:read": apiTokenAreaDocuments, "documents:write": apiTokenAreaDocuments,
		"graph:read": apiTokenAreaGraph, "graph:write": apiTokenAreaGraph, "search": apiTokenAreaGraph,
		"branches:read": apiTokenAreaBranches, "branches:write": apiTokenAreaBranches,
		"agents:read": apiTokenAreaAgents, "agents:write": apiTokenAreaAgents, "chat:use": apiTokenAreaAgents,
		"projects:read": apiTokenAreaProjects, "projects:write": apiTokenAreaProjects, "project:admin": apiTokenAreaProjects,
		"journal:read": apiTokenAreaJournal, "journal:write": apiTokenAreaJournal,
		"skills:read": apiTokenAreaSkills, "skills:write": apiTokenAreaSkills,
		"admin": apiTokenAreaAdmin, "admin:all": apiTokenAreaAdmin,
		"mystery:scope": apiTokenAreaOther,
	}
	for scope, want := range cases {
		if got := apiTokenScopeArea(scope); got != want {
			t.Errorf("apiTokenScopeArea(%q) = %q, want %q", scope, got, want)
		}
	}
}

// TestAPITokenAreaBadges asserts scopes compose into one badge per area in
// canonical order, with area-level intent and exact scopes kept for the title.
func TestAPITokenAreaBadges(t *testing.T) {
	got := apiTokenAreaBadges([]string{"search", "data:read", "mystery:scope", "graph:read", "admin"})
	want := []apiTokenAreaBadge{
		{Area: apiTokenAreaData, Scopes: []string{"data:read"}, Intent: ui.BadgeNeutral},
		{Area: apiTokenAreaGraph, Scopes: []string{"search", "graph:read"}, Intent: ui.BadgeInfo},
		{Area: apiTokenAreaAdmin, Scopes: []string{"admin"}, Intent: ui.BadgeWarning},
		{Area: apiTokenAreaOther, Scopes: []string{"mystery:scope"}, Intent: ui.BadgeNeutral},
	}
	if len(got) != len(want) {
		t.Fatalf("apiTokenAreaBadges = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Area != want[i].Area {
			t.Errorf("badge[%d].Area = %q, want %q", i, got[i].Area, want[i].Area)
		}
		if got[i].Intent != want[i].Intent {
			t.Errorf("badge[%d].Intent = %q, want %q", i, got[i].Intent, want[i].Intent)
		}
		if !slices.Equal(got[i].Scopes, want[i].Scopes) {
			t.Errorf("badge[%d].Scopes = %v, want %v", i, got[i].Scopes, want[i].Scopes)
		}
	}
	// read-only and mutating areas resolve to the right tone
	if apiTokenScopeAreaIntent(apiTokenAreaGraph, []string{"graph:read"}) != ui.BadgeNeutral {
		t.Error("read-only area should be neutral")
	}
	if apiTokenScopeAreaIntent(apiTokenAreaData, []string{"data:read", "data:write"}) != ui.BadgeInfo {
		t.Error("area holding a write scope should be info")
	}
	if apiTokenScopeAreaIntent(apiTokenAreaAdmin, nil) != ui.BadgeWarning {
		t.Error("admin area should warn")
	}
}

// TestAPITokenScopeMutates pins which scopes change state. apiTokenScopeMutates
// special-cases admin/admin:all, schema:migrate, chat:use and search (none carry
// the :write suffix), so those are asserted explicitly to catch a dropped case.
func TestAPITokenScopeMutates(t *testing.T) {
	for _, scope := range []string{"admin", "admin:all", "project:admin", "schema:migrate", "chat:use", "search"} {
		if !apiTokenScopeMutates(scope) {
			t.Errorf("apiTokenScopeMutates(%q) = false, want true (special-cased)", scope)
		}
	}
	for _, scope := range []string{"schema:read", "data:read", "documents:read", "graph:read", "branches:read", "agents:read", "projects:read", "journal:read", "skills:read"} {
		if apiTokenScopeMutates(scope) {
			t.Errorf("apiTokenScopeMutates(%q) = true, want false (read-only)", scope)
		}
	}
	for _, scope := range apiTokenScopes {
		if strings.HasSuffix(scope, ":write") && !apiTokenScopeMutates(scope) {
			t.Errorf("apiTokenScopeMutates(%q) = false, want true (:write)", scope)
		}
	}
}

// TestRenderAPITokensPageEmpty asserts the empty state with its New token CTA.
func TestRenderAPITokensPageEmpty(t *testing.T) {
	html := renderHTML(t, APITokensPage(apiTokensPageData{}))
	for _, want := range []string{"No API tokens yet", `href="/settings/tokens/new"`, "New token"} {
		if !strings.Contains(html, want) {
			t.Errorf("empty APITokensPage missing %q", want)
		}
	}
	if !strings.Contains(html, "0 tokens") {
		t.Errorf("empty page should show the 0 count, html=%q", html)
	}
	if strings.Contains(html, `name="name"`) {
		t.Error("empty list page must not embed the create form")
	}
}

// TestRenderAPITokensPageLoadError asserts the whole-page error state.
func TestRenderAPITokensPageLoadError(t *testing.T) {
	html := renderHTML(t, APITokensPage(apiTokensPageData{LoadErr: errTest}))
	if !strings.Contains(html, "API tokens unavailable") || !strings.Contains(html, "backend unreachable") {
		t.Error("load-error state missing")
	}
}

// TestRenderAccountTokensPage asserts the account token list renders in the
// profile rail ("API tokens" active) with the shared TABLE and New token CTA.
func TestRenderAccountTokensPage(t *testing.T) {
	data := apiTokensPageData{Tokens: accountTokenRenderFixture()}
	html := renderHTML(t, AccountTokensPage(data))
	for _, want := range []string{
		"API tokens", "personal script", "emt_personalx",
		// search belongs to the Graph area, journal:read to the Journal area
		`data-testid="token-scope-area"`, ">Graph<", ">Journal<",
		`title="search"`, `title="journal:read"`,
		"1 token",
		"New token", `href="/profile/tokens/new"`,
		`action="/profile/tokens/a1/revoke"`,
		`action="/profile/tokens/a1/regenerate"`, `href="/profile/tokens/a1/edit"`,
		// profile rail links + the active item carries aria-current
		`href="/profile"`, `href="/profile/invitations"`, `href="/profile/tokens" aria-current="page"`,
		"Profile", "Invitations",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("AccountTokensPage missing %q", want)
		}
	}
	if strings.Contains(html, "CI deploy") {
		t.Error("account token list must not render project tokens")
	}
}

// TestRenderAccountTokensPageEmpty asserts the account empty state.
func TestRenderAccountTokensPageEmpty(t *testing.T) {
	html := renderHTML(t, AccountTokensPage(apiTokensPageData{}))
	for _, want := range []string{"No account tokens yet", `href="/profile/tokens/new"`, "New token"} {
		if !strings.Contains(html, want) {
			t.Errorf("empty AccountTokensPage missing %q", want)
		}
	}
	if strings.Contains(html, "No API tokens yet") {
		t.Error("project empty-state copy must not leak into the account surface")
	}
}

// TestRenderAPITokenCreatePage asserts the project create page renders the
// name field + grouped scope picker posting to POST /settings/tokens/new.
func TestRenderAPITokenCreatePage(t *testing.T) {
	html := renderHTML(t, APITokenCreatePage(apiTokenCreatePageData{}))
	for _, want := range []string{
		"New token", `action="/settings/tokens/new"`, `name="name"`,
		"Create token", `href="/settings/tokens"`, "Cancel",
		// picker scopes grouped by area
		`name="scopes"`, `value="data:read"`, `value="schema:write"`, `value="chat:use"`,
		"Schemas", "Data", "Graph", "Agents",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("create page missing %q", want)
		}
	}
	if strings.Contains(html, "api-token-secret") || strings.Contains(html, "shown only once") {
		t.Error("fresh create page must not carry a plaintext panel")
	}
	if !strings.Contains(html, `name="scopes"`) {
		t.Error("scope picker must post checkboxes named scopes")
	}
}

// TestRenderAPITokenCreatePageReveal asserts that after a successful create the
// create page re-renders with the one-shot plaintext panel.
func TestRenderAPITokenCreatePageReveal(t *testing.T) {
	data := apiTokenCreatePageData{Reveal: &apiTokenReveal{
		Heading:     "API token created",
		Name:        "ci",
		Token:       "emt_ci_tok-1_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		DismissHref: "/settings/tokens",
	}}
	html := renderHTML(t, APITokenCreatePage(data))
	if got := strings.Count(html, "emt_ci_tok-1_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"); got != 1 {
		t.Errorf("plaintext occurrences on create page = %d, want exactly 1", got)
	}
	if !strings.Contains(html, "API token created") || !strings.Contains(html, `href="/settings/tokens"`) {
		t.Error("create plaintext panel missing on the create page")
	}
}

// TestRenderAccountTokenCreatePage asserts the account create page sits in the
// profile rail and posts to POST /profile/tokens/new.
func TestRenderAccountTokenCreatePage(t *testing.T) {
	html := renderHTML(t, AccountTokenCreatePage(apiTokenCreatePageData{}))
	for _, want := range []string{
		"New token", `action="/profile/tokens/new"`, `name="name"`, "Create token",
		`href="/profile/tokens" aria-current="page"`, `href="/profile/invitations"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("account create page missing %q", want)
		}
	}
}

// TestRenderAPITokenEditPage asserts the project edit-scopes page shows the
// token name and its scopes pre-checked, saving to /:id/scopes.
func TestRenderAPITokenEditPage(t *testing.T) {
	tokens := apiTokenRenderFixture()
	data := apiTokenEditPageData{Token: &tokens[0]}
	html := renderHTML(t, APITokenEditPage(data))
	for _, want := range []string{
		"Edit scopes", "CI deploy", `action="/settings/tokens/t1/scopes"`,
		"Save scopes", `href="/settings/tokens"`, "Cancel",
		// current scopes pre-checked
		`value="data:read" class="checkbox checkbox-sm mt-0.5" checked`,
		`value="data:write" class="checkbox checkbox-sm mt-0.5" checked`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
	// unrelated scopes stay unchecked
	if strings.Contains(html, `value="search" class="checkbox checkbox-sm mt-0.5" checked`) {
		t.Error("unrelated scope must not be pre-checked")
	}
}

func TestRenderAPITokenEditPageNotFoundError(t *testing.T) {
	nf := renderHTML(t, APITokenEditPage(apiTokenEditPageData{NotFound: true}))
	if !strings.Contains(nf, "Token not found") {
		t.Error("edit not-found state missing")
	}
	le := renderHTML(t, APITokenEditPage(apiTokenEditPageData{LoadErr: errTest}))
	if !strings.Contains(le, "Token unavailable") || !strings.Contains(le, "backend unreachable") {
		t.Error("edit load-error state missing")
	}
}

// TestRenderAccountTokenEditPage asserts the account edit page pre-checks the
// account token's scopes in the profile rail.
func TestRenderAccountTokenEditPage(t *testing.T) {
	tokens := accountTokenRenderFixture()
	data := apiTokenEditPageData{Token: &tokens[0]}
	html := renderHTML(t, AccountTokenEditPage(data))
	for _, want := range []string{
		"Edit scopes", "personal script", `action="/profile/tokens/a1/scopes"`,
		`value="search" class="checkbox checkbox-sm mt-0.5" checked`,
		`value="journal:read" class="checkbox checkbox-sm mt-0.5" checked`,
		`href="/profile/tokens" aria-current="page"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("account edit page missing %q", want)
		}
	}
}

// TestRenderScopePickerOmitsAdminAll asserts the picker offers every area group
// except Admin and never offers the platform admin scopes (admin, admin:all).
func TestRenderScopePickerOmitsAdminAll(t *testing.T) {
	html := renderHTML(t, apiTokenScopePickerContent([]string{"data:read"}, apiTokenScopePickerConfig{}))
	for _, want := range []string{
		"Schemas", "Data", "Documents", "Graph", "Branches",
		"Agents", "Projects", "Journal", "Skills",
		`name="scopes"`, `value="schema:read"`, `value="schema:write"`,
		`value="data:read"`, `value="data:write"`, `value="agents:read"`,
		`value="chat:use"`, `value="graph:read"`, `value="schema:migrate"`,
		`value="search"`, `value="documents:read"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("scope picker missing %q", want)
		}
	}
	if strings.Contains(html, `value="admin"`) || strings.Contains(html, `value="admin:all"`) {
		t.Error("admin and admin:all must never be offered in the scope picker")
	}
	if strings.Contains(html, ">Admin<") {
		t.Error("the Admin area group must not render")
	}
	if strings.Contains(html, "Coarse-grained") || strings.Contains(html, "Fine-grained") {
		t.Error("the picker must no longer bucket scopes as coarse/fine-grained")
	}
	// preselected scopes render checked
	if !strings.Contains(html, `value="data:read" class="checkbox checkbox-sm mt-0.5" checked`) {
		t.Error("a granted scope should render checked")
	}
}

// TestScopePickerGroupsCoverTaxonomy asserts the area picker groups follow the
// canonical order (excluding the Admin area) and offer every token scope
// exactly once except the platform admin scopes (admin, admin:all) and the
// gated project:admin.
func TestScopePickerGroupsCoverTaxonomy(t *testing.T) {
	if len(scopePickerGroups) != len(apiTokenAreas)-1 {
		t.Fatalf("picker groups = %d, want %d (one per non-admin area)", len(scopePickerGroups), len(apiTokenAreas)-1)
	}
	for _, g := range scopePickerGroups {
		if g.Label == apiTokenAreaAdmin {
			t.Fatalf("picker must not offer the Admin area (platform admin scopes are not selectable)")
		}
	}
	seen := map[string]int{}
	for i, g := range scopePickerGroups {
		if g.Label != apiTokenAreas[i] {
			t.Errorf("group[%d].Label = %q, want %q (canonical order)", i, g.Label, apiTokenAreas[i])
		}
		if g.Hint == "" {
			t.Errorf("group[%d] (%s) needs a hint", i, g.Label)
		}
		for _, opt := range g.Options {
			seen[opt.Value]++
			if want := apiTokenScopeArea(opt.Value); want != g.Label {
				t.Errorf("option %q sits in group %q, want %q", opt.Value, g.Label, want)
			}
		}
	}
	for _, scope := range apiTokenScopes {
		if scope == "admin" || scope == "admin:all" || scope == "project:admin" {
			continue
		}
		if seen[scope] != 1 {
			t.Errorf("scope %q offered %d times in the picker, want exactly 1", scope, seen[scope])
		}
	}
	for scope := range seen {
		if !validAPITokenScope(scope) {
			t.Errorf("picker offers unknown scope %q", scope)
		}
	}
}

// TestScopePickerProjectAdminGating asserts project:admin is offered only for
// project tokens held by an admin caller (project_admin or owning org_admin),
// never for account tokens or non-admin project callers — and that the platform
// admin scopes (admin, admin:all) are never offered for any surface or caller.
func TestScopePickerProjectAdminGating(t *testing.T) {
	offered := func(cfg apiTokenScopePickerConfig) bool {
		html := renderHTML(t, apiTokenScopePickerContent(nil, cfg))
		return strings.Contains(html, `value="project:admin"`)
	}
	if !offered(apiTokenScopePickerConfig{CanManage: true}) {
		t.Error("project admin caller should be offered project:admin")
	}
	if offered(apiTokenScopePickerConfig{}) {
		t.Error("non-admin project caller must not be offered project:admin")
	}
	if offered(apiTokenScopePickerConfig{Account: true}) {
		t.Error("account tokens must never offer project:admin")
	}
	if offered(apiTokenScopePickerConfig{Account: true, CanManage: true}) {
		t.Error("account tokens must never offer project:admin even for an admin caller")
	}
	// platform admin scopes are never offered, for any surface or caller
	for _, cfg := range []apiTokenScopePickerConfig{
		{},
		{CanManage: true},
		{Account: true},
		{Account: true, CanManage: true},
	} {
		html := renderHTML(t, apiTokenScopePickerContent(nil, cfg))
		if strings.Contains(html, `value="admin"`) || strings.Contains(html, `value="admin:all"`) {
			t.Errorf("picker must never offer platform admin scopes, cfg=%+v", cfg)
		}
	}
	// the offered option sits in the Projects bucket
	groups := scopePickerGroupsFor(apiTokenScopePickerConfig{CanManage: true})
	found := false
	for _, g := range groups {
		if g.Label != apiTokenAreaProjects {
			continue
		}
		for _, o := range g.Options {
			if o.Value == "project:admin" {
				found = true
			}
		}
	}
	if !found {
		t.Error("project:admin must be offered in the Projects bucket")
	}
}

// TestRenderAPITokenRevealPanel asserts the one-shot plaintext panel (shown on
// create / regenerate) shows the secret exactly once with a copy affordance.
func TestRenderAPITokenRevealPanel(t *testing.T) {
	r := apiTokenReveal{
		Heading:     "API token created",
		Name:        "CI deploy",
		Token:       "emt_secret_plaintext_1",
		DismissHref: "/settings/tokens",
	}
	html := renderHTML(t, apiTokenRevealContent(r))
	for _, want := range []string{
		"API token created", "CI deploy", "emt_secret_plaintext_1",
		`id="api-token-secret"`, `data-copy-target="#api-token-secret"`, "Copy",
		"shown only once", `href="/settings/tokens"`, "Done",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("plaintext panel missing %q", want)
		}
	}
	if got := strings.Count(html, "emt_secret_plaintext_1"); got != 1 {
		t.Errorf("plaintext occurrences = %d, want exactly 1", got)
	}
}

// TestRenderProfileSubNav asserts the three profile pages render their rail
// entry with aria-current exactly once and each item's icon/label.
func TestRenderProfileSubNav(t *testing.T) {
	cases := []struct{ active, href string }{
		{"profile", "/profile"},
		{"invitations", "/profile/invitations"},
		{"tokens", "/profile/tokens"},
	}
	for _, tc := range cases {
		html := renderHTML(t, profileSubNav(tc.active))
		if !strings.Contains(html, `href="`+tc.href+`" aria-current="page"`) {
			t.Errorf("profileSubNav(%q) must mark %q active", tc.active, tc.href)
		}
		if got := strings.Count(html, "aria-current=\"page\""); got != 1 {
			t.Errorf("profileSubNav(%q) active items = %d, want 1", tc.active, got)
		}
		for _, want := range []string{"Profile", "Invitations", "API tokens"} {
			if !strings.Contains(html, want) {
				t.Errorf("profileSubNav(%q) missing label %q", tc.active, want)
			}
		}
	}
}
