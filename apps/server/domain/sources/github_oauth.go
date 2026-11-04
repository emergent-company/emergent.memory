package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ---- GitHub OAuth2 strategy ----

const (
	githubAuthURL  = "https://github.com/login/oauth/authorize"
	githubTokenURL = "https://github.com/login/oauth/access_token"

	// GitHub OAuth App access tokens are long-lived (no refresh token); the
	// default scopes grant repo access plus read access to the user's orgs.
	githubScopeRepo = "repo"
	githubScopeOrg  = "read:org"
)

// githubOAuthStrategy implements OAuthStrategy for the GitHub provider.
type githubOAuthStrategy struct{}

func newGitHubOAuthStrategy() *githubOAuthStrategy {
	return &githubOAuthStrategy{}
}

func (s *githubOAuthStrategy) DefaultScopes() []string {
	return []string{githubScopeRepo, githubScopeOrg}
}

func (s *githubOAuthStrategy) AuthorizeURL(client OAuthClient, req OAuthAuthorizeRequest) string {
	authURL := client.AuthURL
	if authURL == "" {
		authURL = githubAuthURL
	}
	scopes := req.Scopes
	if len(scopes) == 0 {
		scopes = client.Scopes
	}
	if len(scopes) == 0 {
		scopes = s.DefaultScopes()
	}

	q := url.Values{}
	q.Set("client_id", client.ClientID)
	q.Set("redirect_uri", req.RedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(scopes, " "))
	q.Set("state", req.State)
	q.Set("code_challenge", req.CodeChallenge)
	q.Set("code_challenge_method", "S256")
	for k, v := range req.Extra {
		q.Set(k, v)
	}
	return authURL + "?" + q.Encode()
}

func (s *githubOAuthStrategy) Exchange(ctx context.Context, httpClient *http.Client, client OAuthClient, code, codeVerifier, redirectURI string) (Credentials, error) {
	tokenURL := client.TokenURL
	if tokenURL == "" {
		tokenURL = githubTokenURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", client.ClientID)
	form.Set("client_secret", client.ClientSecret)
	form.Set("redirect_uri", redirectURI)
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("sources: build github token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sources: github token exchange request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var payload struct {
		AccessToken      string `json:"access_token"`
		TokenType        string `json:"token_type"`
		Scope            string `json:"scope"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("sources: decode github token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || payload.Error != "" {
		msg := payload.Error
		if payload.ErrorDescription != "" {
			msg = payload.Error + ": " + payload.ErrorDescription
		}
		if msg == "" {
			msg = fmt.Sprintf("token endpoint returned %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("sources: github token exchange failed: %s", msg)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return nil, fmt.Errorf("sources: github token exchange returned no access token")
	}

	// The stored credential carries the long-lived access token (GitHub OAuth
	// App tokens do not expire, so there is no refresh token). The client
	// id/secret live in org/deployment config.
	creds := Credentials{
		"access_token": payload.AccessToken,
	}
	if payload.Scope != "" {
		creds["scope"] = payload.Scope
	}
	return creds, nil
}
