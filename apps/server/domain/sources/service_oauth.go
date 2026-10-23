package sources

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// oauthPendingTTL bounds how long an authorize request may wait for the callback.
const oauthPendingTTL = 10 * time.Minute

// oauthGrantedStrategyKey is the reserved config key under which the connect
// flow records the scope strategy the token response actually granted. It is
// non-secret and stays visible in the source's config on read. It is deliberately
// NOT a credential key (absent from credentialKeyNames, so it is never extracted
// as credential material from user-submitted config); edits preserve it.
const oauthGrantedStrategyKey = "oauth_granted_strategy"

// oauthAccountEmailKey is the reserved config key holding the authenticated
// provider account's email, captured best-effort at connect. It is non-secret
// metadata (used to show which account a source syncs), not a credential.
const oauthAccountEmailKey = "oauth_account_email"

// StartOAuthRequest carries the inputs for starting an OAuth connect.
type StartOAuthRequest struct {
	UserID *uuid.UUID
	// Config is the intended (non-secret) source config, e.g. {"name": "...",
	// "folder_id": "..."}. It is persisted with the pending state and applied
	// when the callback creates the source.
	Config map[string]any
	Scopes []string
}

// StartOAuthResponse is the start result: the provider authorization URL.
type StartOAuthResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
}

// StartOAuth builds a provider authorization URL (state + PKCE S256) and
// persists the pending state that the callback will consume.
func (s *Service) StartOAuth(ctx context.Context, projectID uuid.UUID, providerType string, req StartOAuthRequest) (*StartOAuthResponse, error) {
	p, ok := s.providers.Get(providerType)
	if !ok {
		return nil, apperror.NewValidation("unsupported providerType: " + providerType)
	}
	strategy, ok := p.OAuth()
	if !ok {
		return nil, apperror.NewValidation("provider does not support OAuth connect: " + providerType)
	}
	redirectURI, err := s.oauthRedirectURI(providerType)
	if err != nil {
		return nil, err
	}
	state, err := NewOAuthState()
	if err != nil {
		return nil, apperror.NewInternal("generate oauth state", err)
	}
	verifier, err := NewCodeVerifier()
	if err != nil {
		return nil, apperror.NewInternal("generate pkce verifier", err)
	}

	// Resolve the client. Dynamic (self-registering) providers discover + DCR a
	// client instead of reading a configured one; static providers (Google/GitHub)
	// keep the resolveOAuthClient path byte-for-byte.
	scopes := req.Scopes
	var extra map[string]string
	var client OAuthClient
	if dyn, ok := p.(DynamicOAuthProvider); ok {
		dc, derr := dyn.DiscoverOAuth(ctx, s.oauthHTTP, redirectURI)
		if derr != nil {
			return nil, apperror.NewValidation("OAuth client discovery failed: " + derr.Error())
		}
		client = OAuthClient{
			ClientID: dc.ClientID,
			AuthURL:  dc.AuthURL,
			TokenURL: dc.TokenURL,
			Scopes:   dc.Scopes,
			Resource: dc.Resource,
		}
		scopes = dc.Scopes
		extra = map[string]string{"resource": dc.Resource}
		// Persist the discovered identity/endpoints so the callback can exchange
		// the code without re-registering (registration is single-use scoped to
		// the redirect URI, and re-registering on the callback would race the
		// just-issued authorization code).
		req.Config = withDynamicOAuthConfig(req.Config, dc)
	} else {
		client, err = s.resolveOAuthClient(ctx, projectID, providerType)
		if err != nil {
			return nil, err
		}
	}

	if err := s.repo.CreateOAuthPendingState(ctx, &OAuthPendingState{
		State:        state,
		ProjectID:    projectID,
		Provider:     providerType,
		UserID:       req.UserID,
		CodeVerifier: verifier,
		RedirectURI:  redirectURI,
		Config:       req.Config,
		Scopes:       scopes,
		ExpiresAt:    s.now().Add(oauthPendingTTL),
	}); err != nil {
		return nil, err
	}

	authorizeURL := strategy.AuthorizeURL(client, OAuthAuthorizeRequest{
		State:         state,
		CodeChallenge: CodeChallengeS256(verifier),
		RedirectURI:   redirectURI,
		Scopes:        scopes,
		Extra:         extra,
	})
	return &StartOAuthResponse{AuthorizeURL: authorizeURL}, nil
}

// withDynamicOAuthConfig returns a copy of cfg carrying the discovered dynamic
// client under the reserved key. A nil cfg becomes a fresh map.
func withDynamicOAuthConfig(cfg map[string]any, dc DynamicOAuthClient) map[string]any {
	out := make(map[string]any, len(cfg)+1)
	for k, v := range cfg {
		out[k] = v
	}
	out[oauthDynamicKey] = dynamicOAuthToConfig(dc)
	return out
}

// OAuthConnectError is a callback failure raised after the pending state was
// consumed. It carries the project the flow started from so the browser-facing
// landing can return the caller to that scope even though no source exists.
// Failures before the state is read (missing, expired, or already consumed)
// carry no project: the landing then falls back to the caller's active scope.
type OAuthConnectError struct {
	ProjectID uuid.UUID
	Err       error
}

func (e *OAuthConnectError) Error() string { return e.Err.Error() }
func (e *OAuthConnectError) Unwrap() error { return e.Err }

// CompleteOAuth consumes a pending state, exchanges the authorization code for
// credentials, and creates the source. A state that is missing, expired, or
// already consumed resolves to not-found, so a callback can never be replayed.
func (s *Service) CompleteOAuth(ctx context.Context, providerType, state, code string) (*Integration, error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return nil, apperror.NewValidation("code and state are required")
	}
	pending, err := s.repo.ConsumeOAuthPendingState(ctx, state)
	if err != nil {
		return nil, err
	}
	// Every failure from here on knows the flow's project; wrap it so the
	// handler can scope the browser landing (see OAuthConnectError).
	fail := func(err error) error {
		return &OAuthConnectError{ProjectID: pending.ProjectID, Err: err}
	}
	if pending.Provider != providerType {
		return nil, fail(apperror.NewValidation("provider mismatch for oauth state"))
	}
	p, ok := s.providers.Get(pending.Provider)
	if !ok {
		return nil, fail(apperror.NewValidation("unsupported providerType: " + pending.Provider))
	}
	strategy, ok := p.OAuth()
	if !ok {
		return nil, fail(apperror.NewValidation("provider does not support OAuth connect: " + pending.Provider))
	}

	// Resolve the client. Dynamic (self-registering) providers recover the
	// client they registered at start time from the reserved pending-state key;
	// static providers (Google/GitHub) resolve as before.
	var client OAuthClient
	dynamic := false
	if _, ok := p.(DynamicOAuthProvider); ok {
		v, present := pending.Config[oauthDynamicKey]
		if !present {
			return nil, fail(apperror.NewValidation("oauth state is missing its dynamic client; reconnect the source"))
		}
		dc, ok := dynamicOAuthFromConfig(v)
		if !ok {
			return nil, fail(apperror.NewValidation("oauth state has an invalid dynamic client"))
		}
		client = OAuthClient{
			ClientID: dc.ClientID,
			AuthURL:  dc.AuthURL,
			TokenURL: dc.TokenURL,
			Scopes:   dc.Scopes,
			Resource: dc.Resource,
		}
		dynamic = true
	} else {
		client, err = s.resolveOAuthClient(ctx, pending.ProjectID, pending.Provider)
		if err != nil {
			return nil, fail(err)
		}
	}

	creds, err := strategy.Exchange(ctx, s.oauthHTTP, client, code, pending.CodeVerifier, pending.RedirectURI)
	if err != nil {
		return nil, fail(apperror.NewValidation("oauth token exchange failed: " + err.Error()))
	}

	// Merge the client material into the stored credential so the connector can
	// refresh. Dynamic clients register as public clients (no secret), so only
	// the client id is merged; the strategy's Exchange already stores
	// client_id/token_endpoint/resource. Org-level clients (layered in a later
	// phase) will replace the static merge.
	creds["client_id"] = client.ClientID
	if !dynamic {
		creds["client_secret"] = client.ClientSecret
	}

	cfg := make(map[string]any, len(pending.Config)+len(creds))
	for k, v := range pending.Config {
		cfg[k] = v
	}
	// The reserved dynamic-client key is flow state, never source config.
	delete(cfg, oauthDynamicKey)
	name := "Data source"
	if v, ok := cfg["name"].(string); ok && strings.TrimSpace(v) != "" {
		name = v
	}
	delete(cfg, "name")
	for _, k := range p.CredentialKeys() {
		if v, ok := creds[k]; ok {
			cfg[k] = v
		}
	}
	// Persist the scope strategy the token response actually granted, resolved
	// from its raw scope string. It exists only in the token response at exchange
	// time; the configured strategy can drift after the token was issued, and the
	// granted strategy cannot be recovered from the stored credential later. Capture
	// it now so the read can report it independently of the client config.
	if granted := grantedStrategy(p, creds); granted != "" {
		cfg[oauthGrantedStrategyKey] = granted
	}
	// Capture which Google account authorized the source while the exchange's
	// short-lived access token is still available. Best-effort: a failed lookup
	// must never fail the connect, and the value is display metadata only.
	if s.accountEmail != nil {
		if token, _ := creds["access_token"].(string); strings.TrimSpace(token) != "" {
			emailCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if email, eerr := s.accountEmail(emailCtx, token); eerr == nil && strings.TrimSpace(email) != "" {
				cfg[oauthAccountEmailKey] = strings.TrimSpace(email)
			}
			cancel()
		}
	}

	integration, err := s.Create(ctx, pending.ProjectID, &CreateIntegrationRequest{
		Name:                  name,
		ProviderType:          pending.Provider,
		Config:                cfg,
		AllowIncompleteConfig: true,
	})
	if err != nil {
		// The token exchange succeeded but the source could not be stored: still
		// a post-consume failure, so the landing keeps the flow's project.
		return nil, fail(err)
	}
	return integration, nil
}

// grantedStrategy resolves the scope strategy a token response actually granted
// from its raw scope string (creds["scope"]). It splits the space-delimited
// scopes and maps them through the provider's ScopeStrategies.StrategyForScopes;
// "" means no recognisable granted strategy (no scope, a non-strategy provider,
// or explicit/custom scopes), in which case nothing should be persisted.
func grantedStrategy(p SourceProvider, creds Credentials) string {
	raw, _ := creds["scope"].(string)
	scopes := strings.Fields(raw)
	if len(scopes) == 0 {
		return ""
	}
	ss, ok := p.(ScopeStrategies)
	if !ok {
		return ""
	}
	return ss.StrategyForScopes(scopes)
}

// oauthRedirectURI builds the provider callback URL from the configured base.
func (s *Service) oauthRedirectURI(providerType string) (string, error) {
	base := strings.TrimRight(s.oauthCfg.RedirectBaseURL, "/")
	if base == "" {
		return "", apperror.NewValidation("OAuth redirect base URL is not configured (SOURCES_OAUTH_REDIRECT_BASE_URL)")
	}
	return base + "/api/data-source-integrations/oauth/" + providerType + "/callback", nil
}

// OAuthLandingURL builds the browser landing URL after a callback (success or
// error), carrying the given query string. It uses the configured landing URL
// (SOURCES_OAUTH_LANDING_URL) when set, else falls back to
// <RedirectBaseURL>/oauth/landing/data-sources, else a relative
// /oauth/landing/data-sources path.
func (s *Service) OAuthLandingURL(query string) string {
	return s.oauthLandingBase() + query
}

// oauthLandingBase returns the browser landing base URL (no query) after a
// callback: the configured landing URL, else
// <RedirectBaseURL>/oauth/landing/data-sources, else the relative
// /oauth/landing/data-sources path. The landing path is deliberately
// scope-agnostic: the gateway resolves the caller's org/project from the
// scope_project hint this server forwards (see oauthLandingQuery), so a single
// deployment-wide value works for every user and project.
func (s *Service) oauthLandingBase() string {
	if l := strings.TrimSpace(s.oauthCfg.LandingURL); l != "" {
		// Drop the query/fragment first, then the trailing slash: the outcome
		// query is appended directly, and ".../landing/?connected=..." matches
		// no route.
		return strings.TrimRight(stripLandingQuery(l), "/")
	}
	base := strings.TrimRight(s.oauthCfg.RedirectBaseURL, "/")
	if base == "" {
		return "/oauth/landing/data-sources"
	}
	return base + "/oauth/landing/data-sources"
}

// stripLandingQuery drops the configured landing URL's own query and fragment:
// OAuthLandingURL appends the callback query verbatim, so a value that already
// carried one would yield "...?a=1?connected=..." (or hide the outcome inside a
// fragment). The landing's query is always ours.
func stripLandingQuery(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i]
	}
	return raw
}

// oauthJavascriptOrigin returns the scheme://host[:port] origin of the browser
// landing URL (for the Google Picker app origin allowlist). Empty when the
// landing base is a relative path or unparseable.
func (s *Service) oauthJavascriptOrigin() string {
	u, err := url.Parse(s.oauthLandingBase())
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// redirectURIOrEmpty builds the provider callback redirect URI, or "" when the
// redirect base URL is not configured.
func (s *Service) redirectURIOrEmpty(providerType string) string {
	uri, err := s.oauthRedirectURI(providerType)
	if err != nil {
		return ""
	}
	return uri
}
