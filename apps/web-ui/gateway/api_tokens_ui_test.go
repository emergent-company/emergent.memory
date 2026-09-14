package main

import (
	"strings"
	"testing"
	"time"
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
// (name/prefix/scopes/created/last used/actions + revoked state), the count
// badge, the "New token" action — and never any plaintext or inline form.
func TestRenderAPITokensPage(t *testing.T) {
	data := apiTokensPageData{Tokens: apiTokenRenderFixture()}
	html := renderHTML(t, APITokensPage(data))
	for _, want := range []string{
		"API Tokens", "CI deploy", "old token", "emt_cideployx", "emt_oldtoken",
		"data:read", "data:write", "schema:read", "Revoked", "2 tokens",
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
		"API tokens", "personal script", "emt_personalx", "search", "journal:read",
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
		// picker scopes
		`name="scopes"`, `value="data:read"`, `value="schema:write"`, `value="chat:use"`,
		`value="admin"`, "Coarse-grained", "Fine-grained",
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

// TestRenderScopePickerOmitsAdminAll asserts the picker offers every group and
// the admin scope but never admin:all (server-gated).
func TestRenderScopePickerOmitsAdminAll(t *testing.T) {
	html := renderHTML(t, apiTokenScopePickerContent([]string{"data:read", "admin"}))
	for _, want := range []string{
		"Coarse-grained", "Fine-grained", "Admin",
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
	// preselected scopes render checked
	if !strings.Contains(html, `value="data:read" class="checkbox checkbox-sm mt-0.5" checked`) {
		t.Error("a granted scope should render checked")
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
