package mcpregistry

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// This file implements the OAuth 2.1 client used to authorize an OAuth-protected
// remote MCP server (MCP authorization, RFC 9728 protected resource metadata,
// RFC 8414 authorization server metadata, RFC 7591 dynamic client registration,
// RFC 7636 PKCE, RFC 8707 resource indicators).
//
// It is deliberately hand-rolled rather than delegated to the MCP client
// library's OAuth handler, for two reasons: the library omits the RFC 8707
// `resource` parameter on both the authorization URL and the token/refresh
// requests, and it keeps a dynamically registered client identity only in
// process memory (this flow persists it per server).

const (
	// oauthHTTPTimeout bounds every outbound OAuth HTTP call.
	oauthHTTPTimeout = 15 * time.Second
	// oauthMaxBodyBytes caps how much of a metadata/token response is read.
	oauthMaxBodyBytes = 1 << 20
	// oauthExpirySkew is how far before real expiry a token is treated as
	// expired, so a token is never used past its lifetime mid-call.
	oauthExpirySkew = 30 * time.Second
)

// authChallenge is the parsed `WWW-Authenticate: Bearer` challenge a protected
// resource returns alongside its 401. Only the parameters the connect flow needs
// are extracted; everything else is ignored.
type authChallenge struct {
	ResourceMetadataURL string
	AuthorizationURI    string
	Scope               string
}

// protectedResourceMetadata is the RFC 9728 document describing the resource
// server (the MCP endpoint) and which authorization servers can issue tokens for
// it.
type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// authServerMetadata is the RFC 8414 document describing the authorization
// server. RegistrationEndpoint is empty when dynamic client registration is not
// offered.
type authServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	ScopesSupported                   []string `json:"scopes_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

// authRequirement is the resolved "this server needs authorization" fact: the
// authorization server to send the user to, the scopes to request, and the
// endpoints needed to complete the flow later.
type authRequirement struct {
	// ResourceMetadataURL is the protected-resource metadata document that was
	// consulted (empty when only an authorization_uri was advertised).
	ResourceMetadataURL string
	// Resource is the RFC 8707 resource identifier (the MCP endpoint URL).
	Resource string
	// AuthorizationServer is the issuer/base URL of the authorization server.
	AuthorizationServer string
	// AuthorizationEndpoint, TokenEndpoint, RegistrationEndpoint come from the
	// authorization server metadata.
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string
	// Scopes are the scopes to request: the challenge scope when present,
	// otherwise the resource's supported scopes.
	Scopes []string
	// RegistrationSupported is true when the authorization server offers
	// dynamic client registration.
	RegistrationSupported bool
}

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

// oauthTokenSet is a token endpoint response. Token values never leave the
// service layer except to be encrypted into the store.
type oauthTokenSet struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	ExpiresAt    time.Time
}

// parseBearerChallenge extracts the Bearer challenge parameters from a
// WWW-Authenticate header value. It reports false when the header is not a
// Bearer challenge; missing parameters are simply empty.
//
// The parser accepts both quoted (`key="value"`) and token (`key=value`) forms
// and tolerates extra parameters in any order.
func parseBearerChallenge(header string) (authChallenge, bool) {
	var ch authChallenge
	header = strings.TrimSpace(header)
	if header == "" {
		return ch, false
	}
	if !strings.HasPrefix(strings.ToLower(header), "bearer") {
		return ch, false
	}
	rest := strings.TrimSpace(header[len("bearer"):])

	// Split on commas that are not inside a quoted string.
	var params []string
	var cur strings.Builder
	inQuotes := false
	for _, r := range rest {
		switch {
		case r == '"':
			inQuotes = !inQuotes
			cur.WriteRune(r)
		case r == ',' && !inQuotes:
			params = append(params, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		params = append(params, cur.String())
	}

	for _, p := range params {
		key, value, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"`)
		switch key {
		case "resource_metadata":
			ch.ResourceMetadataURL = value
		case "authorization_uri":
			ch.AuthorizationURI = value
		case "scope":
			ch.Scope = value
		}
	}
	if ch.ResourceMetadataURL == "" && ch.AuthorizationURI == "" {
		return ch, false
	}
	return ch, true
}

// discoverAuthRequirement resolves a challenge into the authorization server,
// endpoints, and scopes needed to drive a connect flow. Metadata documents are
// fetched over the SSRF-guarded outbound client.
//
// The resource identifier is the MCP endpoint URL unless the protected resource
// metadata names a different canonical resource (Figma, for example, serves PRM
// from a well-known path but identifies the resource as the MCP endpoint).
func discoverAuthRequirement(ctx context.Context, resourceURL string, ch authChallenge) (*authRequirement, error) {
	req := &authRequirement{
		ResourceMetadataURL: ch.ResourceMetadataURL,
		Resource:            resourceURL,
		Scopes:              splitScope(ch.Scope),
	}

	// Step 1: protected resource metadata (RFC 9728) names the authorization
	// server(s) — unless the challenge gave us one directly.
	if ch.ResourceMetadataURL != "" {
		prm := &protectedResourceMetadata{}
		if err := getJSON(ctx, ch.ResourceMetadataURL, prm); err != nil {
			return nil, fmt.Errorf("fetching protected resource metadata: %w", err)
		}
		if strings.TrimSpace(prm.Resource) != "" {
			req.Resource = strings.TrimSpace(prm.Resource)
		}
		if len(prm.AuthorizationServers) > 0 && strings.TrimSpace(prm.AuthorizationServers[0]) != "" {
			req.AuthorizationServer = strings.TrimSpace(prm.AuthorizationServers[0])
		}
		if len(req.Scopes) == 0 && len(prm.ScopesSupported) > 0 {
			req.Scopes = append([]string(nil), prm.ScopesSupported...)
		}
	}
	if req.AuthorizationServer == "" && ch.AuthorizationURI != "" {
		req.AuthorizationServer = issuerFromMetadataURL(ch.AuthorizationURI)
		// The challenge may point straight at the metadata document; prefer the
		// issuer, and fall back to fetching the document's own issuer field.
		if req.AuthorizationServer == "" {
			md := &authServerMetadata{}
			if err := getJSON(ctx, ch.AuthorizationURI, md); err != nil {
				return nil, fmt.Errorf("fetching authorization server metadata: %w", err)
			}
			req.AuthorizationServer = strings.TrimSpace(md.Issuer)
		}
	}
	if req.AuthorizationServer == "" {
		return nil, fmt.Errorf("challenge did not identify an authorization server")
	}

	// Step 2: authorization server metadata (RFC 8414). The well-known path is
	// derived from the issuer, and the challenge's authorization_uri is used
	// directly when it points at a metadata document.
	md := &authServerMetadata{}
	mdURL := ch.AuthorizationURI
	if mdURL == "" || issuerFromMetadataURL(mdURL) != "" {
		mdURL = metadataURLFor(req.AuthorizationServer)
	}
	if err := getJSON(ctx, mdURL, md); err != nil {
		return nil, fmt.Errorf("fetching authorization server metadata: %w", err)
	}
	req.AuthorizationEndpoint = strings.TrimSpace(md.AuthorizationEndpoint)
	req.TokenEndpoint = strings.TrimSpace(md.TokenEndpoint)
	req.RegistrationEndpoint = strings.TrimSpace(md.RegistrationEndpoint)
	req.RegistrationSupported = req.RegistrationEndpoint != ""

	if req.AuthorizationEndpoint == "" || req.TokenEndpoint == "" {
		return nil, fmt.Errorf("authorization server metadata is missing endpoints")
	}
	if len(req.Scopes) == 0 && len(md.ScopesSupported) > 0 {
		req.Scopes = append([]string(nil), md.ScopesSupported...)
	}
	return req, nil
}

// issuerFromMetadataURL returns the issuer base URL implied by a well-known
// metadata document URL, or "" when the URL is not a well-known document path.
//
// It handles both the RFC 8414 path form (`https://issuer/.well-known/oauth-authorization-server`)
// and the path-inserting form (`https://issuer/.well-known/oauth-authorization-server/tenant`).
func issuerFromMetadataURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	idx := strings.Index(u.Path, "/.well-known/")
	if idx < 0 {
		return ""
	}
	base := *u
	base.Path = u.Path[:idx]
	base.RawQuery = ""
	base.Fragment = ""
	return strings.TrimRight(base.String(), "/")
}

// metadataURLFor builds the RFC 8414 well-known metadata URL for an issuer.
func metadataURLFor(issuer string) string {
	return strings.TrimRight(issuer, "/") + "/.well-known/oauth-authorization-server"
}

// splitScope splits a space-delimited scope string, dropping empties.
func splitScope(scope string) []string {
	fields := strings.Fields(scope)
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// Test seams. The outbound guard and client factories are variables so unit
// tests can drive a local httptest authorization server; production always uses
// validateOutboundFetchURL and the SSRF-guarded clients.
var (
	oauthValidateURL     = validateOutboundFetchURL
	oauthMetadataClient  = func(rawURL string) *http.Client { return outboundHTTPClient(rawURL) }
	oauthProbeHTTPClient = func(rawURL string) *http.Client { return outboundProbeClient(rawURL) }
)

// getJSON performs a guarded GET and decodes a JSON document into out.
func getJSON(ctx context.Context, rawURL string, out any) error {
	if err := oauthValidateURL(ctx, rawURL); err != nil {
		return fmt.Errorf("%w: %v", ErrOutboundURLNotAllowed, err)
	}
	client := oauthMetadataClient(rawURL)
	client.Timeout = oauthHTTPTimeout

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("building metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("metadata request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, oauthMaxBodyBytes))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metadata endpoint returned HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding metadata document: %w", err)
	}
	return nil
}

// --- PKCE / state ---

// newOAuthRandom returns a URL-safe random string from n random bytes.
func newOAuthRandom(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// newOAuthState returns a single-use state value (RFC 6749 §10.12).
func newOAuthState() (string, error) { return newOAuthRandom(32) }

// newCodeVerifier returns a PKCE code verifier (RFC 7636 §4.1).
func newCodeVerifier() (string, error) { return newOAuthRandom(32) }

// codeChallengeS256 returns the S256 PKCE challenge for a verifier.
func codeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// --- OAuth client operations ---

// oauthClientIdentity is the client material registered (or pre-configured) for
// a server. A public client has no secret.
type oauthClientIdentity struct {
	ClientID     string
	ClientSecret string
}

// registerClient performs RFC 7591 dynamic client registration, returning the
// issued client identity. Public clients (no secret) are requested by default,
// which is what a PKCE-driven browser flow needs.
func registerClient(ctx context.Context, registrationEndpoint, clientName, redirectURI string, scopes []string) (*oauthClientIdentity, error) {
	body := map[string]any{
		"client_name":                clientName,
		"redirect_uris":              []string{redirectURI},
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	}
	if len(scopes) > 0 {
		body["scope"] = strings.Join(scopes, " ")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding registration request: %w", err)
	}

	if err := oauthValidateURL(ctx, registrationEndpoint); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOutboundURLNotAllowed, err)
	}
	client := oauthMetadataClient(registrationEndpoint)
	client.Timeout = oauthHTTPTimeout

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("building registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registration request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, oauthMaxBodyBytes))

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dynamic client registration failed with HTTP %d", resp.StatusCode)
	}
	var out struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decoding registration response: %w", err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("dynamic client registration rejected: %s", out.Error)
	}
	if strings.TrimSpace(out.ClientID) == "" {
		return nil, fmt.Errorf("dynamic client registration returned no client_id")
	}
	return &oauthClientIdentity{ClientID: out.ClientID, ClientSecret: out.ClientSecret}, nil
}

// buildAuthorizeURL constructs the authorization request URL, including the
// RFC 8707 resource indicator and the PKCE challenge.
func buildAuthorizeURL(req *authRequirement, identity *oauthClientIdentity, redirectURI, state, codeChallenge string) (string, error) {
	if req.AuthorizationEndpoint == "" {
		return "", fmt.Errorf("authorization endpoint is unknown")
	}
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", identity.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	if len(req.Scopes) > 0 {
		q.Set("scope", strings.Join(req.Scopes, " "))
	}
	// RFC 8707: identify the protected resource the token is for. The MCP
	// authorization spec requires this so an authorization server cannot hand
	// back a token minted for a different resource.
	if req.Resource != "" {
		q.Set("resource", req.Resource)
	}
	u, err := url.Parse(req.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("authorization endpoint is not a valid URL: %w", err)
	}
	existing := u.Query()
	for k, v := range q {
		existing.Set(k, v[0])
	}
	u.RawQuery = existing.Encode()
	return u.String(), nil
}

// exchangeCode redeems an authorization code for a token set (RFC 6749 §4.1.3),
// carrying the PKCE verifier and the resource indicator.
func exchangeCode(ctx context.Context, req *authRequirement, identity *oauthClientIdentity, code, codeVerifier, redirectURI string) (*oauthTokenSet, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}
	if req.Resource != "" {
		form.Set("resource", req.Resource)
	}
	return postToken(ctx, req.TokenEndpoint, identity, form)
}

// refreshAccessToken exchanges a refresh token for a new token set, carrying the
// resource indicator.
func refreshAccessToken(ctx context.Context, tokenEndpoint string, identity *oauthClientIdentity, refreshToken, resource string) (*oauthTokenSet, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	if resource != "" {
		form.Set("resource", resource)
	}
	return postToken(ctx, tokenEndpoint, identity, form)
}

// postToken performs a token endpoint form POST and decodes the token set.
//
// Client authentication follows the registered identity: a client with a secret
// authenticates with HTTP basic auth (client_secret_basic, the RFC 6749
// preferred method), and a public client sends only its client_id in the body.
func postToken(ctx context.Context, tokenEndpoint string, identity *oauthClientIdentity, form url.Values) (*oauthTokenSet, error) {
	if tokenEndpoint == "" {
		return nil, fmt.Errorf("token endpoint is unknown")
	}
	if err := oauthValidateURL(ctx, tokenEndpoint); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOutboundURLNotAllowed, err)
	}

	body := url.Values{}
	for k, v := range form {
		body[k] = v
	}
	if identity.ClientSecret == "" {
		body.Set("client_id", identity.ClientID)
	}

	client := oauthMetadataClient(tokenEndpoint)
	client.Timeout = oauthHTTPTimeout

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, fmt.Errorf("building token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")
	if identity.ClientSecret != "" {
		httpReq.SetBasicAuth(identity.ClientID, identity.ClientSecret)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, oauthMaxBodyBytes))

	var out struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		TokenType        string `json:"token_type"`
		Scope            string `json:"scope"`
		ExpiresIn        int64  `json:"expires_in"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || out.Error != "" {
		msg := out.Error
		if out.ErrorDescription != "" {
			msg = out.Error + ": " + out.ErrorDescription
		}
		if msg == "" {
			msg = fmt.Sprintf("token endpoint returned HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("token endpoint rejected the request: %s", msg)
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return nil, fmt.Errorf("token endpoint returned no access_token")
	}

	set := &oauthTokenSet{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		TokenType:    out.TokenType,
		Scope:        out.Scope,
	}
	if out.ExpiresIn > 0 {
		set.ExpiresAt = time.Now().UTC().Add(time.Duration(out.ExpiresIn) * time.Second)
	}
	return set, nil
}
