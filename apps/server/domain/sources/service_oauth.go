package sources

import (
	"context"
	"net/url"
	"strings"
	"time"
	"unicode"

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

// oauthGrantedScopesKey is the reserved config key holding the scope set the
// token response actually granted, in the provider's own words (space-delimited
// in the response, stored as a list). Non-secret: it is display metadata on the
// source read, and for a provider that declares no selectable scope strategies
// (Trale) it is the only grant detail there is to show. Deliberately NOT a
// credential key, so it is never extracted as credential material from
// user-submitted config; edits preserve it.
const oauthGrantedScopesKey = "oauth_granted_scopes"

// StartOAuthRequest carries the inputs for starting an OAuth connect.
type StartOAuthRequest struct {
	UserID *uuid.UUID
	// Config is the intended (non-secret) source config, e.g. {"name": "...",
	// "folder_id": "..."}. It is persisted with the pending state and applied
	// when the callback creates the source.
	Config map[string]any
	Scopes []string
	// ReconnectSourceID re-runs consent for an existing source instead of
	// connecting a new one: the callback replaces that source's stored grant in
	// place, preserving its name, config and history (#2107). Config is ignored
	// when it is set — a reconnect repairs the credential, it does not re-enter
	// the source's settings.
	ReconnectSourceID *uuid.UUID
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
	// Flow keys are reserved: only this function may set them. Without this a
	// crafted connect body carrying a reconnect key would aim the callback at an
	// existing source in the project, and either key would leak into the new
	// source's stored config (#2107 review).
	req.Config = sanitizeOAuthFlowConfig(req.Config)
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

	// A reconnect carries the target source through the pending state so the
	// callback can replace its grant rather than create a source. Validated here,
	// while the caller's project scope is still known: the id arrives from the
	// browser, so a cross-project or mismatched id must not become a flow.
	if req.ReconnectSourceID != nil && *req.ReconnectSourceID != uuid.Nil {
		src, serr := s.repo.GetByID(ctx, *req.ReconnectSourceID, projectID)
		if serr != nil {
			return nil, serr
		}
		if src.Provider != providerType {
			return nil, apperror.NewValidation("source " + src.ID.String() + " is a " + src.Provider + " source, not " + providerType)
		}
		cfg := map[string]any{oauthReconnectKey: src.ID.String()}
		// The callback still needs the discovered dynamic client, if any.
		if v, ok := req.Config[oauthDynamicKey]; ok {
			cfg[oauthDynamicKey] = v
		}
		req.Config = cfg
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

// sanitizeOAuthFlowConfig returns a copy of a caller-supplied source config with
// the reserved flow keys removed. A request may describe a source; it may never
// join the flow's own bookkeeping.
func sanitizeOAuthFlowConfig(cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if k == oauthReconnectKey || k == oauthDynamicKey {
			continue
		}
		out[k] = v
	}
	return out
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

// OAuthCompletion is the callback outcome: the source the grant now belongs to,
// and whether the flow repaired an existing source's grant instead of creating
// one. The landing message differs ("connected" vs "renewed"), so the fact has
// to survive the call instead of being re-derived by the handler (#2186).
type OAuthCompletion struct {
	Integration *Integration
	Reconnected bool
}

// LandingKey is the one-shot landing outcome parameter for this completion. It
// lives here so the mapping is covered by the domain tests: the callback handler
// only has to pass it through, and a repair can never silently land as a new
// connection.
func (c *OAuthCompletion) LandingKey() string {
	if c != nil && c.Reconnected {
		return "reconnected"
	}
	return "connected"
}

// CompleteOAuth consumes a pending state, exchanges the authorization code for
// credentials, and then either creates the source or — for a reconnect — repairs
// the existing one's grant in place. A state that is missing, expired, or
// already consumed resolves to not-found, so a callback can never be replayed.
func (s *Service) CompleteOAuth(ctx context.Context, providerType, state, code string) (*OAuthCompletion, error) {
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
	// Reserved flow keys are flow state, never source config.
	delete(cfg, oauthDynamicKey)
	delete(cfg, oauthReconnectKey)
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
	// Persist the granted scope set verbatim. Unlike the strategy — which only
	// exists for providers with selectable scope strategies — this is what the
	// provider actually issued, and for a self-registering provider it is the
	// only grant detail the read can report.
	if scopes := grantedScopes(creds); len(scopes) > 0 {
		cfg[oauthGrantedScopesKey] = scopes
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

	// A reconnect replaces the existing source's grant instead of creating a
	// second source: only the credential blob and the connect-time metadata move,
	// so the name, sync scope, cursor and history the user already has survive
	// (#2107).
	if raw, ok := pending.Config[oauthReconnectKey].(string); ok && strings.TrimSpace(raw) != "" {
		id, perr := uuid.Parse(raw)
		if perr != nil {
			return nil, fail(apperror.NewValidation("oauth state carries an invalid reconnect source id"))
		}
		src, serr := s.repo.GetByID(ctx, id, pending.ProjectID)
		if serr != nil {
			return nil, fail(serr)
		}
		if src.Provider != pending.Provider {
			return nil, fail(apperror.NewValidation("oauth state provider does not match the source being reconnected"))
		}
		meta := map[string]any{}
		for _, k := range []string{oauthGrantedStrategyKey, oauthGrantedScopesKey, oauthAccountEmailKey} {
			if v, ok := cfg[k]; ok {
				meta[k] = v
			}
		}
		updated, rerr := s.replaceCredentials(ctx, src, creds, meta)
		if rerr != nil {
			return nil, fail(rerr)
		}
		return &OAuthCompletion{Integration: updated, Reconnected: true}, nil
	}

	if _, typed := p.(TypedObjectProvider); typed {
		if verr := p.ValidateConfig(cfg); verr != nil {
			return nil, fail(verr)
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
	return &OAuthCompletion{Integration: integration}, nil
}

// replaceCredentials installs a freshly granted credential set on an existing
// source. The stored config is the source's own — the reconnect flow never
// re-enters settings — and only the connect-time metadata the token exchange
// yields (the granted scope strategy, the account email) is merged into it.
func (s *Service) replaceCredentials(ctx context.Context, src *Source, creds Credentials, meta map[string]any) (*Integration, error) {
	blob, err := s.encryptCredentials(creds)
	if err != nil {
		return nil, err
	}
	cfg := cloneConfig(src.Config)
	for k, v := range meta {
		cfg[k] = v
	}
	// The new grant may belong to a different account, or the provider may report
	// a scope set we cannot map. A value this exchange could not supply has to
	// clear the old one, or the card keeps describing the account and scope that
	// just went away (#2186).
	for _, k := range []string{oauthGrantedStrategyKey, oauthGrantedScopesKey, oauthAccountEmailKey} {
		if _, ok := meta[k]; !ok {
			delete(cfg, k)
		}
	}
	if src.CredentialID != nil {
		// The sync reads the saved credential, not this row
		// (credentialsForSource), so the new grant has to land there or the
		// reconnect stores a grant nothing reads and the source re-parks on its
		// next sync — the user's one repair action appearing not to work (#2418).
		// One credential is one account's grant, so this repairs every source that
		// reads it; parked siblings are returned to active in the same write,
		// because the scheduler never admits a parked source and they could
		// otherwise never notice.
		grantMeta := make(map[string]any, 3)
		for _, k := range []string{oauthGrantedStrategyKey, oauthGrantedScopesKey, oauthAccountEmailKey} {
			if v, ok := cfg[k]; ok {
				grantMeta[k] = v
			}
		}
		reactivated, err := s.repo.ReplaceSavedCredentialGrant(ctx, *src.CredentialID, src.ProjectID, blob, grantMeta)
		if err != nil {
			return nil, err
		}
		// Each source the statement returned to active has been repaired, so its
		// reconnect item is resolved. A disabled sibling it left alone keeps its
		// status and therefore keeps its own item (#2414).
		for _, id := range reactivated {
			s.resolveSourceNotification(ctx, src.ProjectID, id)
		}
	}
	if err := s.repo.ReplaceOAuthGrant(ctx, src.ID, src.ProjectID, blob, cfg); err != nil {
		return nil, err
	}
	s.resolveSourceNotification(ctx, src.ProjectID, src.ID)
	updated, err := s.repo.GetByID(ctx, src.ID, src.ProjectID)
	if err != nil {
		return nil, err
	}
	return s.toIntegration(updated), nil
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

// grantedScopes resolves the scope set a token response actually granted from
// its raw scope string (creds["scope"]), preserving the provider's order and
// dropping duplicates. Unlike grantedStrategy it applies to every provider,
// strategy-aware or not: it is what the provider issued, not what we mapped.
func grantedScopes(creds Credentials) []string {
	raw, _ := creds["scope"].(string)
	// RFC 6749 §3.3 uses space-delimited scope sets, but OAuth 2.0 servers also
	// answer with the comma-delimited form GitHub uses ("repo,read:org"), so both
	// separators are accepted rather than storing one compound entry.
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	seen := make(map[string]bool, len(fields))
	for _, s := range fields {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
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
