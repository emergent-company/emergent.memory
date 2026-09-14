package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- route wiring + helpers ---

// apiTokensTestServer registers the project token routes, the profile pages,
// and the account (profile token) routes against the given server.
func apiTokensTestServer(s *Server) *echo.Echo {
	e := echo.New()
	// project tokens
	e.GET("/settings/tokens", s.uiAPITokens)
	e.GET("/settings/tokens/new", s.uiAPITokensNewPage)
	e.POST("/settings/tokens/new", s.uiAPITokensCreate)
	e.GET("/settings/tokens/:tokenId/edit", s.uiAPITokensEditPage)
	e.POST("/settings/tokens/:tokenId/revoke", s.uiAPITokensRevoke)
	e.POST("/settings/tokens/:tokenId/scopes", s.uiAPITokensScopes)
	e.POST("/settings/tokens/:tokenId/regenerate", s.uiAPITokensRegenerate)
	// profile area
	e.GET("/profile", s.uiProfile)
	e.GET("/profile/invitations", s.uiProfileInvitations)
	e.POST("/profile", s.uiUpdateProfile)
	e.POST("/invites/:id/accept", s.uiAcceptInvite)
	e.POST("/invites/:id/decline", s.uiDeclineInvite)
	// account tokens
	e.GET("/profile/tokens", s.uiProfileTokens)
	e.GET("/profile/tokens/new", s.uiProfileTokensNewPage)
	e.POST("/profile/tokens/new", s.uiProfileAPITokensCreate)
	e.GET("/profile/tokens/:tokenId/edit", s.uiProfileAPITokensEditPage)
	e.POST("/profile/tokens/:tokenId/revoke", s.uiProfileAPITokensRevoke)
	e.POST("/profile/tokens/:tokenId/scopes", s.uiProfileAPITokensScopes)
	e.POST("/profile/tokens/:tokenId/regenerate", s.uiProfileAPITokensRegenerate)
	return e
}

func apiTokenPost(e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	return rec
}

func apiTokenGet(e *echo.Echo, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// apiTokenStore returns a fakeMemory seeded with one live project token and one
// live account token (secrets registered for later regenerate flows).
func apiTokenStore() *fakeMemory {
	proj := APIToken{
		ID: "t1", Name: "CI deploy", TokenPrefix: "emt_cideployx",
		Scopes: []string{"data:read", "data:write"}, CreatedAt: fakeAPITokenCreatedAt,
	}
	acct := APIToken{
		ID: "a1", Name: "personal script", TokenPrefix: "emt_personalx",
		Scopes: []string{"search"}, CreatedAt: fakeAPITokenCreatedAt,
	}
	return &fakeMemory{
		apiTokens:        []APIToken{proj},
		apiTokenSecrets:  map[string]string{"t1": fakeAPITokenSecret("CI deploy", "t1"), "a1": fakeAPITokenSecret("personal script", "a1")},
		accountAPITokens: []APIToken{acct},
		profile:          &UserProfileDto{ID: "u1", FirstName: "Ada", DisplayName: "Ada"},
	}
}

// --- project page: GET list / empty / load error ---

func TestUIAPITokensListRoute(t *testing.T) {
	f := apiTokenStore()
	// add a revoked token to assert the revoked state renders
	f.apiTokens = append(f.apiTokens, APIToken{
		ID: "t2", Name: "old token", TokenPrefix: "emt_oldtoken",
		Scopes: []string{"data:read"}, CreatedAt: fakeAPITokenCreatedAt, IsRevoked: true,
	})
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenGet(e, "/settings/tokens")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings/tokens = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"API Tokens", "CI deploy", "old token", "emt_cideployx", "emt_oldtoken",
		"data:read", "data:write", "Revoked",
		// no inline create form; create moved to /new
		`href="/settings/tokens/new"`, "New token",
		`action="/settings/tokens/t1/revoke"`,
		`action="/settings/tokens/t1/regenerate"`, `href="/settings/tokens/t1/edit"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /settings/tokens missing %q", want)
		}
	}
	if strings.Contains(body, `action="/settings/tokens/new"`) {
		t.Error("list page must not embed the create form (it moved to /settings/tokens/new)")
	}
	// no plaintext in the list, ever
	secret := fakeAPITokenSecret("CI deploy", "t1")
	if strings.Contains(body, secret) {
		t.Error("list page must never contain a plaintext token value")
	}
	// revoked tokens offer no destructive actions
	if strings.Contains(body, `action="/settings/tokens/t2/revoke"`) || strings.Contains(body, `href="/settings/tokens/t2/edit"`) {
		t.Error("revoked tokens must not offer destructive actions or scope editing")
	}
}

func TestUIAPITokensEmptyRoute(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)
	rec := apiTokenGet(e, "/settings/tokens")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"No API tokens yet", `href="/settings/tokens/new"`, "New token"} {
		if !strings.Contains(body, want) {
			t.Errorf("empty route missing %q", want)
		}
	}
	if strings.Contains(body, `action="/settings/tokens/new"`) {
		t.Error("empty list must not embed the create form")
	}
}

func TestUIAPITokensLoadErrorRoute(t *testing.T) {
	f := &fakeMemory{apiTokenErr: errTest}
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)
	rec := apiTokenGet(e, "/settings/tokens")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "API tokens unavailable") {
		t.Error("load-error state missing")
	}
}

// --- project: create page + create flow ---

func TestUIAPITokensNewPageRoute(t *testing.T) {
	s := &Server{cfg: Config{}, memory: apiTokenStore()}
	e := apiTokensTestServer(s)

	rec := apiTokenGet(e, "/settings/tokens/new")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings/tokens/new = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"New token", `action="/settings/tokens/new"`, `name="name"`,
		`name="scopes"`, `value="data:read"`, "Create token", `href="/settings/tokens"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /settings/tokens/new missing %q", want)
		}
	}
	if strings.Contains(body, "api-token-secret") {
		t.Error("fresh create page must not carry a plaintext panel")
	}
}

func TestUIAPITokensCreateShowsPlaintextOnce(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/settings/tokens/new", "name=ci&scopes=data:read&scopes=data:write")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /settings/tokens/new = %d, want 200 (render once, no redirect)", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("create must not redirect, Location = %q", loc)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "API token created") {
		t.Error("create plaintext panel missing")
	}
	// the create page re-renders with the plaintext panel (secret exactly once)
	secret := fakeAPITokenSecret("ci", "tok-1") // seq starts at 1 (store was seeded directly)
	if got := strings.Count(body, secret); got != 1 {
		t.Errorf("plaintext secret occurrences = %d, want exactly 1", got)
	}
	if !strings.Contains(body, `href="/settings/tokens"`) {
		t.Error("plaintext panel dismiss link must return to the token list")
	}
	// the created token is in the store and shows on the subsequent list GET
	rec = apiTokenGet(e, "/settings/tokens")
	if !strings.Contains(rec.Body.String(), ">ci<") {
		t.Error("created token name missing from the list after create")
	}
}

func TestUIAPITokensCreateValidationErrors(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/settings/tokens/new", "name=&scopes=data:read")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/settings/tokens/new?err=token+name+is+required") {
		t.Errorf("empty name = %d %q, want name-required redirect back to /new", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.apiTokens) != 1 {
		t.Error("empty-name create must not create a token")
	}

	rec = apiTokenPost(e, "/settings/tokens/new", "name=ci&scopes=")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/settings/tokens/new?err=select+at+least+one+scope") {
		t.Errorf("no scopes = %d %q, want at-least-one-scope redirect back to /new", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.apiTokens) != 1 {
		t.Error("no-scopes create must not create a token")
	}

	rec = apiTokenPost(e, "/settings/tokens/new", "name=ci&scopes=bogus:read")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "unsupported+scope") {
		t.Errorf("unsupported scope = %d %q, want unsupported-scope redirect", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.apiTokens) != 1 {
		t.Error("unsupported-scope create must not create a token")
	}
}

func TestUIAPITokensCreateDuplicateName(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	// the store already holds a token named "CI deploy" → 409 token_name_exists
	rec := apiTokenPost(e, "/settings/tokens/new", "name=CI+deploy&scopes=data:read")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("duplicate create = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "/settings/tokens/new?err=") || !strings.Contains(loc, "token_name_exists") {
		t.Errorf("duplicate create Location = %q, want token_name_exists surfaced on /new", loc)
	}
}

func TestUIAPITokensCreateForbidden(t *testing.T) {
	f := apiTokenStore()
	f.apiTokenErr = fmt.Errorf("memory 403 viewer-write-scope-denied: project viewers cannot create tokens with write scopes")
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/settings/tokens/new", "name=ci&scopes=data:write")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("forbidden create = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "/settings/tokens/new?err=") || !strings.Contains(loc, "viewer-write-scope-denied") {
		t.Errorf("forbidden create Location = %q, want 403 surfaced", loc)
	}
}

// --- project: edit page (pre-checked scopes) + scope save ---

func TestUIAPITokensEditPageRoute(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenGet(e, "/settings/tokens/t1/edit")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings/tokens/t1/edit = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Edit scopes", "CI deploy", `action="/settings/tokens/t1/scopes"`,
		"Save scopes", `href="/settings/tokens"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
	// the token's current scopes are pre-checked
	for _, scope := range []string{"data:read", "data:write"} {
		if !strings.Contains(body, `value="`+scope+`" class="checkbox checkbox-sm mt-0.5" checked`) {
			t.Errorf("scope %q should render pre-checked on the edit page", scope)
		}
	}
	if !strings.Contains(body, `value="search" class="checkbox checkbox-sm mt-0.5"`) {
		t.Error("unrelated scope should render unchecked")
	}
}

func TestUIAPITokensEditPageNotFound(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenGet(e, "/settings/tokens/nope/edit")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Token not found") {
		t.Errorf("unknown token edit GET = %d, want the not-found state", rec.Code)
	}
}

func TestUIAPITokensScopesEditPersists(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/settings/tokens/t1/scopes", "scopes=data:write&scopes=graph:read")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/tokens?updated=1" {
		t.Fatalf("scopes = %d %q, want 303 /settings/tokens?updated=1", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastAPITokenID != "t1" {
		t.Errorf("updated id = %q, want t1", f.lastAPITokenID)
	}
	if len(f.lastAPITokenScopes) != 2 || f.lastAPITokenScopes[0] != "data:write" || f.lastAPITokenScopes[1] != "graph:read" {
		t.Errorf("updated scopes = %v", f.lastAPITokenScopes)
	}
	// the list re-render reflects the new scopes
	rec = apiTokenGet(e, "/settings/tokens")
	body := rec.Body.String()
	if !strings.Contains(body, "graph:read") {
		t.Error("edited scopes missing from the relist")
	}
	if !strings.Contains(f.apiTokens[0].Scopes[0], "data:write") {
		t.Errorf("store scopes = %v, want data:write first", f.apiTokens[0].Scopes)
	}
}

func TestUIAPITokensScopesEditEmpty(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/settings/tokens/t1/scopes", "")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "err=select+at+least+one+scope") {
		t.Errorf("empty scopes = %d %q, want validation redirect", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIAPITokensScopesEditErrorRedirectsToEditPage(t *testing.T) {
	f := apiTokenStore()
	f.apiTokenErr = fmt.Errorf("memory 403 viewer-write-scope-denied")
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/settings/tokens/t1/scopes", "scopes=data:write")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("scopes-save failure = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/settings/tokens/t1/edit?err=") || !strings.Contains(loc, "viewer-write-scope-denied") {
		t.Errorf("scopes-save failure Location = %q, want back to the edit page with the error", loc)
	}
}

// --- project: regenerate / revoke render the list page ---

func TestUIAPITokensRegenerateRendersNewToken(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/settings/tokens/t1/regenerate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("regenerate = %d, want 200 (render once)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "API token regenerated") {
		t.Error("regenerate panel missing")
	}
	// t1 is revoked and a replacement tok-1 with the same name/scopes exists
	oldSecret := fakeAPITokenSecret("CI deploy", "t1")
	newSecret := fakeAPITokenSecret("CI deploy", "tok-1")
	if got := strings.Count(body, newSecret); got != 1 {
		t.Errorf("new plaintext occurrences = %d, want exactly 1", got)
	}
	if strings.Contains(body, oldSecret) {
		t.Error("old secret must never appear after regeneration")
	}
	if !f.apiTokens[0].IsRevoked {
		t.Error("regenerate must revoke the old token in the store")
	}
	if len(f.apiTokens) != 2 || f.apiTokens[1].Name != "CI deploy" {
		t.Errorf("store = %+v, want old revoked + replacement", f.apiTokens)
	}
}

func TestUIAPITokensRegenerateRejected(t *testing.T) {
	f := apiTokenStore()
	f.apiTokens[0].IsRevoked = true // already revoked → 409
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/settings/tokens/t1/regenerate", "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("regenerate-rejected = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "token_already_revoked") {
		t.Errorf("regenerate-rejected Location = %q, want 409 surfaced", loc)
	}
	if len(f.apiTokens) != 1 {
		t.Error("rejected regenerate must not create a replacement")
	}
}

func TestUIAPITokensRevokeMarksRevoked(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/settings/tokens/t1/revoke", "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/tokens?updated=1" {
		t.Fatalf("revoke = %d %q, want 303 /settings/tokens?updated=1", rec.Code, rec.Header().Get("Location"))
	}
	if !f.apiTokens[0].IsRevoked {
		t.Fatal("revoke must mark the store token revoked")
	}
	// the next list render shows the revoked state
	rec = apiTokenGet(e, "/settings/tokens")
	if !strings.Contains(rec.Body.String(), "Revoked") {
		t.Error("revoked state missing after revoke")
	}
}

func TestUIAPITokensRevokeRejected(t *testing.T) {
	f := apiTokenStore()
	f.apiTokens[0].IsRevoked = true
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/settings/tokens/t1/revoke", "")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "token_already_revoked") {
		t.Errorf("revoke-rejected = %d %q, want 409 surfaced", rec.Code, rec.Header().Get("Location"))
	}
}

// --- profile split: profile page (no tokens/invites) + separate pages ---

func TestUIProfilePageHasNoTokensOrInvitations(t *testing.T) {
	f := apiTokenStore()
	f.pendingInvites = []PendingInviteDto{{
		ID: "pinv-1", OrganizationName: "Acme", Role: "project_admin", Token: "tok-abc", CreatedAt: "2026-08-19T09:00:00Z",
	}}
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenGet(e, "/profile")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profile = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Profile", "Ada", `action="/profile"`, `href="/profile/tokens"`, `href="/profile/invitations"`} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /profile missing %q", want)
		}
	}
	if strings.Contains(body, "personal script") || strings.Contains(body, `action="/profile/tokens/a1/revoke"`) {
		t.Error("profile page must not render the account-token section (moved to /profile/tokens)")
	}
	if strings.Contains(body, "pendingInviteRow") || strings.Contains(body, "Acme") {
		t.Error("profile page must not render pending invitations (moved to /profile/invitations)")
	}
}

func TestUIProfileInvitationsRoute(t *testing.T) {
	f := apiTokenStore()
	f.pendingInvites = []PendingInviteDto{{
		ID: "pinv-1", ProjectID: "p1", ProjectName: "Home",
		OrganizationID: "o1", OrganizationName: "Acme",
		Role: "project_admin", Token: "tok-abc", CreatedAt: "2026-08-19T09:00:00Z",
	}}
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenGet(e, "/profile/invitations")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profile/invitations = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Invitations", "Acme", "Home", "project admin",
		`action="/invites/pinv-1/accept"`, `name="token" value="tok-abc"`,
		`action="/invites/pinv-1/decline"`, "Accept", "Decline",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /profile/invitations missing %q", want)
		}
	}
	if strings.Contains(body, "personal script") {
		t.Error("invitations page must not render account tokens")
	}

	// accept / decline PRG back to the invitations page with a flash
	rec = apiTokenPost(e, "/invites/pinv-1/accept", "token=tok-abc")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/profile/invitations?accepted=1" {
		t.Errorf("accept = %d %q, want 303 /profile/invitations?accepted=1", rec.Code, rec.Header().Get("Location"))
	}
	rec = apiTokenPost(e, "/invites/pinv-1/decline", "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/profile/invitations?declined=1" {
		t.Errorf("decline = %d %q, want 303 /profile/invitations?declined=1", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIProfileTokensListRoute(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenGet(e, "/profile/tokens")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profile/tokens = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"API tokens", "personal script", "emt_personalx", "search",
		`href="/profile/tokens/new"`, "New token",
		`action="/profile/tokens/a1/revoke"`,
		`action="/profile/tokens/a1/regenerate"`, `href="/profile/tokens/a1/edit"`,
		// profile rail present, token page active
		`href="/profile"`, `href="/profile/invitations"`, `href="/profile/tokens"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /profile/tokens missing %q", want)
		}
	}
	if strings.Contains(body, "CI deploy") {
		t.Error("account token list must not show project tokens")
	}
}

func TestUIProfileTokensNewPageRoute(t *testing.T) {
	s := &Server{cfg: Config{}, memory: apiTokenStore()}
	e := apiTokensTestServer(s)

	rec := apiTokenGet(e, "/profile/tokens/new")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profile/tokens/new = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"New token", `action="/profile/tokens/new"`, `name="name"`, `name="scopes"`, "Create token",
		`href="/profile"`, `href="/profile/invitations"`, `href="/profile/tokens"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /profile/tokens/new missing %q", want)
		}
	}
}

func TestUIProfileTokensEditPageRoute(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenGet(e, "/profile/tokens/a1/edit")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profile/tokens/a1/edit = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Edit scopes", "personal script", `action="/profile/tokens/a1/scopes"`,
		`value="search" class="checkbox checkbox-sm mt-0.5" checked`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("account edit page missing %q", want)
		}
	}
}

// --- account tokens: create / regenerate / revoke / scopes ---

func TestUIProfileAPITokensCreateShowsPlaintextOnce(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/profile/tokens/new", "name=acct2&scopes=search&scopes=graph:read")
	if rec.Code != http.StatusOK {
		t.Fatalf("account create = %d, want 200 (render once, no redirect)", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("account create must not redirect, Location = %q", loc)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Account token created") {
		t.Error("account create plaintext panel missing")
	}
	secret := fakeAPITokenSecret("acct2", "acct-1") // seq starts at 1 (store was seeded directly)
	if got := strings.Count(body, secret); got != 1 {
		t.Errorf("account plaintext occurrences = %d, want exactly 1", got)
	}
}

func TestUIProfileAPITokensCreateValidation(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/profile/tokens/new", "name=&scopes=search")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/profile/tokens/new?err=token+name+is+required") {
		t.Errorf("empty name = %d %q, want redirect back to /profile/tokens/new", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.accountAPITokens) != 1 {
		t.Error("empty-name account create must not create a token")
	}

	rec = apiTokenPost(e, "/profile/tokens/new", "name=acct2&scopes=")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/profile/tokens/new?err=select+at+least+one+scope") {
		t.Errorf("no scopes = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIProfileAPITokensDuplicateName(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/profile/tokens/new", "name=personal+script&scopes=search")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "token_name_exists") {
		t.Errorf("duplicate account create = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIProfileAPITokensForbidden(t *testing.T) {
	f := apiTokenStore()
	f.accountAPITokenErr = fmt.Errorf("memory 403 admin-all-scope-denied: only org admins may hold admin:all")
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/profile/tokens/new", "name=acct2&scopes=admin:all")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "admin-all-scope-denied") {
		t.Errorf("forbidden account create = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIProfileAPITokensRegenerate(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/profile/tokens/a1/regenerate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("account regenerate = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Account token regenerated") {
		t.Error("account regenerate panel missing")
	}
	newSecret := fakeAPITokenSecret("personal script", "acct-1")
	if got := strings.Count(body, newSecret); got != 1 {
		t.Errorf("account new plaintext occurrences = %d, want exactly 1", got)
	}
	if !f.accountAPITokens[0].IsRevoked {
		t.Error("account regenerate must revoke the old token")
	}
}

func TestUIProfileAPITokensRevoke(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/profile/tokens/a1/revoke", "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/profile/tokens?updated=1" {
		t.Fatalf("account revoke = %d %q, want 303 /profile/tokens?updated=1", rec.Code, rec.Header().Get("Location"))
	}
	if !f.accountAPITokens[0].IsRevoked {
		t.Fatal("account revoke must mark the store token revoked")
	}
	rec = apiTokenGet(e, "/profile/tokens")
	if !strings.Contains(rec.Body.String(), "Revoked") {
		t.Error("revoked state missing on the account tokens relist")
	}
}

func TestUIProfileAPITokensScopesEdit(t *testing.T) {
	f := apiTokenStore()
	s := &Server{cfg: Config{}, memory: f}
	e := apiTokensTestServer(s)

	rec := apiTokenPost(e, "/profile/tokens/a1/scopes", "scopes=search&scopes=journal:read")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/profile/tokens?updated=1" {
		t.Fatalf("account scopes = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastAccountAPITokenID != "a1" || len(f.lastAccountAPITokenScopes) != 2 || f.lastAccountAPITokenScopes[1] != "journal:read" {
		t.Errorf("account scopes edit = %s %v", f.lastAccountAPITokenID, f.lastAccountAPITokenScopes)
	}

	// save failure returns to the account edit page with the error
	f2 := apiTokenStore()
	f2.accountAPITokenErr = fmt.Errorf("memory 403 admin-all-scope-denied")
	s2 := &Server{cfg: Config{}, memory: f2}
	e2 := apiTokensTestServer(s2)
	rec = apiTokenPost(e2, "/profile/tokens/a1/scopes", "scopes=admin")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/profile/tokens/a1/edit?err=") {
		t.Errorf("account scopes failure = %d %q, want back to the edit page", rec.Code, rec.Header().Get("Location"))
	}
}

// --- unauthenticated redirects (session mode) ---

func TestUIAPITokensUnauthenticatedRedirect(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: apiTokenStore()}
	e := echo.New()
	e.Use(s.authDispatch)
	// public auth route so the redirect target resolves (the assertion is the
	// redirect itself, not the login page render)
	e.GET("/auth/login", s.authLogin)
	te := apiTokensTestServer(s)
	for _, r := range te.Routes() {
		method, path := r.Method, r.Path
		e.Add(method, path, func(c echo.Context) error {
			// no-op handler; authDispatch decides access before it runs
			return c.String(http.StatusOK, "ok")
		})
	}

	requests := []struct{ method, path string }{
		{http.MethodGet, "/settings/tokens"},
		{http.MethodGet, "/settings/tokens/new"},
		{http.MethodPost, "/settings/tokens/new"},
		{http.MethodGet, "/settings/tokens/t1/edit"},
		{http.MethodPost, "/settings/tokens/t1/revoke"},
		{http.MethodPost, "/settings/tokens/t1/scopes"},
		{http.MethodGet, "/profile"},
		{http.MethodGet, "/profile/invitations"},
		{http.MethodGet, "/profile/tokens"},
		{http.MethodGet, "/profile/tokens/new"},
		{http.MethodPost, "/profile/tokens/new"},
		{http.MethodGet, "/profile/tokens/a1/edit"},
		{http.MethodPost, "/profile/tokens/a1/revoke"},
	}
	for _, r := range requests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(r.method, r.path, nil)
		if r.method == http.MethodPost {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/auth/login" {
			t.Errorf("%s %s without a session = %d (Location %q), want 302 /auth/login", r.method, r.path, rec.Code, rec.Header().Get("Location"))
		}
	}
}
