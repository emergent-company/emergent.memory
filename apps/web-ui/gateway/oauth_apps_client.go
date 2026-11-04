package main

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// OAuthProject is one project offered on the consent page.
type OAuthProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// OAuthConsentContext is the consent-page bootstrap payload returned by the
// Memory authorization server.
type OAuthConsentContext struct {
	ClientID        string         `json:"client_id"`
	ClientName      string         `json:"client_name"`
	RedirectURI     string         `json:"redirect_uri"`
	ScopesSupported []string       `json:"scopes_supported"`
	Projects        []OAuthProject `json:"projects"`
}

// OAuthAuthorizeRequest is the consent decision posted back to Memory.
type OAuthAuthorizeRequest struct {
	ClientID            string   `json:"client_id"`
	RedirectURI         string   `json:"redirect_uri"`
	ProjectID           string   `json:"project_id"`
	Scopes              []string `json:"scopes"`
	State               string   `json:"state"`
	CodeChallenge       string   `json:"code_challenge"`
	CodeChallengeMethod string   `json:"code_challenge_method"`
}

// OAuthAuthorizeResult carries the redirect back to the OAuth client.
type OAuthAuthorizeResult struct {
	RedirectURL string `json:"redirect_url"`
}

// OAuthGrant is one connected app shown under Settings → Connected apps.
type OAuthGrant struct {
	ID          string     `json:"id"`
	ClientID    string     `json:"client_id"`
	ClientName  string     `json:"client_name"`
	ProjectID   string     `json:"project_id"`
	ProjectName string     `json:"project_name"`
	Scopes      []string   `json:"scopes"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
}

// OAuthAuthorizeContext fetches the validated client, redirect URI, supported
// scopes, and writable projects for the consent page.
func (m *MemoryClient) OAuthAuthorizeContext(ctx context.Context, clientID, redirectURI string) (*OAuthConsentContext, error) {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	var out OAuthConsentContext
	if err := m.do(ctx, http.MethodGet, "/api/oauth/authorize/context?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// OAuthAuthorize posts the user's consent decision and returns the redirect
// carrying the one-time authorization code.
func (m *MemoryClient) OAuthAuthorize(ctx context.Context, in OAuthAuthorizeRequest) (*OAuthAuthorizeResult, error) {
	var out OAuthAuthorizeResult
	if err := m.do(ctx, http.MethodPost, "/api/oauth/authorize", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListOAuthGrants returns the user's active connected apps.
func (m *MemoryClient) ListOAuthGrants(ctx context.Context) ([]OAuthGrant, error) {
	var out []OAuthGrant
	if err := m.do(ctx, http.MethodGet, "/api/oauth/grants", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RevokeOAuthGrant revokes one connected app and its project token.
func (m *MemoryClient) RevokeOAuthGrant(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodDelete, "/api/oauth/grants/"+url.PathEscape(id), nil, nil)
}

// oauthBackend is the narrow slice of the Memory API the OAuth consent and
// connected-app surfaces need. Keeping it separate from MemoryBackend means
// existing fakes and alternative backends keep compiling unchanged.
type oauthBackend interface {
	OAuthAuthorizeContext(ctx context.Context, clientID, redirectURI string) (*OAuthConsentContext, error)
	OAuthAuthorize(ctx context.Context, in OAuthAuthorizeRequest) (*OAuthAuthorizeResult, error)
	ListOAuthGrants(ctx context.Context) ([]OAuthGrant, error)
	RevokeOAuthGrant(ctx context.Context, id string) error
}

var _ oauthBackend = (*MemoryClient)(nil)
