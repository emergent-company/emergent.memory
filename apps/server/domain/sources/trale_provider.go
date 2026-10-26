package sources

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/mcpauth"
)

// Trale's remote MCP server and authorization server. The authorization and
// token endpoints are discovered at connect time (RFC 8414) and these constants
// are the fallback defaults; the resource is fixed.
const (
	traleResource   = "https://app.trale.ai/api/mcp"
	traleAuthURL    = "https://lnzymgzwwmdgbosweuih.supabase.co/auth/v1/oauth/authorize"
	traleTokenURL   = "https://lnzymgzwwmdgbosweuih.supabase.co/auth/v1/oauth/token"
	traleClientName = "Memory"
)

// traleScopes are the OIDC scopes Trale's authorization server supports; they
// identify the connecting user and gate the MCP tools the token may call.
var traleScopes = []string{"openid", "email", "profile"}

// traleCredentialKeyNames are the config keys that carry Trale credential
// material. They are write-only: stripped from the persisted config and never
// returned. access_token/refresh_token/expires_at are the OAuth token set;
// client_id/token_endpoint/resource identify the dynamically-registered client
// so the connector can refresh without a stored secret (Trale registers a
// public client).
var traleCredentialKeyNames = []string{
	"access_token", "refresh_token", "expires_at", "client_id", "token_endpoint", "resource", "scope",
}

// traleProvider implements SourceProvider for the trale provider: it syncs
// meeting intelligence (transcripts/summaries) from Trale via its remote MCP
// server, connected with OAuth 2.1 + dynamic client registration.
type traleProvider struct {
	// httpClient is the transport used for MCP calls and token refresh. When
	// nil (production), the connector builds an SSRF-guarded client per resource.
	// Tests inject a plain client so httptest servers on loopback are reachable.
	httpClient *http.Client
	// resource is the protected-resource URL the provider discovers against and
	// connects to. Defaults to traleResource; tests point it at an httptest
	// server so discovery/DCR/token requests stay on loopback.
	resource string
}

func newTraleProvider() *traleProvider { return &traleProvider{resource: traleResource} }

// NewTraleProvider builds the Trale provider with an injectable HTTP client. It
// exists so tests can exercise the connector against an httptest MCP server
// without the SSRF guard blocking loopback.
func NewTraleProvider(httpClient *http.Client) SourceProvider {
	return &traleProvider{httpClient: httpClient, resource: traleResource}
}

func (p *traleProvider) Catalog() Provider {
	return Provider{
		Type:            ProviderTypeTrale,
		Name:            "Trale",
		Description:     "Sync meeting transcripts and summaries from Trale into your knowledge base.",
		SourceType:      SourceTypeMeeting,
		Vendor:          "trale",
		VendorName:      "Trale",
		Available:       true,
		SelfRegistering: true,
	}
}

// ConfigSchema is the user-facing scope control. tags is required (the user
// explicitly chooses which meeting types to sync — never "everything"); the
// rest are optional and default to a filterless backfill of the last 90 days.
func (p *traleProvider) ConfigSchema() ProviderSchema {
	return ProviderSchema{
		Type: ProviderTypeTrale,
		Properties: map[string]any{
			"tags": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Trale tag slugs to sync (ANY match). Meetings tagged with any of these are imported.",
				"x-ui-widget": "resource",
			},
			"name_pattern": map[string]any{
				"type":        "string",
				"description": "Optional Go RE2 regular expression matched against the meeting title; non-matching meetings are skipped.",
			},
			"include_transcript": map[string]any{
				"type":        "boolean",
				"description": "Include the speaker-tagged transcript in the imported document. Default false (summary only).",
			},
			"backfill_days": map[string]any{
				"type":        "integer",
				"description": "How many days back to sync. Default 90.",
			},
		},
		Required: []string{"tags"},
	}
}

// CredentialFields is empty: Trale connect is OAuth-only and self-registering,
// so there is no pasted-credential surface.
func (p *traleProvider) CredentialFields() []ProviderField { return nil }

func (p *traleProvider) AuthMethods() []AuthMethod {
	return []AuthMethod{AuthMethodOAuth}
}

func (p *traleProvider) OAuth() (OAuthStrategy, bool) {
	return newTraleOAuthStrategy(), true
}

// DiscoverOAuth implements DynamicOAuthProvider: discover the protected-resource
// and authorization-server metadata, then register a public client via RFC 7591.
func (p *traleProvider) DiscoverOAuth(ctx context.Context, httpClient *http.Client, redirectURI string) (DynamicOAuthClient, error) {
	resource := p.resource
	if strings.TrimSpace(resource) == "" {
		resource = traleResource
	}
	c, err := mcpauth.DiscoverAndRegister(ctx, mcpauth.Config{
		Resource:    resource,
		ClientName:  traleClientName,
		Scopes:      traleScopes,
		RedirectURI: redirectURI,
		HTTPClient:  httpClient,
	})
	if err != nil {
		return DynamicOAuthClient{}, err
	}
	return DynamicOAuthClient{
		ClientID: c.ClientID,
		AuthURL:  c.AuthURL,
		TokenURL: c.TokenURL,
		Resource: c.Resource,
		Scopes:   c.Scopes,
	}, nil
}

func (p *traleProvider) CredentialKeys() []string { return traleCredentialKeyNames }

func (p *traleProvider) ValidateConfig(cfg map[string]any) error {
	return validateTraleConfig(cfg)
}

func (p *traleProvider) ExtractCredentials(cfg map[string]any) (Credentials, error) {
	creds := Credentials{}
	for _, k := range traleCredentialKeyNames {
		if v, ok := cfg[k]; ok {
			creds[k] = v
		}
	}
	if token, _ := creds["access_token"].(string); strings.TrimSpace(token) == "" {
		return nil, apperror.NewValidation("config.access_token is required")
	}
	return creds, nil
}

// ListResources lists the Trale tags the connecting user can see as sync-scope
// candidates (the tag picker backs the required `tags` config).
func (p *traleProvider) ListResources(ctx context.Context, creds Credentials, _ map[string]any, _ string, deps ConnectorDeps) ([]Resource, bool, error) {
	tc, err := parseTraleCredentials(creds)
	if err != nil {
		return nil, false, err
	}
	// The tag picker can refresh an expired token, so it takes the same
	// write-back the sync worker uses: an unpersisted rotation would strand the
	// source at the next sync (#2091).
	conn := newTraleConnector(tc, p.httpClient, deps)
	tags, err := conn.listTags(ctx)
	if err != nil {
		return nil, false, apperror.NewValidation("list tags failed: " + err.Error())
	}
	out := make([]Resource, 0, len(tags))
	for _, t := range tags {
		out = append(out, Resource{ID: t.Slug, Name: t.Name, Type: "tag"})
	}
	return out, false, nil
}

func (p *traleProvider) NewConnector(ctx context.Context, creds Credentials, deps ConnectorDeps) (Connector, error) {
	tc, err := parseTraleCredentials(creds)
	if err != nil {
		return nil, err
	}
	return newTraleConnector(tc, p.httpClient, deps), nil
}

// ---- Trale OAuth strategy ----

// traleOAuthStrategy implements OAuthStrategy for trale. It is a public client:
// the authorize URL carries a resource parameter (RFC 8707) and the exchange
// posts no client secret.
type traleOAuthStrategy struct {
	resource string
}

func newTraleOAuthStrategy() *traleOAuthStrategy {
	return &traleOAuthStrategy{resource: traleResource}
}

func (s *traleOAuthStrategy) DefaultScopes() []string { return traleScopes }

func (s *traleOAuthStrategy) AuthorizeURL(client OAuthClient, req OAuthAuthorizeRequest) string {
	authURL := client.AuthURL
	if authURL == "" {
		authURL = traleAuthURL
	}
	scopes := req.Scopes
	if len(scopes) == 0 {
		scopes = client.Scopes
	}
	if len(scopes) == 0 {
		scopes = s.DefaultScopes()
	}
	resource := client.Resource
	if resource == "" {
		resource = s.resource
	}

	q := url.Values{}
	q.Set("client_id", client.ClientID)
	q.Set("redirect_uri", req.RedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(scopes, " "))
	q.Set("state", req.State)
	q.Set("code_challenge", req.CodeChallenge)
	q.Set("code_challenge_method", "S256")
	// RFC 8707: name the protected resource so the token is scoped to the MCP
	// endpoint, not the user's whole account.
	q.Set("resource", resource)
	for k, v := range req.Extra {
		q.Set(k, v)
	}
	return authURL + "?" + q.Encode()
}

func (s *traleOAuthStrategy) Exchange(ctx context.Context, httpClient *http.Client, client OAuthClient, code, codeVerifier, redirectURI string) (Credentials, error) {
	tokenURL := client.TokenURL
	if tokenURL == "" {
		tokenURL = traleTokenURL
	}
	resource := client.Resource
	if resource == "" {
		resource = s.resource
	}
	// A nil client makes mcpauth build its SSRF-guarded, timeout-bounded client;
	// tests inject their own, and a non-nil default here would silently bypass the
	// guard (the token endpoint comes from server-controlled metadata).
	mc := mcpauth.NewClient(client.ClientID, tokenURL, resource, httpClient)
	tok, err := mc.Exchange(ctx, code, codeVerifier, redirectURI)
	if err != nil {
		return nil, err
	}

	creds := Credentials{
		"access_token":   tok.AccessToken,
		"refresh_token":  tok.RefreshToken,
		"client_id":      client.ClientID,
		"token_endpoint": tokenURL,
		"resource":       resource,
	}
	if tok.Scope != "" {
		creds["scope"] = tok.Scope
	}
	if exp := tok.ExpiresAt(time.Now().UTC()); !exp.IsZero() {
		creds["expires_at"] = exp.Format(time.RFC3339)
	}
	return creds, nil
}
