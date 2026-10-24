package mcpauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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

	httpClient   *http.Client
	clientSecret string
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
//     for a 401/403 Bearer challenge advertising `resource_metadata`, else fetch
//     `/.well-known/oauth-protected-resource` on the resource origin.
//  2. Resolve the authorization-server metadata (RFC 8414 / OpenID discovery)
//     from the issuer advertised in the protected-resource metadata (or the
//     resource origin when none is advertised). See Discover.
//  3. Register a public client via RFC 7591 dynamic client registration.
func DiscoverAndRegister(ctx context.Context, cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Resource) == "" {
		return nil, fmt.Errorf("mcpauth: resource URL is required")
	}
	var clientFor ClientFactory
	var validate ValidateFunc
	if cfg.HTTPClient != nil {
		// An injected transport means the caller owns destination safety (tests,
		// or a caller with its own policy): use it as-is and skip the static
		// check, exactly as before.
		clientFor = func(string) *http.Client { return cfg.HTTPClient }
		validate = func(context.Context, string) error { return nil }
	} else {
		if err := validateURL(cfg.Resource); err != nil {
			return nil, fmt.Errorf("mcpauth: resource URL rejected: %w", err)
		}
	}

	// The probe is best-effort: a resource that does not answer with a challenge
	// may still publish its metadata at the well-known location.
	challenge, _, _ := ProbeResource(ctx, ProbeConfig{Resource: cfg.Resource, ClientFor: clientFor, Validate: validate})

	req, err := Discover(ctx, DiscoveryConfig{
		Resource:  cfg.Resource,
		Challenge: challenge,
		Scopes:    cfg.Scopes,
		ClientFor: clientFor,
		Validate:  validate,
	})
	if err != nil {
		return nil, fmt.Errorf("mcpauth: %w", err)
	}

	identity, err := Register(ctx, RegistrationConfig{
		RegistrationEndpoint: req.RegistrationEndpoint,
		ClientName:           cfg.ClientName,
		RedirectURI:          cfg.RedirectURI,
		Scopes:               req.Scopes,
		ClientFor:            clientFor,
		Validate:             validate,
	})
	if err != nil {
		return nil, fmt.Errorf("mcpauth: dynamic client registration: %w", err)
	}

	httpc := cfg.HTTPClient
	if httpc == nil {
		httpc = OutboundHTTPClient(cfg.Resource)
	}
	return &Client{
		ClientID:     identity.ClientID,
		AuthURL:      req.AuthorizationEndpoint,
		TokenURL:     req.TokenEndpoint,
		Resource:     req.Resource,
		Scopes:       req.Scopes,
		httpClient:   httpc,
		clientSecret: identity.ClientSecret,
	}, nil
}

// tokenClient builds the token endpoint client for this identity. The stored
// httpClient already carries the SSRF guard (unless the caller injected a
// transport), so no extra static check is applied here — matching the guard
// posture of the rest of this package.
func (c *Client) tokenClient() *TokenClient {
	return &TokenClient{
		TokenEndpoint: c.TokenURL,
		Resource:      c.Resource,
		Identity:      Identity{ClientID: c.ClientID, ClientSecret: c.clientSecret},
		ClientFor:     func(string) *http.Client { return c.httpClient },
		Validate:      func(context.Context, string) error { return nil },
	}
}

// Exchange redeems an authorization code for tokens (RFC 6749 §4.1.3, PKCE,
// RFC 8707 resource parameter).
func (c *Client) Exchange(ctx context.Context, code, codeVerifier, redirectURI string) (*Tokens, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("mcpauth: client not initialised")
	}
	return c.tokenClient().Exchange(ctx, code, codeVerifier, redirectURI)
}

// Refresh rotates a refresh token for a new token set (RFC 6749 §6).
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("mcpauth: client not initialised")
	}
	return c.tokenClient().Refresh(ctx, refreshToken)
}

// resourceOrigin returns the scheme://host[:port] origin of a URL, used to
// derive the well-known discovery endpoints on the resource's own origin.
func resourceOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
