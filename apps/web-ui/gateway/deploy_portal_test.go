package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func deployTestConfig(tokenURL, graphURL string) Config {
	return Config{
		AuthMode:                 "dev",
		DeployPortalEnabled:      true,
		RailwayOAuthClientID:     "client-1",
		RailwayOAuthClientSecret: "secret-1",
		RailwayOAuthRedirectURI:  "http://gateway.test/deploy/oauth/callback",
		RailwayOAuthAuthorizeURL: "https://railway.test/oauth/auth",
		RailwayOAuthTokenURL:     tokenURL,
		RailwayGraphQLURL:        graphURL,
		RailwayOAuthScopes:       []string{"openid", "workspace:admin"},
		DeploySessionSecret:      "01234567890123456789012345678901",
		PublicBaseURL:            "http://gateway.test",
	}
}

func deployTestEcho(s *Server) *echo.Echo {
	e := echo.New()
	e.GET("/deploy", s.deployPortalPage)
	e.GET("/deploy/oauth/start", s.deployOAuthStart)
	e.GET("/deploy/oauth/callback", s.deployOAuthCallback)
	e.GET("/deploy/oauth/logout", s.deployOAuthLogout)
	return e
}

func TestDeployPortalDisabledIsNotFound(t *testing.T) {
	s := &Server{cfg: Config{DeployPortalEnabled: false}}
	e := deployTestEcho(s)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/deploy", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeployOAuthStartBuildsAuthorizeURLAndStateCookie(t *testing.T) {
	s := &Server{cfg: deployTestConfig("https://railway.test/token", "https://railway.test/graphql")}
	e := deployTestEcho(s)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/deploy/oauth/start", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Host != "railway.test" || loc.Path != "/oauth/auth" {
		t.Fatalf("authorize URL = %s", loc)
	}
	q := loc.Query()
	for _, key := range []string{"client_id", "redirect_uri", "scope", "state", "code_challenge"} {
		if q.Get(key) == "" {
			t.Errorf("authorize URL missing %s", key)
		}
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q", q.Get("code_challenge_method"))
	}
	if q.Get("client_id") != "client-1" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == deployStateCookie && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Error("state cookie was not set")
	}
}

func TestDeployOAuthCallbackExchangesCodeAndListsWorkspaces(t *testing.T) {
	mux := http.NewServeMux()
	var gotTokenForm url.Values
	var gotGraphAuth string
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotTokenForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600,"token_type":"Bearer"}`))
	})
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		gotGraphAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"me":{"workspaces":[{"id":"ws-1","name":"Acme"}]}}}`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	s := &Server{cfg: deployTestConfig(ts.URL+"/token", ts.URL+"/graphql")}
	e := deployTestEcho(s)

	// Start to mint a state cookie and read the state value from the redirect.
	start := httptest.NewRecorder()
	e.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/deploy/oauth/start", nil))
	loc, _ := url.Parse(start.Header().Get("Location"))
	state := loc.Query().Get("state")
	var stateCookie *http.Cookie
	for _, c := range start.Result().Cookies() {
		if c.Name == deployStateCookie {
			stateCookie = c
		}
	}
	if stateCookie == nil {
		t.Fatal("no state cookie")
	}

	cb := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/deploy/oauth/callback?code=code-1&state="+url.QueryEscape(state), nil)
	req.AddCookie(stateCookie)
	e.ServeHTTP(cb, req)
	if cb.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want 302; body=%s", cb.Code, cb.Body.String())
	}
	if got := cb.Header().Get("Location"); got != "/deploy" {
		t.Fatalf("callback redirect = %q, want /deploy", got)
	}
	if gotTokenForm.Get("code") != "code-1" || gotTokenForm.Get("client_id") != "client-1" || gotTokenForm.Get("client_secret") != "secret-1" {
		t.Fatalf("token form = %v", gotTokenForm)
	}
	var sessionCookie *http.Cookie
	for _, c := range cb.Result().Cookies() {
		if c.Name == deploySessionCookie && c.Value != "" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("session cookie was not set")
	}

	page := httptest.NewRecorder()
	pageReq := httptest.NewRequest(http.MethodGet, "/deploy", nil)
	pageReq.AddCookie(sessionCookie)
	e.ServeHTTP(page, pageReq)
	if page.Code != http.StatusOK {
		t.Fatalf("page status = %d", page.Code)
	}
	if gotGraphAuth != "Bearer access-1" {
		t.Fatalf("graphql auth = %q", gotGraphAuth)
	}
	body := page.Body.String()
	if !strings.Contains(body, "Acme") || !strings.Contains(body, "Railway connected") {
		t.Logf("page status=%d body=%s", page.Code, body[:min(1200, len(body))])
		t.Errorf("page missing connected workspace")
	}
}

func TestDeployPortalPageSignedOut(t *testing.T) {
	s := &Server{cfg: deployTestConfig("https://railway.test/token", "https://railway.test/graphql")}
	e := deployTestEcho(s)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/deploy", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Connect Railway") || !strings.Contains(body, "Deploy Memory on Railway") {
		t.Errorf("signed-out page missing CTA: %s", body[:min(400, len(body))])
	}
}

func TestDeployPortalConfigValidation(t *testing.T) {
	base := deployTestConfig("https://railway.test/token", "https://railway.test/graphql")
	if err := base.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	missing := base
	missing.RailwayOAuthClientID = ""
	if err := missing.Validate(); err == nil {
		t.Fatal("missing client id must fail validation")
	}
	short := base
	short.DeploySessionSecret = "short"
	if err := short.Validate(); err == nil {
		t.Fatal("short deploy session secret must fail validation")
	}
}
