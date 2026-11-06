package mcpregistry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/mcpauth"
)

// This file adapts the MCP registry's OAuth connect flow to pkg/mcpauth, which
// owns the OAuth 2.1 protocol layer for a self-registering protected resource
// (MCP authorization, RFC 9728 protected resource metadata, RFC 8414
// authorization server metadata, RFC 7591 dynamic client registration, RFC 7636
// PKCE, RFC 8707 resource indicators).
//
// The registry used to carry its own copy of that logic. Two implementations of
// security-sensitive discovery code drift — the Trale bug in #2073 (fetching a
// protected-resource metadata issuer as if it were the metadata URL) is exactly
// the class of mistake the second copy would keep — so the protocol code lives in
// pkg/mcpauth and this file keeps only what is registry policy: the probe error
// contract, the sentinels the handler maps to HTTP statuses, and the persisted
// token shape.

// oauthExpirySkew is how far before real expiry a stored token is treated as
// expired, so a token is never used past its lifetime mid-call.
const oauthExpirySkew = 30 * time.Second

// authChallenge is the parsed `WWW-Authenticate: Bearer` challenge a protected
// resource returns alongside its 401/403.
type authChallenge = mcpauth.Challenge

// authRequirement is the resolved "this server needs authorization" fact: the
// authorization server to send the user to, the scopes to request, and the
// endpoints needed to complete the flow later.
type authRequirement = mcpauth.Requirement

// oauthClientIdentity is the client material registered (or pre-configured) for
// a server. A public client has no secret; a pre-registered confidential client
// authenticates with HTTP basic auth.
type oauthClientIdentity = mcpauth.Identity

// AuthRequiredError reports that a remote MCP server requires OAuth
// authorization. It carries the discovered requirement so the caller can offer
// the connect flow.
type AuthRequiredError struct {
	Requirement *authRequirement
}

func (e *AuthRequiredError) Error() string {
	server := "the authorization server"
	if e != nil && e.Requirement != nil && e.Requirement.AuthorizationServer != "" {
		server = e.Requirement.AuthorizationServer
	}
	return fmt.Sprintf("MCP server requires OAuth authorization (%s)", server)
}

// oauthTokenSet is a token endpoint response in the shape the store persists
// (an absolute expiry, encrypted values). Token values never leave the service
// layer except to be encrypted into the store.
type oauthTokenSet struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	ExpiresAt    time.Time
}

// tokenSetFrom converts the shared protocol result into the persisted shape.
func tokenSetFrom(tokens *mcpauth.Tokens) *oauthTokenSet {
	if tokens == nil {
		return nil
	}
	set := &oauthTokenSet{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		TokenType:    tokens.TokenType,
		Scope:        tokens.Scope,
	}
	if tokens.ExpiresIn > 0 {
		set.ExpiresAt = time.Now().UTC().Add(time.Duration(tokens.ExpiresIn) * time.Second)
	}
	return set
}

// Test seams. The outbound guard and client factories are variables so unit
// tests can drive a local httptest authorization server; production always uses
// validateOutboundFetchURL and the SSRF-guarded clients.
//
// oauthMetadataClient drives every OAuth protocol call (metadata fetch, dynamic
// client registration, token exchange/refresh) and is therefore the unwrapped
// outboundProbeClient: protocol code must observe 401/403 as ordinary status
// codes so registration and token handling can build their typed errors.
// authStatusTransport exists only for credential-bearing MCP-server transports
// (see outboundHTTPClient), where a 401/403 is terminal.
var (
	oauthValidateURL     = validateOutboundFetchURL
	oauthMetadataClient  = func(rawURL string) *http.Client { return outboundProbeClient(rawURL) }
	oauthProbeHTTPClient = func(rawURL string) *http.Client { return outboundProbeClient(rawURL) }
)

// translateProtocolError maps the shared package's sentinels onto the registry's
// own, which the handler maps to HTTP statuses.
func translateProtocolError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, mcpauth.ErrURLNotAllowed):
		return fmt.Errorf("%w: %v", ErrOutboundURLNotAllowed, err)
	case errors.Is(err, mcpauth.ErrMetadataUnavailable):
		return fmt.Errorf("%w: %v", ErrOAuthMetadataUnavailable, err)
	default:
		return err
	}
}

// discoverAuthRequirement resolves a challenge into the authorization server,
// endpoints, and scopes needed to drive a connect flow. Metadata documents are
// fetched over the SSRF-guarded outbound client, and every derived URL is
// validated before it is fetched.
//
// mcpauth.Discover has two fallbacks this adapter does not exercise in its own
// tests, so they are pinned in pkg/mcpauth instead:
//
//   - when the challenge advertises no resource_metadata, Discover consults
//     /.well-known/oauth-protected-resource on the resource origin. The probe
//     reports only challenges carrying resource_metadata or authorization_uri;
//     an advertised document is authoritative, and the authorization_uri-only
//     form is the only registry-reachable route into this fallback;
//   - when the protected-resource document omits authorization_servers (RFC
//     9728 §2 permits this) or the authorization_uri document omits issuer, no
//     authorization server has been named and Discover uses the resource
//     origin. Both routes are reachable from this adapter: a
//     resource_metadata-only challenge whose document has `resource` but no
//     authorization_servers, and an authorization_uri that is not a well-known
//     URL whose document has no issuer.
func discoverAuthRequirement(ctx context.Context, resourceURL string, ch authChallenge) (*authRequirement, error) {
	req, err := mcpauth.Discover(ctx, mcpauth.DiscoveryConfig{
		Resource:  resourceURL,
		Challenge: ch,
		ClientFor: oauthMetadataClient,
		Validate:  oauthValidateURL,
	})
	if err != nil {
		return nil, translateProtocolError(err)
	}
	return req, nil
}

// splitScope splits a space-delimited scope string, dropping empties.
func splitScope(scope string) []string {
	fields := strings.Fields(scope)
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// --- dynamic client registration ---

// oauthRegistrationRejected is returned when the authorization server answers a
// dynamic client registration request with a non-success status (e.g. Figma's
// 403 for catalog-only clients). It carries the HTTP status and a short,
// sanitized excerpt of the response body for diagnosis, and unwraps to
// ErrOAuthClientRequired so the handler can surface the single actionable
// remedy: provision a pre-registered client.
type oauthRegistrationRejected struct {
	Status  int
	Excerpt string
}

func (e *oauthRegistrationRejected) Error() string {
	msg := fmt.Sprintf("dynamic client registration failed with HTTP %d", e.Status)
	if e.Excerpt != "" {
		msg += ": " + e.Excerpt
	}
	return msg
}

func (e *oauthRegistrationRejected) Unwrap() error { return ErrOAuthClientRequired }

// registerClient performs RFC 7591 dynamic client registration, returning the
// issued client identity. Public clients (no secret) are requested by default,
// which is what a PKCE-driven browser flow needs.
func registerClient(ctx context.Context, registrationEndpoint, clientName, redirectURI string, scopes []string) (*oauthClientIdentity, error) {
	identity, err := mcpauth.Register(ctx, mcpauth.RegistrationConfig{
		RegistrationEndpoint: registrationEndpoint,
		ClientName:           clientName,
		RedirectURI:          redirectURI,
		Scopes:               scopes,
		ClientFor:            oauthMetadataClient,
		Validate:             oauthValidateURL,
	})
	if err != nil {
		var rejected *mcpauth.RegistrationRejected
		if errors.As(err, &rejected) {
			return nil, &oauthRegistrationRejected{Status: rejected.Status, Excerpt: rejected.Excerpt}
		}
		if errors.Is(err, mcpauth.ErrNoRegistrationEndpoint) ||
			errors.Is(err, mcpauth.ErrClientRequired) {
			return nil, fmt.Errorf("%w: %v", ErrOAuthClientRequired, err)
		}
		return nil, translateProtocolError(err)
	}
	return identity, nil
}

// --- PKCE / state ---

// newOAuthState returns a single-use state value (RFC 6749 §10.12).
func newOAuthState() (string, error) { return mcpauth.NewState() }

// newCodeVerifier returns a PKCE code verifier (RFC 7636 §4.1).
func newCodeVerifier() (string, error) { return mcpauth.NewCodeVerifier() }

// codeChallengeS256 returns the S256 PKCE challenge for a verifier.
func codeChallengeS256(verifier string) string { return mcpauth.CodeChallengeS256(verifier) }

// --- OAuth client operations ---

// tokenClient builds the shared token endpoint client for one identity. The
// client factory and URL check are the registry's (test seams included), so the
// SSRF posture is identical to the discovery calls.
func tokenClient(tokenEndpoint string, identity *oauthClientIdentity, resource string) *mcpauth.TokenClient {
	tc := &mcpauth.TokenClient{
		TokenEndpoint: tokenEndpoint,
		Resource:      resource,
		ClientFor:     oauthMetadataClient,
		Validate:      oauthValidateURL,
	}
	if identity != nil {
		tc.Identity = *identity
	}
	return tc
}

// buildAuthorizeURL constructs the authorization request URL, including the
// RFC 8707 resource indicator and the PKCE challenge.
func buildAuthorizeURL(req *authRequirement, identity *oauthClientIdentity, redirectURI, state, codeChallenge string) (string, error) {
	return mcpauth.AuthorizeURL(req, identity, redirectURI, state, codeChallenge)
}

// exchangeCode redeems an authorization code for a token set (RFC 6749 §4.1.3),
// carrying the PKCE verifier and the resource indicator.
func exchangeCode(ctx context.Context, req *authRequirement, identity *oauthClientIdentity, code, codeVerifier, redirectURI string) (*oauthTokenSet, error) {
	tokens, err := tokenClient(req.TokenEndpoint, identity, req.Resource).Exchange(ctx, code, codeVerifier, redirectURI)
	if err != nil {
		return nil, translateProtocolError(err)
	}
	return tokenSetFrom(tokens), nil
}

// refreshAccessToken exchanges a refresh token for a new token set, carrying the
// resource indicator.
func refreshAccessToken(ctx context.Context, tokenEndpoint string, identity *oauthClientIdentity, refreshToken, resource string) (*oauthTokenSet, error) {
	tokens, err := tokenClient(tokenEndpoint, identity, resource).Refresh(ctx, refreshToken)
	if err != nil {
		return nil, translateProtocolError(err)
	}
	return tokenSetFrom(tokens), nil
}
