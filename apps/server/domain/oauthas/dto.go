package oauthas

import "time"

// ClientRegistrationRequest is the RFC 7591 dynamic client registration body.
// Every field except redirect_uris is optional; the server only accepts public
// clients (token_endpoint_auth_method = "none") and the authorization_code
// grant.
type ClientRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

// ClientRegistrationResponse is the RFC 7591 registration response. It never
// contains a client secret.
type ClientRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	Scope                   string   `json:"scope"`
}

// OAuthProject is one project offered on the consent page.
//
// kb.projects has no persisted slug column, so Slug is a deterministic,
// URL-safe derivation of the project name (the project UUID remains the
// canonical identifier and is what authorize accepts).
type OAuthProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// AuthorizeContextResponse is the consent-page bootstrap payload: the validated
// client, the fixed redirect URI, the scopes the AS supports, and the projects
// the caller can write to.
type AuthorizeContextResponse struct {
	ClientID        string         `json:"client_id"`
	ClientName      string         `json:"client_name"`
	RedirectURI     string         `json:"redirect_uri"`
	ScopesSupported []string       `json:"scopes_supported"`
	Projects        []OAuthProject `json:"projects"`
}

// AuthorizeRequest is the consent decision posted by the gateway. The gateway
// is responsible for rendering consent; this endpoint requires a real user
// bearer and re-checks project write membership before issuing a code.
type AuthorizeRequest struct {
	ClientID            string   `json:"client_id"`
	RedirectURI         string   `json:"redirect_uri"`
	ProjectID           string   `json:"project_id"`
	Scopes              []string `json:"scopes"`
	State               string   `json:"state"`
	CodeChallenge       string   `json:"code_challenge"`
	CodeChallengeMethod string   `json:"code_challenge_method"`
}

// AuthorizeResponse carries the redirect back to the client with the one-time
// code (and the caller's state) appended.
type AuthorizeResponse struct {
	RedirectURL string `json:"redirect_url"`
}

// TokenRequest is the parsed authorization_code token request. Fields arrive as
// application/x-www-form-urlencoded values.
type TokenRequest struct {
	GrantType    string
	Code         string
	RedirectURI  string
	ClientID     string
	CodeVerifier string
}

// TokenResponse is the RFC 6749 token response. It intentionally carries no
// refresh token: the access token is a project-scoped emt_ token.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
}

// GrantDTO is one row of GET /api/oauth/grants.
type GrantDTO struct {
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

// OAuthErrorResponse is the RFC 6749 §5.2 (and RFC 7591) error body.
type OAuthErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}
