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
		`data-testid="token-scope-area"`, ">Data<", ">Schemas<",
		`title="data:read, data:write"`, `title="schema:read"`,
		"Revoked", "2 tokens",
		"New token", `href="/settings/tokens/new"`,
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
		"projects:read": apiTokenAreaProjects, "projects:write": apiTokenAreaProjects,
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
		`value="admin"`, "Schemas", "Data", "Graph", "Agents", "Admin",
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
// and the admin scope but never admin:all (server-gated).
func TestRenderScopePickerOmitsAdminAll(t *testing.T) {
	html := renderHTML(t, apiTokenScopePickerContent([]string{"data:read", "admin"}))
	for _, want := range []string{
		"Schemas", "Data", "Documents", "Graph", "Branches",
		"Agents", "Projects", "Journal", "Skills", "Admin",
		`name="scopes"`, `value="schema:read"`, `value="schema:write"`,
		`value="data:read"`, `value="data:write"`, `value="agents:read"`,
		`value="chat:use"`, `value="graph:read"`, `value="schema:migrate"`,
		`value="search"`, `value="documents:read"`, `value="admin"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("scope picker missing %q", want)
		}
	}
	if strings.Contains(html, `value="admin:all"`) {
		t.Error("admin:all must never be offered in the scope picker")
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
// canonical order and offer every token scope exactly once except admin:all
// (server-gated).
func TestScopePickerGroupsCoverTaxonomy(t *testing.T) {
	if len(scopePickerGroups) != len(apiTokenAreas) {
		t.Fatalf("picker groups = %d, want %d (one per area)", len(scopePickerGroups), len(apiTokenAreas))
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
		if scope == "admin:all" {
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
