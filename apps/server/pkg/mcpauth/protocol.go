package mcpauth

// This file is the single implementation of the OAuth protocol layer for a
// self-registering protected resource: the Bearer challenge (RFC 9728), the
// protected-resource / authorization-server metadata walk (RFC 9728, RFC 8414,
// OpenID Connect Discovery), dynamic client registration (RFC 7591), the
// authorization request (RFC 6749, RFC 7636 PKCE, RFC 8707 resource
// indicators), and the token exchange/refresh.
//
// It used to exist twice — once in domain/mcpregistry for the MCP registry
// connect flow, once here for the Trale data source. #2073 (fetching a
// protected-resource metadata `authorization_servers` ISSUER as if it were a
// metadata URL) was exactly the class of defect a second copy keeps alive, so
// the registry now adapts to this package instead of carrying its own.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ClientFactory returns the HTTP client to use for one absolute URL. A caller
// that owns its transport (test injection, or the MCP registry's per-target
// probe client) supplies one; nil selects the package's SSRF-guarded client.
type ClientFactory func(rawURL string) *http.Client

// ValidateFunc checks one absolute URL before it is fetched. Callers with their
// own SSRF policy pass it here; nil selects the package's static guard (the
// dial-time guard still applies to every connection either way).
type ValidateFunc func(ctx context.Context, rawURL string) error

var (
	// ErrURLNotAllowed marks an outbound URL rejected before any request was
	// made — by the package guard or a caller-supplied ValidateFunc.
	ErrURLNotAllowed = errors.New("outbound OAuth URL not allowed")

	// ErrMetadataUnavailable marks an authorization server that publishes no
	// discoverable metadata at any candidate location (every document answered
	// 404/410). A permanent, actionable absence rather than an outage.
	ErrMetadataUnavailable = errors.New("authorization-server metadata is unavailable")

	// ErrNoRegistrationEndpoint marks authorization-server metadata that
	// advertises no dynamic client registration endpoint.
	ErrNoRegistrationEndpoint = errors.New("the authorization server advertises no dynamic client registration endpoint")

	// ErrClientRequired marks a registration attempt the authorization server
	// refused. It is the sentinel behind both a registration-endpoint-free
	// metadata document and a non-2xx registration response, so callers can
	// surface the single actionable remedy: provision a pre-registered client.
	ErrClientRequired = errors.New("the authorization server will not issue a client — dynamic client registration is unavailable or was rejected")
)

// Challenge is the parsed `WWW-Authenticate: Bearer` challenge a protected
// resource returns alongside its 401/403. Only the parameters the connect flow
// needs are extracted; everything else is ignored.
type Challenge struct {
	ResourceMetadataURL string
	AuthorizationURI    string
	Scope               string
}

// ParseBearerChallenge extracts the Bearer challenge parameters from a
// WWW-Authenticate header value. It reports false when the header is not a
// Bearer challenge; missing parameters are simply empty.
//
// The parser accepts both quoted (`key="value"`) and token (`key=value`) forms
// and tolerates extra parameters in any order.
func ParseBearerChallenge(header string) (Challenge, bool) {
	var ch Challenge
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
		value = strings.Trim(strings.TrimSpace(value), `"`)
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

// ProbeConfig is the input for probing a protected resource for its
// authorization challenge.
type ProbeConfig struct {
	// Resource is the protected resource URL (the MCP endpoint).
	Resource string
	// ClientFor overrides the HTTP client; nil selects the SSRF-guarded one.
	ClientFor ClientFactory
	// Validate overrides the pre-fetch URL check; nil selects the package guard.
	Validate ValidateFunc
}

// ProbeResource issues an unauthenticated MCP `initialize` to the resource and
// returns the Bearer challenge it advertises. found is false (with a nil error)
// when the resource did not answer with an OAuth challenge: not every remote
// MCP server is protected.
func ProbeResource(ctx context.Context, cfg ProbeConfig) (challenge Challenge, found bool, err error) {
	resource := strings.TrimSpace(cfg.Resource)
	if resource == "" {
		return Challenge{}, false, fmt.Errorf("mcpauth: probe resource URL is required")
	}
	d := &discovery{clientFor: cfg.ClientFor, validate: cfg.Validate}
	if err := d.check(ctx, resource); err != nil {
		return Challenge{}, false, err
	}

	const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"memory-mcp-oauth-probe","version":"1"}}}`
	httpc := d.client(resource)
	httpc.Timeout = requestTimeout

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resource, strings.NewReader(initBody))
	if err != nil {
		return Challenge{}, false, fmt.Errorf("building probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := httpc.Do(req)
	if err != nil {
		return Challenge{}, false, fmt.Errorf("probing the resource: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		return Challenge{}, false, nil
	}
	ch, ok := ParseBearerChallenge(resp.Header.Get("WWW-Authenticate"))
	if !ok {
		return Challenge{}, false, nil
	}
	return ch, true, nil
}

// Requirement is the resolved "this server needs authorization" fact: the
// authorization server to send the user to, the scopes to request, and the
// endpoints needed to complete the flow later.
type Requirement struct {
	// ResourceMetadataURL is the protected-resource metadata document that was
	// consulted (empty when the resource publishes none).
	ResourceMetadataURL string
	// Resource is the RFC 8707 resource identifier (the protected resource URL).
	Resource string
	// AuthorizationServer is the issuer/base URL of the authorization server.
	AuthorizationServer string
	// AuthorizationEndpoint, TokenEndpoint, RegistrationEndpoint come from the
	// authorization server metadata.
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string
	// Scopes are the scopes to request: explicit caller scopes, else the
	// challenge scope, else the resource's supported scopes, else the
	// authorization server's.
	Scopes []string
	// RegistrationSupported is true when the authorization server offers
	// dynamic client registration.
	RegistrationSupported bool
}

// DiscoveryConfig is the input for resolving a resource's authorization
// requirement.
type DiscoveryConfig struct {
	// Resource is the protected resource URL (the MCP endpoint).
	Resource string
	// Challenge is what the resource's 401/403 advertised, when it advertised
	// anything.
	Challenge Challenge
	// Scopes are the caller's explicit scopes. They win over anything
	// discovered.
	Scopes []string
	// ClientFor overrides the HTTP client; nil selects the SSRF-guarded one.
	ClientFor ClientFactory
	// Validate overrides the pre-fetch URL check; nil selects the package guard.
	Validate ValidateFunc
}

// protectedResourceMetadata is the protected-resource metadata document
// (RFC 9728), the subset a self-registering MCP server advertises.
type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
	BearerMethods        []string `json:"bearer_methods_supported"`
}

// authServerMetadata is the authorization-server metadata document (RFC 8414 /
// OpenID Connect discovery), the subset the flow needs.
type authServerMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// Discover resolves a resource's authorization requirement: the protected
// resource metadata (RFC 9728), then the authorization-server metadata
// (RFC 8414 / OpenID Connect discovery, path-inserted before appended), then
// the scopes to request.
//
// The resource identifier is the protected resource URL unless its metadata
// names a different canonical resource (Figma, for example, serves its metadata
// from a well-known path but identifies the resource as the MCP endpoint).
func Discover(ctx context.Context, cfg DiscoveryConfig) (*Requirement, error) {
	resource := strings.TrimSpace(cfg.Resource)
	if resource == "" {
		return nil, fmt.Errorf("mcpauth: resource URL is required")
	}
	d := &discovery{clientFor: cfg.ClientFor, validate: cfg.Validate}
	if err := d.check(ctx, resource); err != nil {
		return nil, err
	}

	req := &Requirement{
		ResourceMetadataURL: cfg.Challenge.ResourceMetadataURL,
		Resource:            resource,
		Scopes:              splitScope(cfg.Challenge.Scope),
	}
	if len(cfg.Scopes) > 0 {
		req.Scopes = append([]string(nil), cfg.Scopes...)
	}

	// Step 1: protected resource metadata (RFC 9728) names the authorization
	// server(s). A challenge that advertises the document is authoritative: if
	// it cannot be fetched, that is a discovery failure, not a reason to guess.
	// A resource that advertises nothing may still publish the document at the
	// well-known location on its own origin (RFC 9728 §3).
	prm := &protectedResourceMetadata{}
	switch {
	case strings.TrimSpace(req.ResourceMetadataURL) != "":
		if err := d.getJSON(ctx, req.ResourceMetadataURL, prm); err != nil {
			return nil, fmt.Errorf("fetching protected resource metadata: %w", err)
		}
	case resourceOrigin(resource) != "":
		fallback := resourceOrigin(resource) + "/.well-known/oauth-protected-resource"
		if err := d.getJSON(ctx, fallback, prm); err != nil {
			// Absence is not fatal: the challenge may name the server directly.
			prm = &protectedResourceMetadata{}
		} else {
			req.ResourceMetadataURL = fallback
		}
	}
	if r := strings.TrimSpace(prm.Resource); r != "" {
		req.Resource = r
	}
	if len(prm.AuthorizationServers) > 0 && strings.TrimSpace(prm.AuthorizationServers[0]) != "" {
		req.AuthorizationServer = strings.TrimSpace(prm.AuthorizationServers[0])
	}
	if len(req.Scopes) == 0 && len(prm.ScopesSupported) > 0 {
		req.Scopes = append([]string(nil), prm.ScopesSupported...)
	}

	// The challenge may name the authorization server directly when no metadata
	// document does. Prefer the issuer its URL implies; fall back to fetching
	// the document's own issuer field.
	if req.AuthorizationServer == "" && cfg.Challenge.AuthorizationURI != "" {
		req.AuthorizationServer = issuerFromMetadataURL(cfg.Challenge.AuthorizationURI)
		if req.AuthorizationServer == "" {
			md := &authServerMetadata{}
			if err := d.getJSON(ctx, cfg.Challenge.AuthorizationURI, md); err != nil {
				return nil, fmt.Errorf("fetching authorization server metadata: %w", err)
			}
			req.AuthorizationServer = strings.TrimSpace(md.Issuer)
		}
	}
	if req.AuthorizationServer == "" {
		// No server advertised anywhere: discover on the resource's own origin.
		req.AuthorizationServer = resourceOrigin(resource)
	}
	if req.AuthorizationServer == "" {
		return nil, fmt.Errorf("challenge did not identify an authorization server")
	}

	// Step 2: authorization server metadata. RFC 9728's authorization_servers
	// entries are ISSUER identifiers, never metadata URLs — fetching one
	// directly is the Trale #2073 bug. Walk the ordered candidate list and use
	// the first usable document.
	md, err := d.fetchAuthMetadata(ctx, authMetadataCandidates(req.AuthorizationServer, cfg.Challenge.AuthorizationURI))
	if err != nil {
		return nil, err
	}
	req.AuthorizationEndpoint = strings.TrimSpace(md.AuthorizationEndpoint)
	req.TokenEndpoint = strings.TrimSpace(md.TokenEndpoint)
	req.RegistrationEndpoint = strings.TrimSpace(md.RegistrationEndpoint)
	req.RegistrationSupported = req.RegistrationEndpoint != ""
	if len(req.Scopes) == 0 && len(md.ScopesSupported) > 0 {
		req.Scopes = append([]string(nil), md.ScopesSupported...)
	}
	return req, nil
}

// --- URL helpers ---

// splitScope splits a space-delimited scope string, dropping empties.
func splitScope(scope string) []string {
	fields := strings.Fields(scope)
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// issuerFromMetadataURL returns the issuer base URL implied by a well-known
// metadata document URL, or "" when the URL is not a well-known document path.
//
// It handles both the RFC 8414 path form
// (`https://issuer/.well-known/oauth-authorization-server`) and the
// path-inserting form
// (`https://issuer/.well-known/oauth-authorization-server/tenant`).
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

// Well-known document paths. RFC 8414 §3.1 defines the OAuth authorization
// server metadata path; OpenID Connect Discovery 1.0 defines the issuer
// configuration path some authorization servers (e.g. GitHub) serve instead.
const (
	wellKnownOAuthASPath = ".well-known/oauth-authorization-server"
	wellKnownOIDCPath    = ".well-known/openid-configuration"
)

// wellKnownInsert builds a well-known document URL by inserting the well-known
// segment between the host and the issuer's path (RFC 8414 §3.1). A host-only
// issuer yields the familiar append form, while a path-bearing issuer (e.g.
// https://github.com/login/oauth) yields
// https://github.com/.well-known/<segment>/login/oauth.
func wellKnownInsert(issuer, segment string) string {
	u, err := url.Parse(issuer)
	if err != nil {
		return strings.TrimRight(issuer, "/") + "/" + segment
	}
	p := strings.TrimSuffix(u.Path, "/")
	u.Path = "/" + segment + p
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// wellKnownAppend builds the legacy append form: {issuer}/{segment}.
func wellKnownAppend(issuer, segment string) string {
	return strings.TrimRight(issuer, "/") + "/" + segment
}

// authMetadataCandidates returns the ordered, deduplicated list of URLs to try
// when discovering authorization server metadata for an issuer. Order:
// (i) the challenge's authorization_uri when it is itself a well-known metadata
// document (it is authoritative, and may carry a path-inserted tenant the
// derived forms would lose); (ii) RFC 8414 path-insertion; (iii) RFC 8414
// append; (iv) OpenID Connect path-insertion; (v) OpenID Connect append.
//
// The bare issuer is never returned as a candidate.
func authMetadataCandidates(issuer, authorizationURI string) []string {
	issuer = strings.TrimSpace(issuer)
	if strings.Contains(issuer, "/.well-known/") {
		// A deployment may advertise a full metadata URL where an issuer is
		// expected; use it as-is.
		return []string{issuer}
	}
	var out []string
	if authorizationURI != "" && issuerFromMetadataURL(authorizationURI) != "" {
		out = appendUniqueURL(out, authorizationURI)
	}
	for _, candidate := range []string{
		wellKnownInsert(issuer, wellKnownOAuthASPath),
		wellKnownAppend(issuer, wellKnownOAuthASPath),
		wellKnownInsert(issuer, wellKnownOIDCPath),
		wellKnownAppend(issuer, wellKnownOIDCPath),
	} {
		out = appendUniqueURL(out, candidate)
	}
	return out
}

// appendUniqueURL appends a URL only if it is not already present, preserving
// candidate order.
func appendUniqueURL(list []string, candidate string) []string {
	if candidate == "" {
		return list
	}
	for _, existing := range list {
		if existing == candidate {
			return list
		}
	}
	return append(list, candidate)
}

// metadataHTTPError is a definitive HTTP status returned by a metadata endpoint.
type metadataHTTPError struct {
	URL    string
	Status int
}

func (e *metadataHTTPError) Error() string {
	return fmt.Sprintf("metadata endpoint %s returned HTTP %d", e.URL, e.Status)
}

// metadataAbsent reports whether err is a definitive "this document does not
// exist here" answer (404/410) rather than a transient failure.
func metadataAbsent(err error) bool {
	var httpErr *metadataHTTPError
	return errors.As(err, &httpErr) && (httpErr.Status == http.StatusNotFound || httpErr.Status == http.StatusGone)
}

// --- guarded fetch ---

// discovery carries the caller's transport customisation for one operation.
type discovery struct {
	clientFor ClientFactory
	validate  ValidateFunc
}

// check applies the caller's URL check, defaulting to the package's static
// guard. A rejection is always ErrURLNotAllowed so a caller can map it to its
// own sentinel.
func (d *discovery) check(ctx context.Context, rawURL string) error {
	v := d.validate
	if v == nil {
		v = func(_ context.Context, raw string) error { return validateURL(raw) }
	}
	if err := v(ctx, rawURL); err != nil {
		return fmt.Errorf("%w: %v", ErrURLNotAllowed, err)
	}
	return nil
}

// client returns the HTTP client for one URL: the caller's when supplied, the
// SSRF-guarded per-target client otherwise.
func (d *discovery) client(rawURL string) *http.Client {
	if d.clientFor != nil {
		if c := d.clientFor(rawURL); c != nil {
			return c
		}
	}
	return OutboundHTTPClient(rawURL)
}

// getJSON performs a guarded GET and decodes a JSON document into out.
func (d *discovery) getJSON(ctx context.Context, rawURL string, out any) error {
	if err := d.check(ctx, rawURL); err != nil {
		return err
	}
	client := d.client(rawURL)
	client.Timeout = requestTimeout

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
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))

	if resp.StatusCode != http.StatusOK {
		return &metadataHTTPError{URL: rawURL, Status: resp.StatusCode}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding metadata document: %w", err)
	}
	return nil
}

// fetchAuthMetadata fetches the authorization server metadata from the first
// candidate URL that returns a usable document. It returns
// ErrMetadataUnavailable when every candidate answers with a definitive "not
// found" (404/410), and otherwise a transient failure summary naming the
// candidates (a network error, timeout, 5xx, 401/403, or a document that omits
// an endpoint).
func (d *discovery) fetchAuthMetadata(ctx context.Context, candidates []string) (*authServerMetadata, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: no candidate metadata URL could be derived", ErrMetadataUnavailable)
	}
	var failures []string
	allAbsent := true
	for _, candidate := range candidates {
		md := &authServerMetadata{}
		err := d.getJSON(ctx, candidate, md)
		if err == nil {
			if strings.TrimSpace(md.AuthorizationEndpoint) == "" || strings.TrimSpace(md.TokenEndpoint) == "" {
				err = errors.New("document is missing the authorization/token endpoint")
			} else {
				return md, nil
			}
		}
		if !metadataAbsent(err) {
			allAbsent = false
		}
		failures = append(failures, fmt.Sprintf("%s -> %s", candidate, failureReason(err)))
	}
	detail := strings.Join(failures, "; ")
	if allAbsent {
		return nil, fmt.Errorf("%w: authorization-server metadata is absent at every candidate (%s)", ErrMetadataUnavailable, detail)
	}
	return nil, fmt.Errorf("authorization-server metadata: all discovery endpoints failed: %s", detail)
}

// failureReason reduces a candidate failure to a compact outcome for the
// aggregate failure message: the HTTP status when there was one, otherwise a
// short marker (network error, undecodable document, missing endpoints).
func failureReason(err error) string {
	var statusErr *metadataHTTPError
	if errors.As(err, &statusErr) {
		return fmt.Sprintf("%d", statusErr.Status)
	}
	return "error"
}

// --- dynamic client registration ---

// Identity is the client material registered (or pre-configured) for a server.
// A public client has no secret; a pre-registered confidential client
// authenticates with HTTP basic auth.
type Identity struct {
	ClientID     string
	ClientSecret string
}

// RegistrationRejected reports a dynamic client registration the authorization
// server refused (e.g. Figma's 403 for catalog-only clients). It carries the
// HTTP status and a short, sanitized excerpt of the response body for
// diagnosis, and unwraps to ErrClientRequired so the caller can surface the
// single actionable remedy: provision a pre-registered client.
type RegistrationRejected struct {
	Status  int
	Excerpt string
}

func (e *RegistrationRejected) Error() string {
	msg := fmt.Sprintf("dynamic client registration failed with HTTP %d", e.Status)
	if e.Excerpt != "" {
		msg += ": " + e.Excerpt
	}
	return msg
}

func (e *RegistrationRejected) Unwrap() error { return ErrClientRequired }

// excerptResponseBody returns a short, sanitized excerpt of a response body for
// inclusion in a diagnostic error. It caps the length, strips invalid UTF-8, and
// collapses whitespace so no binary payload or control character can leak into a
// message. A registration request never contains a secret (token_endpoint_auth_method
// is "none"), so the rejection body cannot echo one.
func excerptResponseBody(body []byte) string {
	const maxExcerpt = 256
	s := string(body)
	if len(s) > maxExcerpt {
		s = s[:maxExcerpt] + "…"
	}
	s = strings.ToValidUTF8(s, "")
	return strings.Join(strings.Fields(s), " ")
}

// RegistrationConfig is the input for RFC 7591 dynamic client registration.
type RegistrationConfig struct {
	// RegistrationEndpoint comes from the authorization-server metadata.
	RegistrationEndpoint string
	// ClientName is the RFC 7591 client_name submitted.
	ClientName string
	// RedirectURI is the OAuth callback the AS redirects to.
	RedirectURI string
	// Scopes is the scope set requested at registration (optional).
	Scopes []string
	// ClientFor overrides the HTTP client; nil selects the SSRF-guarded one.
	ClientFor ClientFactory
	// Validate overrides the pre-fetch URL check; nil selects the package guard.
	Validate ValidateFunc
}

// Register performs RFC 7591 dynamic client registration, returning the issued
// client identity. Public clients (no secret) are requested by default, which
// is what a PKCE-driven browser flow needs.
func Register(ctx context.Context, cfg RegistrationConfig) (*Identity, error) {
	endpoint := strings.TrimSpace(cfg.RegistrationEndpoint)
	if endpoint == "" {
		return nil, ErrNoRegistrationEndpoint
	}
	body := map[string]any{
		"client_name":                cfg.ClientName,
		"redirect_uris":              []string{cfg.RedirectURI},
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	}
	if len(cfg.Scopes) > 0 {
		body["scope"] = strings.Join(cfg.Scopes, " ")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding registration request: %w", err)
	}

	d := &discovery{clientFor: cfg.ClientFor, validate: cfg.Validate}
	if err := d.check(ctx, endpoint); err != nil {
		return nil, err
	}
	client := d.client(endpoint)
	client.Timeout = requestTimeout

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
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
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, &RegistrationRejected{Status: resp.StatusCode, Excerpt: excerptResponseBody(respBody)}
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
		return nil, fmt.Errorf("%w: dynamic client registration rejected: %s", ErrClientRequired, out.Error)
	}
	if strings.TrimSpace(out.ClientID) == "" {
		return nil, fmt.Errorf("dynamic client registration returned no client_id")
	}
	return &Identity{ClientID: out.ClientID, ClientSecret: out.ClientSecret}, nil
}

// --- authorization request and token endpoint ---

// AuthorizeURL constructs the authorization request URL, including the RFC 8707
// resource indicator and the PKCE challenge. Parameters already present on the
// endpoint URL are preserved.
func AuthorizeURL(req *Requirement, identity *Identity, redirectURI, state, codeChallenge string) (string, error) {
	if req == nil || identity == nil {
		return "", fmt.Errorf("mcpauth: authorization requirement and client identity are required")
	}
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

// tokenResponse is the RFC 6749 token endpoint response.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`

	// GoTrue answers with its own envelope instead of the RFC 6749 one: no
	// `error` field, the reason in `error_code` and the text in `msg`. Trale's
	// authorization server is Supabase, so a refusal there carries no `error` at
	// all and is only decodable through these.
	ErrorCode string `json:"error_code"`
	Msg       string `json:"msg"`
}

// Rejection is a token-endpoint response that refused the request: the
// authorization server answered, so the grant or the client registration was
// rejected — as opposed to the endpoint being unavailable (a transport failure
// or a 5xx), which leaves whatever was presented intact. Callers use it to tell
// a dead credential (reconnect) from a transient outage (retry).
type Rejection struct {
	StatusCode  int
	Code        string
	Description string
}

func (r *Rejection) Error() string {
	switch {
	case r.Code != "" && r.Description != "":
		return fmt.Sprintf("token endpoint rejected the request: %s: %s", r.Code, r.Description)
	case r.Code != "":
		return "token endpoint rejected the request: " + r.Code
	default:
		return fmt.Sprintf("token endpoint rejected the request: token endpoint returned HTTP %d", r.StatusCode)
	}
}

// requestShapeCodes are the token-endpoint codes that say the *request* was
// malformed rather than the credential refused: an unknown grant type, a missing
// or unexpected resource indicator, a scope the client may not ask for. They are
// our bug, and reporting them as a dead credential would stop a working
// connection until someone reconnected it.
var requestShapeCodes = map[string]struct{}{
	"invalid_request":        {},
	"invalid_target":         {},
	"unsupported_grant_type": {},
	"invalid_scope":          {},
}

// IsCredentialRejection reports whether err is the authorization server refusing
// what was presented — the grant, or the client registration the grant belongs
// to — as opposed to the endpoint being unavailable. It is the one predicate
// callers should use to decide between "the credential is dead, ask the user to
// reconnect" and "this failure clears on its own, retry".
//
// Two 4xx answers are still transient and must not count: 429 (rate limited — the
// credential is fine, and Supabase/GoTrue answers `over_request_rate_limit`) and
// 408 (the request timed out at the server). Parking either would silently stop a
// healthy connection for a condition that clears. A request-shape code (see
// requestShapeCodes) is likewise not a rejection. Everything else the server
// answers with a 4xx is a refusal of what was presented; a transport failure or a
// 5xx leaves it intact.
func IsCredentialRejection(err error) bool {
	var rejection *Rejection
	if !errors.As(err, &rejection) {
		return false
	}
	if rejection.StatusCode < 400 || rejection.StatusCode >= 500 {
		return false
	}
	switch rejection.StatusCode {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return false
	}
	if _, ok := requestShapeCodes[strings.ToLower(rejection.Code)]; ok {
		return false
	}
	return true
}

// TokenClient redeems and refreshes tokens for one registered client. The
// identity follows the registration: a client with a secret authenticates with
// HTTP basic auth (client_secret_basic, the RFC 6749 preferred method), and a
// public client sends only its client_id in the body.
type TokenClient struct {
	TokenEndpoint string
	Resource      string
	Identity      Identity
	// ClientFor overrides the HTTP client; nil selects the SSRF-guarded one.
	ClientFor ClientFactory
	// Validate overrides the pre-fetch URL check; nil selects the package guard.
	Validate ValidateFunc
}

// Exchange redeems an authorization code for a token set (RFC 6749 §4.1.3),
// carrying the PKCE verifier and the resource indicator.
func (c *TokenClient) Exchange(ctx context.Context, code, codeVerifier, redirectURI string) (*Tokens, error) {
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("mcpauth: authorization code is empty")
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}
	return c.postToken(ctx, form)
}

// Refresh exchanges a refresh token for a new token set (RFC 6749 §6, public
// client or basic auth, resource indicator).
func (c *TokenClient) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return nil, fmt.Errorf("mcpauth: refresh token is empty")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	return c.postToken(ctx, form)
}

// postToken performs a token endpoint form POST and decodes the token set.
func (c *TokenClient) postToken(ctx context.Context, form url.Values) (*Tokens, error) {
	endpoint := strings.TrimSpace(c.TokenEndpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("token endpoint is unknown")
	}
	d := &discovery{clientFor: c.ClientFor, validate: c.Validate}
	if err := d.check(ctx, endpoint); err != nil {
		return nil, err
	}

	body := url.Values{}
	for k, v := range form {
		body[k] = v
	}
	if c.Resource != "" {
		body.Set("resource", c.Resource)
	}
	if c.Identity.ClientSecret == "" {
		body.Set("client_id", c.Identity.ClientID)
	}

	client := d.client(endpoint)
	client.Timeout = requestTimeout

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, fmt.Errorf("building token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")
	if c.Identity.ClientSecret != "" {
		httpReq.SetBasicAuth(c.Identity.ClientID, c.Identity.ClientSecret)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))

	var out tokenResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || out.Error != "" || out.ErrorCode != "" {
		code := out.Error
		if code == "" {
			code = out.ErrorCode
		}
		desc := out.ErrorDesc
		if desc == "" {
			desc = out.Msg
		}
		return nil, &Rejection{StatusCode: resp.StatusCode, Code: code, Description: desc}
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return nil, fmt.Errorf("token endpoint returned no access_token")
	}
	return &Tokens{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ExpiresIn:    out.ExpiresIn,
		TokenType:    out.TokenType,
		IDToken:      out.IDToken,
		Scope:        out.Scope,
	}, nil
}
