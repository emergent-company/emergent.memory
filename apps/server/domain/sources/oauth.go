package sources

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

// OAuthClient is the resolved OAuth2 client material a provider needs to build
// an authorization URL and exchange an authorization code. The client id/secret
// come from org or deployment config (D3); the same struct is provider-agnostic.
type OAuthClient struct {
	ClientID     string
	ClientSecret string
	// Scopes override the provider default when non-empty.
	Scopes []string
	// Strategy is the resolved scope strategy name (the project row's, an
	// inherited org strategy, or "" when the provider default applies). It is
	// informational: Scopes is what the authorize request uses.
	Strategy string
	// AuthURL/TokenURL override the provider defaults (used by tests).
	AuthURL  string
	TokenURL string
	// Resource is the optional RFC 8707 resource parameter identifying the
	// protected resource (e.g. an MCP endpoint). Static-client providers
	// (Google/GitHub) leave it empty and are unaffected; dynamic-client
	// providers (Trale) carry it so authorize and token requests target the
	// right resource.
	Resource string
	// Config carries the winning row's non-secret provider extras (e.g. the
	// Google Picker developer key/app id). It is empty for a deployment default.
	Config map[string]any
}

// OAuthAuthorizeRequest is the per-connection input for building an authorize URL.
type OAuthAuthorizeRequest struct {
	State         string
	CodeChallenge string
	RedirectURI   string
	Scopes        []string
	// Extra carries provider-specific authorize params (e.g. Google's
	// access_type=offline & prompt=consent to force a refresh token).
	Extra map[string]string
}

// OAuthStrategy is a provider's OAuth2 protocol. The connect flow is generic;
// only the strategy differs per provider.
type OAuthStrategy interface {
	// DefaultScopes is the scope set requested when neither the client nor the
	// request specifies one.
	DefaultScopes() []string
	// AuthorizeURL builds the provider's authorization URL.
	AuthorizeURL(client OAuthClient, req OAuthAuthorizeRequest) string
	// Exchange redeems an authorization code (with PKCE verifier) for a
	// credential map, using the supplied HTTP client (injectable for tests).
	Exchange(ctx context.Context, httpClient *http.Client, client OAuthClient, code, codeVerifier, redirectURI string) (Credentials, error)
}

// DynamicOAuthClient is the result of a provider's dynamic (self-registering)
// OAuth bootstrap: the registered client id, the authorization/token endpoints,
// the protected resource, and the requested scopes. It has no secret — the
// client registers as a public client (token_endpoint_auth_method "none").
type DynamicOAuthClient struct {
	ClientID string
	AuthURL  string
	TokenURL string
	Resource string
	Scopes   []string
}

// DynamicOAuthProvider is implemented by a provider whose OAuth client is
// registered on the fly (RFC 7591 dynamic client registration) rather than
// configured by an admin. When a provider implements it, the connect flow skips
// resolveOAuthClient and instead discovers + registers a client, persisting its
// identity/endpoints in the pending state until the callback consumes them.
type DynamicOAuthProvider interface {
	DiscoverOAuth(ctx context.Context, httpClient *http.Client, redirectURI string) (DynamicOAuthClient, error)
}

// oauthDynamicKey is the reserved pending-state config key under which a
// dynamic provider's discovered client identity/endpoints are persisted between
// StartOAuth and CompleteOAuth. It is stripped at callback time and never lands
// in the stored source config.
const oauthDynamicKey = "_oauth_dynamic"

// dynamicOAuthToConfig serialises a discovered dynamic client into the
// pending-state config shape (a plain JSON object).
func dynamicOAuthToConfig(dc DynamicOAuthClient) map[string]any {
	return map[string]any{
		"client_id": dc.ClientID,
		"auth_url":  dc.AuthURL,
		"token_url": dc.TokenURL,
		"resource":  dc.Resource,
		"scopes":    dc.Scopes,
	}
}

// dynamicOAuthFromConfig reconstructs a discovered dynamic client from a
// pending-state config value. The value arrives as map[string]any (JSONB read)
// or []string/[]any for scopes depending on the store round-trip.
func dynamicOAuthFromConfig(v any) (DynamicOAuthClient, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return DynamicOAuthClient{}, false
	}
	dc := DynamicOAuthClient{
		ClientID: asString(m["client_id"]),
		AuthURL:  asString(m["auth_url"]),
		TokenURL: asString(m["token_url"]),
		Resource: asString(m["resource"]),
		Scopes:   stringSlice(m["scopes"]),
	}
	if strings.TrimSpace(dc.ClientID) == "" {
		return DynamicOAuthClient{}, false
	}
	return dc, true
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// NewOAuthState returns a URL-safe random state value for CSRF protection.
func NewOAuthState() (string, error) { return randToken(32) }

// NewCodeVerifier returns a PKCE code verifier (RFC 7636: 43-128 unreserved
// characters; here 43 from 32 random bytes).
func NewCodeVerifier() (string, error) { return randToken(32) }

// CodeChallengeS256 returns the S256 PKCE challenge for a verifier.
func CodeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sources: random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ---- Google Drive OAuth2 strategy ----

const (
	googleAuthURL   = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL  = "https://oauth2.googleapis.com/token"
	googleDriveFile = "https://www.googleapis.com/auth/drive.file"
	// googleDriveReadonly is the provider default scope. It lets the app list the
	// user's full drive; it may require Google verification for a shared app.
	googleDriveReadonly = "https://www.googleapis.com/auth/drive.readonly"
)

// Org-selectable Drive scope strategies (design D6). drive.readonly is the
// default because it is the only strategy that can list the drive: under
// drive.file, Google's files.list returns only files the app created or the user
// picked, and the Google Picker — the intended companion to drive.file — does not
// exist yet. drive.file stays selectable for deployments that will add Picker
// support.
const (
	ScopeStrategyDriveFile     ScopeStrategy = "drive.file"
	ScopeStrategyDriveReadonly ScopeStrategy = "drive.readonly"
)

// driveScopeStrategies lists the Drive strategies in display order: the default
// (drive.readonly) first, so a client with no stored strategy selects the
// default in the form rather than the scope that cannot list the drive. Both
// stay advertised.
var driveScopeStrategies = []ScopeStrategy{ScopeStrategyDriveReadonly, ScopeStrategyDriveFile}

// googleOAuthStrategy implements OAuthStrategy for google_drive.
type googleOAuthStrategy struct {
	// scope is the default requested Drive scope; defaults to drive.readonly.
	scope string
}

func newGoogleOAuthStrategy() *googleOAuthStrategy {
	return &googleOAuthStrategy{scope: googleDriveReadonly}
}

func (s *googleOAuthStrategy) DefaultScopes() []string { return []string{s.scope} }

// ScopesForStrategy maps an org-selected Drive strategy to its scope set. The
// empty strategy resolves to the provider default (drive.readonly), not
// drive.file, so a client with no strategy can still list the drive.
func (s *googleOAuthStrategy) ScopesForStrategy(strategy string) ([]string, bool) {
	switch strategy {
	case string(ScopeStrategyDriveReadonly), "":
		return []string{googleDriveReadonly}, true
	case string(ScopeStrategyDriveFile):
		return []string{googleDriveFile}, true
	default:
		return nil, false
	}
}

func (s *googleOAuthStrategy) AuthorizeURL(client OAuthClient, req OAuthAuthorizeRequest) string {
	authURL := client.AuthURL
	if authURL == "" {
		authURL = googleAuthURL
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
	// Force a refresh token: offline access + explicit consent (Google only
	// returns a refresh_token when prompt=consent is present).
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	for k, v := range req.Extra {
		q.Set(k, v)
	}
	return authURL + "?" + q.Encode()
}

func (s *googleOAuthStrategy) Exchange(ctx context.Context, httpClient *http.Client, client OAuthClient, code, codeVerifier, redirectURI string) (Credentials, error) {
	tokenURL := client.TokenURL
	if tokenURL == "" {
		tokenURL = googleTokenURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", client.ClientID)
	form.Set("client_secret", client.ClientSecret)
	form.Set("redirect_uri", redirectURI)
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("sources: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sources: token exchange request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var payload struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int    `json:"expires_in"`
		Scope            string `json:"scope"`
		TokenType        string `json:"token_type"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("sources: decode token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || payload.Error != "" {
		msg := payload.Error
		if payload.ErrorDescription != "" {
			msg = payload.Error + ": " + payload.ErrorDescription
		}
		if msg == "" {
			msg = fmt.Sprintf("token endpoint returned %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("sources: token exchange failed: %s", msg)
	}
	if strings.TrimSpace(payload.RefreshToken) == "" {
		return nil, fmt.Errorf("sources: token exchange returned no refresh token (ensure access_type=offline and prompt=consent)")
	}

	// The stored credential is provider-defined; for Google it carries the
	// refresh token (the client id/secret live in org/deployment config).
	creds := Credentials{
		"refresh_token": payload.RefreshToken,
	}
	if payload.Scope != "" {
		creds["scope"] = payload.Scope
	}
	if payload.AccessToken != "" {
		creds["access_token"] = payload.AccessToken
	}
	if payload.ExpiresIn > 0 {
		creds["expires_at"] = time.Now().UTC().Add(time.Duration(payload.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	return creds, nil
}
