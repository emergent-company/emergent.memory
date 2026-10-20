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
	client, err := s.resolveOAuthClient(ctx, projectID, providerType)
	if err != nil {
		return nil, err
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

	if err := s.repo.CreateOAuthPendingState(ctx, &OAuthPendingState{
		State:        state,
		ProjectID:    projectID,
		Provider:     providerType,
		UserID:       req.UserID,
		CodeVerifier: verifier,
		RedirectURI:  redirectURI,
		Config:       req.Config,
		Scopes:       req.Scopes,
		ExpiresAt:    s.now().Add(oauthPendingTTL),
	}); err != nil {
		return nil, err
	}

	authorizeURL := strategy.AuthorizeURL(client, OAuthAuthorizeRequest{
		State:         state,
		CodeChallenge: CodeChallengeS256(verifier),
		RedirectURI:   redirectURI,
		Scopes:        req.Scopes,
	})
	return &StartOAuthResponse{AuthorizeURL: authorizeURL}, nil
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
	client, err := s.resolveOAuthClient(ctx, pending.ProjectID, pending.Provider)
	if err != nil {
		return nil, fail(err)
	}
	creds, err := strategy.Exchange(ctx, s.oauthHTTP, client, code, pending.CodeVerifier, pending.RedirectURI)
	if err != nil {
		return nil, fail(apperror.NewValidation("oauth token exchange failed: " + err.Error()))
	}

	// Merge the deployment client material into the stored credential so the
	// connector can refresh. Org-level clients (layered in a later phase) will
	// replace this merge.
	creds["client_id"] = client.ClientID
	creds["client_secret"] = client.ClientSecret

	cfg := make(map[string]any, len(pending.Config)+len(creds))
	for k, v := range pending.Config {
		cfg[k] = v
	}
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

	integration, err := s.Create(ctx, pending.ProjectID, &CreateIntegrationRequest{
		Name:         name,
		ProviderType: pending.Provider,
		Config:       cfg,
	})
	if err != nil {
		// The token exchange succeeded but the source could not be stored: still
		// a post-consume failure, so the landing keeps the flow's project.
		return nil, fail(err)
	}
	return integration, nil
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
