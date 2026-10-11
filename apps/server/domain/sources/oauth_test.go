package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCodeChallengeS256_RFC7636Vector(t *testing.T) {
	// RFC 7636 Appendix B.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := CodeChallengeS256(verifier); got != want {
		t.Fatalf("CodeChallengeS256() = %q, want %q", got, want)
	}
}

func TestOAuthStateAndVerifier_NonEmptyAndDistinct(t *testing.T) {
	s1, err := NewOAuthState()
	if err != nil {
		t.Fatalf("NewOAuthState() error = %v", err)
	}
	s2, _ := NewOAuthState()
	if s1 == "" || s1 == s2 {
		t.Fatalf("states must be non-empty and distinct: %q, %q", s1, s2)
	}
	v, err := NewCodeVerifier()
	if err != nil {
		t.Fatalf("NewCodeVerifier() error = %v", err)
	}
	// RFC 7636: 43-128 characters; 32 random bytes base64url = 43 chars.
	if len(v) != 43 {
		t.Fatalf("NewCodeVerifier() len = %d, want 43", len(v))
	}
}

func TestGoogleAuthorizeURL_Defaults(t *testing.T) {
	strat := newGoogleOAuthStrategy()
	raw := strat.AuthorizeURL(OAuthClient{ClientID: "cid"}, OAuthAuthorizeRequest{
		State:         "state-1",
		CodeChallenge: "challenge-1",
		RedirectURI:   "https://app.example/callback",
	})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("authorize URL parse error = %v", err)
	}
	if u.Host != "accounts.google.com" {
		t.Errorf("host = %q, want accounts.google.com", u.Host)
	}
	q := u.Query()
	checks := map[string]string{
		"client_id":             "cid",
		"redirect_uri":          "https://app.example/callback",
		"response_type":         "code",
		"state":                 "state-1",
		"code_challenge":        "challenge-1",
		"code_challenge_method": "S256",
		"access_type":           "offline",
		"prompt":                "consent",
	}
	for k, want := range checks {
		if got := q.Get(k); got != want {
			t.Errorf("param %q = %q, want %q", k, got, want)
		}
	}
	if got := q.Get("scope"); !strings.Contains(got, googleDriveFile) {
		t.Errorf("scope = %q, want to contain %q", got, googleDriveFile)
	}
}

func TestGoogleExchange_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("code_verifier") != "verifier-1" {
			t.Errorf("code_verifier = %q", r.Form.Get("code_verifier"))
		}
		if r.Form.Get("client_secret") != "secret" {
			t.Errorf("client_secret = %q", r.Form.Get("client_secret"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3599,"scope":"https://www.googleapis.com/auth/drive.file","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	strat := newGoogleOAuthStrategy()
	creds, err := strat.Exchange(context.Background(), srv.Client(),
		OAuthClient{ClientID: "cid", ClientSecret: "secret", TokenURL: srv.URL},
		"code-1", "verifier-1", "https://app.example/callback")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if creds["refresh_token"] != "rt" {
		t.Errorf("creds[refresh_token] = %v, want rt", creds["refresh_token"])
	}
	if creds["access_token"] != "at" {
		t.Errorf("creds[access_token] = %v, want at", creds["access_token"])
	}
}

func TestGoogleExchange_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"bad code"}`))
	}))
	defer srv.Close()

	strat := newGoogleOAuthStrategy()
	_, err := strat.Exchange(context.Background(), srv.Client(),
		OAuthClient{ClientID: "cid", ClientSecret: "secret", TokenURL: srv.URL},
		"code-1", "verifier-1", "https://app.example/callback")
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("Exchange() error = %v, want invalid_grant", err)
	}
}

func TestGoogleExchange_MissingRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"at","expires_in":3599,"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	strat := newGoogleOAuthStrategy()
	_, err := strat.Exchange(context.Background(), srv.Client(),
		OAuthClient{ClientID: "cid", ClientSecret: "secret", TokenURL: srv.URL},
		"code-1", "verifier-1", "https://app.example/callback")
	if err == nil || !strings.Contains(err.Error(), "no refresh token") {
		t.Fatalf("Exchange() error = %v, want missing-refresh-token error", err)
	}
}
