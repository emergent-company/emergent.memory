package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGitHubAuthorizeURL_Defaults(t *testing.T) {
	strat := newGitHubOAuthStrategy()
	raw := strat.AuthorizeURL(OAuthClient{ClientID: "cid"}, OAuthAuthorizeRequest{
		State:         "state-1",
		CodeChallenge: "challenge-1",
		RedirectURI:   "https://app.example/callback",
	})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("authorize URL parse error = %v", err)
	}
	if u.Host != "github.com" {
		t.Errorf("host = %q, want github.com", u.Host)
	}
	q := u.Query()
	checks := map[string]string{
		"client_id":             "cid",
		"redirect_uri":          "https://app.example/callback",
		"response_type":         "code",
		"state":                 "state-1",
		"code_challenge":        "challenge-1",
		"code_challenge_method": "S256",
	}
	for k, want := range checks {
		if got := q.Get(k); got != want {
			t.Errorf("param %q = %q, want %q", k, got, want)
		}
	}
	if got := q.Get("scope"); !strings.Contains(got, githubScopeRepo) || !strings.Contains(got, githubScopeOrg) {
		t.Errorf("scope = %q, want to contain %q and %q", got, githubScopeRepo, githubScopeOrg)
	}
}

func TestGitHubExchange_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if r.Form.Get("code") != "code-1" {
			t.Errorf("code = %q", r.Form.Get("code"))
		}
		if r.Form.Get("code_verifier") != "verifier-1" {
			t.Errorf("code_verifier = %q", r.Form.Get("code_verifier"))
		}
		if r.Form.Get("client_secret") != "secret" {
			t.Errorf("client_secret = %q", r.Form.Get("client_secret"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"gho_at","token_type":"bearer","scope":"repo,read:org"}`))
	}))
	defer srv.Close()

	strat := newGitHubOAuthStrategy()
	creds, err := strat.Exchange(context.Background(), srv.Client(),
		OAuthClient{ClientID: "cid", ClientSecret: "secret", TokenURL: srv.URL},
		"code-1", "verifier-1", "https://app.example/callback")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if creds["access_token"] != "gho_at" {
		t.Errorf("creds[access_token] = %v, want gho_at", creds["access_token"])
	}
	if creds["scope"] != "repo,read:org" {
		t.Errorf("creds[scope] = %v, want repo,read:org", creds["scope"])
	}
}

func TestGitHubExchange_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad_verification_code","error_description":"bad code"}`))
	}))
	defer srv.Close()

	strat := newGitHubOAuthStrategy()
	_, err := strat.Exchange(context.Background(), srv.Client(),
		OAuthClient{ClientID: "cid", ClientSecret: "secret", TokenURL: srv.URL},
		"code-1", "verifier-1", "https://app.example/callback")
	if err == nil || !strings.Contains(err.Error(), "bad_verification_code") {
		t.Fatalf("Exchange() error = %v, want bad_verification_code", err)
	}
}

func TestGitHubExchange_MissingAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token_type":"bearer"}`))
	}))
	defer srv.Close()

	strat := newGitHubOAuthStrategy()
	_, err := strat.Exchange(context.Background(), srv.Client(),
		OAuthClient{ClientID: "cid", ClientSecret: "secret", TokenURL: srv.URL},
		"code-1", "verifier-1", "https://app.example/callback")
	if err == nil || !strings.Contains(err.Error(), "no access token") {
		t.Fatalf("Exchange() error = %v, want missing-access-token error", err)
	}
}
