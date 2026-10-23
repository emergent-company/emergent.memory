package mcpauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// protectedResourceMetadata is the OAuth 2.0 Protected Resource Metadata
// document (RFC 9728), the subset Trale's remote MCP server advertises.
type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
	BearerMethods        []string `json:"bearer_methods_supported"`
}

// asMetadata is the authorization-server metadata (RFC 8414 / OpenID
// discovery) subset the flow needs.
type asMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	GrantTypesSupported   []string `json:"grant_types_supported"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// discoverProtectedResource resolves the protected-resource metadata. Primary
// path: probe the resource for a 401 Bearer challenge advertising
// `resource_metadata`. Fallback: the well-known endpoint on the resource origin.
func discoverProtectedResource(ctx context.Context, httpc *http.Client, resource string) (*protectedResourceMetadata, error) {
	if metaURL := probeResourceChallenge(ctx, httpc, resource); metaURL != "" {
		if prm, err := fetchProtectedResourceMetadata(ctx, httpc, metaURL); err == nil {
			return prm, nil
		}
	}
	return fetchProtectedResourceMetadata(ctx, httpc, resourceOrigin(resource)+"/.well-known/oauth-protected-resource")
}

// probeResourceChallenge issues an unauthenticated MCP initialize to the
// resource and, on a 401 with a Bearer challenge, returns the advertised
// resource_metadata URL (else "").
func probeResourceChallenge(ctx context.Context, httpc *http.Client, resource string) string {
	const init = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"emergent","version":"1.0.0"}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resource, strings.NewReader(init))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := httpc.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))
	if resp.StatusCode != http.StatusUnauthorized {
		return ""
	}
	return parseResourceMetadataURL(resp.Header.Get("WWW-Authenticate"))
}

// parseResourceMetadataURL extracts the resource_metadata parameter from a
// Bearer WWW-Authenticate challenge: `Bearer resource_metadata="<url>"`.
func parseResourceMetadataURL(challenge string) string {
	challenge = strings.TrimSpace(challenge)
	if !strings.HasPrefix(challenge, "Bearer") {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(challenge, "Bearer"))
	for _, part := range splitChallengeParams(rest) {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "resource_metadata") {
			return strings.Trim(strings.TrimSpace(val), `"`)
		}
	}
	return ""
}

// splitChallengeParams splits a WWW-Authenticate parameter list on commas that
// are not inside a quoted string.
func splitChallengeParams(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch r {
		case '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case ',':
			if inQuote {
				cur.WriteRune(r)
			} else {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// fetchProtectedResourceMetadata fetches and decodes a protected-resource
// metadata document.
func fetchProtectedResourceMetadata(ctx context.Context, httpc *http.Client, metaURL string) (*protectedResourceMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata endpoint returned %d", resp.StatusCode)
	}
	var prm protectedResourceMetadata
	if err := json.Unmarshal(body, &prm); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}
	return &prm, nil
}

// fetchASMetadata fetches and decodes the authorization-server metadata
// document (RFC 8414 / OpenID discovery).
func fetchASMetadata(ctx context.Context, httpc *http.Client, metaURL string) (*asMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if resp.StatusCode != http.StatusOK {
		return nil, &asMetadataStatusError{status: resp.StatusCode}
	}
	var as asMetadata
	if err := json.Unmarshal(body, &as); err != nil {
		return nil, fmt.Errorf("decode authorization-server metadata: %w", err)
	}
	if strings.TrimSpace(as.AuthorizationEndpoint) == "" || strings.TrimSpace(as.TokenEndpoint) == "" {
		return nil, fmt.Errorf("authorization-server metadata is missing endpoints")
	}
	return &as, nil
}

// asMetadataStatusError records the HTTP status of a metadata endpoint that
// answered with a non-200. discoverASMetadata uses it to report which candidate
// returned which status in the aggregate failure.
type asMetadataStatusError struct{ status int }

func (e *asMetadataStatusError) Error() string {
	return fmt.Sprintf("authorization-server metadata endpoint returned %d", e.status)
}

// asMetadataCandidates derives candidate authorization-server metadata URLs from
// an issuer identifier. RFC 9728's authorization_servers entries are ISSUER
// identifiers, not metadata URLs; fetching one directly is the Trale bug
// (Supabase answers 401). RFC 8414 §3.1 defines the path-insertion rule: an
// issuer with a path component is discovered at
// `/.well-known/oauth-authorization-server<path>`, so that is tried first. The
// OpenID locations (and their path-suffix variants) follow because deployments
// differ (Supabase serves `<issuer>/.well-known/openid-configuration`).
//
// The bare issuer is never returned as a candidate.
func asMetadataCandidates(issuer string) []string {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		return nil
	}
	// A deployment may already advertise a full metadata URL.
	if strings.Contains(issuer, "/.well-known/") {
		return []string{issuer}
	}

	u, err := url.Parse(issuer)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil
	}
	base := u.Scheme + "://" + u.Host
	path := strings.TrimSuffix(u.Path, "/")

	var candidates []string
	if path == "" {
		candidates = []string{
			base + "/.well-known/oauth-authorization-server",
			base + "/.well-known/openid-configuration",
		}
	} else {
		candidates = []string{
			// RFC 8414 §3.1 path insertion — the location Supabase/Trale uses.
			base + "/.well-known/oauth-authorization-server" + path,
			base + path + "/.well-known/oauth-authorization-server",
			// OpenID discovery.
			base + path + "/.well-known/openid-configuration",
			base + "/.well-known/openid-configuration" + path,
		}
	}

	// Dedupe while preserving order.
	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

// maxFailuresReported bounds how many candidate outcomes are named in a total
// discovery failure so the error cannot balloon.
const maxFailuresReported = 4

// discoverASMetadata resolves authorization-server metadata from an issuer
// identifier by probing the RFC 8414 / OpenID discovery candidates derived from
// that issuer. Candidates derive solely from the advertised issuer (or the
// resource origin fallback), and every request still goes through the guarded
// httpc (OutboundHTTPClient unless a caller injects a transport), so the SSRF
// posture, redirect handling and body limiting are unchanged.
func discoverASMetadata(ctx context.Context, httpc *http.Client, issuer string) (*asMetadata, error) {
	candidates := asMetadataCandidates(issuer)
	var failures []string
	for _, candidate := range candidates {
		as, err := fetchASMetadata(ctx, httpc, candidate)
		if err == nil {
			return as, nil
		}
		failures = append(failures, fmt.Sprintf("%s -> %s", candidate, asFailureReason(err)))
	}
	if len(failures) == 0 {
		return nil, fmt.Errorf("no authorization-server discovery candidates derivable from issuer %q", issuer)
	}
	if len(failures) > maxFailuresReported {
		failures = failures[:maxFailuresReported]
	}
	return nil, fmt.Errorf("all discovery endpoints failed: %s", strings.Join(failures, "; "))
}

// asFailureReason reduces a fetchASMetadata error to a compact per-endpoint
// outcome for the aggregate failure message: the HTTP status when known,
// otherwise a short marker.
func asFailureReason(err error) string {
	var statusErr *asMetadataStatusError
	if errors.As(err, &statusErr) {
		return strconv.Itoa(statusErr.status)
	}
	return "error"
}

// registerClient performs RFC 7591 dynamic client registration as a public
// client (no secret), returning the assigned client_id.
func registerClient(ctx context.Context, httpc *http.Client, regURL string, cfg Config) (string, error) {
	if strings.TrimSpace(regURL) == "" {
		return "", fmt.Errorf("authorization server advertises no registration endpoint")
	}
	body, err := json.Marshal(map[string]any{
		"client_name":                cfg.ClientName,
		"redirect_uris":              []string{cfg.RedirectURI},
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"scope":                      strings.Join(cfg.Scopes, " "),
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, regURL, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registration endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var reg struct {
		ClientID         string `json:"client_id"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(respBody, &reg); err != nil {
		return "", fmt.Errorf("decode registration response: %w", err)
	}
	if reg.Error != "" {
		if reg.ErrorDescription != "" {
			return "", fmt.Errorf("registration failed: %s: %s", reg.Error, reg.ErrorDescription)
		}
		return "", fmt.Errorf("registration failed: %s", reg.Error)
	}
	if strings.TrimSpace(reg.ClientID) == "" {
		return "", fmt.Errorf("registration response carried no client_id")
	}
	return reg.ClientID, nil
}

// tokenForm builds a token-request form with the client_id and resource already
// set (both are present on every Trale token request; no client secret).
func tokenForm(grantType string) url.Values {
	form := url.Values{}
	form.Set("grant_type", grantType)
	return form
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
}

// postToken POSTs a token form and decodes the response, mapping protocol
// errors to Go errors.
func postToken(ctx context.Context, httpc *http.Client, tokenURL, clientID, resource string, form url.Values) (*Tokens, error) {
	if strings.TrimSpace(tokenURL) == "" {
		return nil, fmt.Errorf("token endpoint is empty")
	}
	form.Set("client_id", clientID)
	if resource != "" {
		form.Set("resource", resource)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))

	var payload tokenResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || payload.Error != "" {
		msg := payload.Error
		if payload.ErrorDesc != "" {
			msg = payload.Error + ": " + payload.ErrorDesc
		}
		if msg == "" {
			msg = fmt.Sprintf("token endpoint returned %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("token exchange failed: %s", msg)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return nil, fmt.Errorf("token response carried no access token")
	}

	return &Tokens{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		ExpiresIn:    payload.ExpiresIn,
		TokenType:    payload.TokenType,
		IDToken:      payload.IDToken,
		Scope:        payload.Scope,
	}, nil
}

// parseURL parses a URL string, tolerating the absence of a scheme for
// derivation purposes (never used for actual requests).
func parseURL(raw string) (*url.URL, error) {
	return url.Parse(raw)
}
