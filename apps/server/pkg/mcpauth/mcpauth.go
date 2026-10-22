package mcpauth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ExpirySkew is subtracted from a token's nominal expiry so a token is treated
// as expired slightly before the server actually rejects it, absorbing clock
// drift between this host and the authorization server.
const ExpirySkew = 30 * time.Second

// Config is the input for discovering and registering a dynamic OAuth client.
type Config struct {
	// Resource is the protected resource URL (the MCP endpoint, e.g.
	// https://app.trale.ai/api/mcp).
	Resource string
	// ClientName is the RFC 7591 client_name submitted at registration.
	ClientName string
	// Scopes is the requested scope set (DCR scope + authorize scope).
	Scopes []string
	// RedirectURI is the OAuth callback the AS redirects to.
	RedirectURI string
	// HTTPClient is optional. When set it is used as-is (test injection or a
	// caller-provided transport); when nil the resource's SSRF-guarded client is
	// used.
	HTTPClient *http.Client
}

// Client is a discovered, registered dynamic OAuth client: the identity and
// endpoints a caller needs to build an authorize URL and exchange a code.
type Client struct {
	ClientID string
	AuthURL  string
	TokenURL string
	Resource string
	Scopes   []string

	httpClient *http.Client
}

// NewClient builds a Client from an already-registered identity/endpoints. It is
// used by callers that registered via DiscoverAndRegister and then carry the
// client_id/endpoints through their own flow (the sources exchange step, the
// connector refresh step).
func NewClient(clientID, tokenURL, resource string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = OutboundHTTPClient(resource)
	}
	return &Client{
		ClientID:   clientID,
		TokenURL:   tokenURL,
		Resource:   resource,
		httpClient: httpClient,
	}
}

// Tokens is the result of an authorization-code exchange or a refresh.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	TokenType    string
	IDToken      string
	Scope        string
}

// ExpiresAt returns the token expiry with the skew applied, or the zero time
// when the endpoint reported no lifetime.
func (t Tokens) ExpiresAt(now time.Time) time.Time {
	if t.ExpiresIn <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(t.ExpiresIn)*time.Second - ExpirySkew)
}

// DiscoverAndRegister performs the full dynamic-client bootstrap for a protected
// resource:
//
//  1. Discover the protected-resource metadata (RFC 9728): probe the resource
//     for a 401 Bearer challenge advertising `resource_metadata`, else fetch
//     `/.well-known/oauth-protected-resource` on the resource origin.
//  2. Fetch the authorization-server metadata (RFC 8414 / OpenID discovery).
//  3. Register a public client via RFC 7591 dynamic client registration.
func DiscoverAndRegister(ctx context.Context, cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Resource) == "" {
		return nil, fmt.Errorf("mcpauth: resource URL is required")
	}
	httpc := cfg.HTTPClient
	if httpc == nil {
		// No caller-supplied transport: enforce the SSRF guard on the resource
		// URL and build the guarded client. When a client is injected (tests, or
		// a caller that owns its transport), the caller is responsible for the
		// destination's safety, so the static check is skipped.
		if err := validateURL(cfg.Resource); err != nil {
			return nil, fmt.Errorf("mcpauth: resource URL rejected: %w", err)
		}
		httpc = OutboundHTTPClient(cfg.Resource)
	}

	prm, err := discoverProtectedResource(ctx, httpc, cfg.Resource)
	if err != nil {
		return nil, fmt.Errorf("mcpauth: protected-resource discovery: %w", err)
	}
	resource := prm.Resource
	if strings.TrimSpace(resource) == "" {
		resource = cfg.Resource
	}

	asURL := ""
	if len(prm.AuthorizationServers) > 0 {
		asURL = prm.AuthorizationServers[0]
	}
	if strings.TrimSpace(asURL) == "" {
		asURL = resourceOrigin(cfg.Resource) + "/.well-known/oauth-authorization-server"
	}
	as, err := fetchASMetadata(ctx, httpc, asURL)
	if err != nil {
		return nil, fmt.Errorf("mcpauth: authorization-server metadata: %w", err)
	}

	clientID, err := registerClient(ctx, httpc, as.RegistrationEndpoint, cfg)
	if err != nil {
		return nil, fmt.Errorf("mcpauth: dynamic client registration: %w", err)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = as.ScopesSupported
	}

	return &Client{
		ClientID:   clientID,
		AuthURL:    as.AuthorizationEndpoint,
		TokenURL:   as.TokenEndpoint,
		Resource:   resource,
		Scopes:     scopes,
		httpClient: httpc,
	}, nil
}

// Exchange redeems an authorization code for tokens (RFC 6749 §4.1.3, public
// client, PKCE, RFC 8707 resource parameter).
func (c *Client) Exchange(ctx context.Context, code, codeVerifier, redirectURI string) (*Tokens, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("mcpauth: client not initialised")
	}
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("mcpauth: authorization code is empty")
	}
	form := tokenForm("authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}
	return postToken(ctx, c.httpClient, c.TokenURL, c.ClientID, c.Resource, form)
}

// Refresh rotates a refresh token for a new token set (RFC 6749 §6, public
// client, no secret).
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("mcpauth: client not initialised")
	}
	if strings.TrimSpace(refreshToken) == "" {
		return nil, fmt.Errorf("mcpauth: refresh token is empty")
	}
	form := tokenForm("refresh_token")
	form.Set("refresh_token", refreshToken)
	return postToken(ctx, c.httpClient, c.TokenURL, c.ClientID, c.Resource, form)
}

// resourceOrigin returns the scheme://host[:port] origin of a URL, used to
// derive the well-known discovery endpoints on the resource's own origin.
func resourceOrigin(raw string) string {
	u, err := parseURL(raw)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
