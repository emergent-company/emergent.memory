package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

type fakeOAuthBackend struct {
	MemoryBackend
	ctx     *OAuthConsentContext
	ctxErr  error
	auth    OAuthAuthorizeRequest
	res     OAuthAuthorizeResult
	authErr error
	called  bool
}

func (f *fakeOAuthBackend) OAuthAuthorizeContext(ctx context.Context, clientID, redirectURI string) (*OAuthConsentContext, error) {
	if f.ctxErr != nil {
		return nil, f.ctxErr
	}
	return f.ctx, nil
}

func (f *fakeOAuthBackend) OAuthAuthorize(ctx context.Context, in OAuthAuthorizeRequest) (*OAuthAuthorizeResult, error) {
	f.called = true
	f.auth = in
	if f.authErr != nil {
		return nil, f.authErr
	}
	return &f.res, nil
}

func (f *fakeOAuthBackend) ListOAuthGrants(ctx context.Context) ([]OAuthGrant, error) {
	return nil, nil
}

func (f *fakeOAuthBackend) RevokeOAuthGrant(ctx context.Context, id string) error { return nil }

func TestSafeLocalPath(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"/oauth/authorize?client_id=x", "/oauth/authorize?client_id=x"},
		{"/settings/connected-apps", "/settings/connected-apps"},
		{"", ""},
		{"https://evil.example/", ""},
		{"//evil.example/", ""},
		{"/%2f%2fevil.example/", ""},
		{"/\\evil.example/", ""},
		{"/\t/evil.example/", ""},
		{"/\r\n/evil.example/", ""},
		{"/%0a/evil.example/", ""},
		{"/\x7f/evil.example/", ""},
		{"/auth/login", ""},
		{"/api/graph/objects", ""},
	}
	for _, tt := range tests {
		if got := safeLocalPath(tt.in); got != tt.want {
			t.Errorf("safeLocalPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLoginPagePreservesReturnTo(t *testing.T) {
	html := renderHTML(t, loginPage("", "development", 0, 0, 0, "/oauth/authorize?client_id=x&state=y"))
	want := `href="/auth/start?return_to=%2Foauth%2Fauthorize%3Fclient_id%3Dx%26state%3Dy"`
	if !strings.Contains(html, want) {
		t.Fatalf("login page missing %q", want)
	}
}

func TestOAuthConsentPageRendersRequest(t *testing.T) {
	html := renderHTML(t, OAuthConsentPage(oauthConsentData{
		ClientID:            "client-1",
		ClientName:          "emergent.feedback",
		RedirectURI:         "https://feedback.example/oauth/memory/callback",
		State:               "state-1",
		CodeChallenge:       "challenge-1",
		CodeChallengeMethod: "S256",
		Scope:               "graph:read graph:write",
		Scopes:              []string{"graph:read", "graph:write"},
		Projects:            []OAuthProject{{ID: "p1", Name: "Project One", Slug: "project-one"}},
	}))
	for _, want := range []string{
		"emergent.feedback",
		"Project One",
		`name="project_id"`,
		`name="code_challenge" value="challenge-1"`,
		`name="state" value="state-1"`,
		"Authorize emergent.feedback",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("consent page missing %q", want)
		}
	}
}

func newOAuthDecisionServer(f *fakeOAuthBackend) *Server {
	return &Server{memory: f}
}

func postConsent(t *testing.T, s *Server, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	e.POST("/oauth/authorize", s.uiOAuthAuthorizeDecision)
	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestClampScopes(t *testing.T) {
	supported := []string{"graph:read", "graph:write", "projects:read"}
	tests := []struct {
		name string
		want []string
		in   []string
		out  []string
	}{
		{name: "keeps supported order", in: []string{"graph:write", "graph:read"}, out: []string{"graph:write", "graph:read"}},
		{name: "drops unknown", in: []string{"offline_access", "graph:read", "ACCESS_VIEW_MANAGE_MCP_CONTENT"}, out: []string{"graph:read"}},
		{name: "deduplicates", in: []string{"graph:read", "graph:read"}, out: []string{"graph:read"}},
		{name: "all unknown", in: []string{"offline_access"}, out: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampScopes(tt.in, supported)
			if strings.Join(got, " ") != strings.Join(tt.out, " ") {
				t.Fatalf("clampScopes(%v) = %v, want %v", tt.in, got, tt.out)
			}
		})
	}
}

// A client that asks for scopes this server does not implement must still get
// a working consent flow with the supported subset (OAuth 2.1 permits a partial
// grant); rejecting the request breaks clients like Gemini's connector.
func TestOAuthConsentClampsUnsupportedScope(t *testing.T) {
	f := &fakeOAuthBackend{
		ctx: &OAuthConsentContext{
			ClientID:        "client-1",
			ClientName:      "gemini",
			RedirectURI:     "https://oauth-redirect.googleusercontent.com/r/project-1",
			ScopesSupported: []string{"graph:read", "graph:write", "schema:read", "projects:read", "chat:use"},
		},
		res: OAuthAuthorizeResult{RedirectURL: "https://oauth-redirect.googleusercontent.com/r/project-1?code=abc"},
	}
	rec := postConsent(t, newOAuthDecisionServer(f), url.Values{
		"client_id":      {"client-1"},
		"redirect_uri":   {"https://oauth-redirect.googleusercontent.com/r/project-1"},
		"state":          {"state-1"},
		"scope":          {"graph:read offline_access ACCESS_VIEW_MANAGE_MCP_CONTENT"},
		"project_id":     {"p1"},
		"code_challenge": {"challenge-1"}, "code_challenge_method": {"S256"},
		"decision": {"approve"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if !f.called {
		t.Fatal("consent must authorize with the clamped scope")
	}
	if got := strings.Join(f.auth.Scopes, " "); got != "graph:read" {
		t.Fatalf("scopes = %q, want %q", got, "graph:read")
	}
}

func TestOAuthConsentDenyRedirectsWithError(t *testing.T) {
	f := &fakeOAuthBackend{ctx: &OAuthConsentContext{
		ClientID: "client-1", ClientName: "app", RedirectURI: "https://client.example/cb",
	}}
	rec := postConsent(t, newOAuthDecisionServer(f), url.Values{
		"client_id": {"client-1"}, "redirect_uri": {"https://client.example/cb"},
		"state": {"state-1"}, "decision": {"deny"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Host != "client.example" || loc.Query().Get("error") != "access_denied" || loc.Query().Get("state") != "state-1" {
		t.Fatalf("location = %s", loc)
	}
	if f.called {
		t.Fatal("deny must not create a grant")
	}
}

func TestOAuthConsentApproveRedirectsWithCode(t *testing.T) {
	f := &fakeOAuthBackend{
		ctx: &OAuthConsentContext{
			ClientID: "client-1", ClientName: "app", RedirectURI: "https://client.example/cb",
			ScopesSupported: []string{"graph:read", "graph:write"},
		},
		res: OAuthAuthorizeResult{RedirectURL: "https://client.example/cb?code=abc&state=state-1"},
	}
	rec := postConsent(t, newOAuthDecisionServer(f), url.Values{
		"client_id": {"client-1"}, "redirect_uri": {"https://client.example/cb"},
		"state": {"state-1"}, "scope": {"graph:read graph:write"}, "project_id": {"p1"},
		"code_challenge": {"challenge-1"}, "code_challenge_method": {"S256"}, "decision": {"approve"},
	})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://client.example/cb?code=abc&state=state-1" {
		t.Fatalf("status=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if !f.called || f.auth.ProjectID != "p1" || strings.Join(f.auth.Scopes, " ") != "graph:read graph:write" {
		t.Fatalf("authorize request = %+v", f.auth)
	}
}

func TestOAuthConsentRejectsRedirectTampering(t *testing.T) {
	f := &fakeOAuthBackend{ctxErr: errors.New("redirect_uri does not match")}
	rec := postConsent(t, newOAuthDecisionServer(f), url.Values{
		"client_id": {"client-1"}, "redirect_uri": {"https://evil.example/cb"},
		"decision": {"approve"}, "project_id": {"p1"}, "code_challenge": {"c"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if f.called {
		t.Fatal("invalid redirect must not authorize")
	}
}
