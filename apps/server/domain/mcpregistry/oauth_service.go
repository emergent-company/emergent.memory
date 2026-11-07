package mcpregistry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/mcpauth"
)

// oauthProviderMCP is the provider value used for MCP authorize flows in the
// shared kb.oauth_pending_states table.
const oauthProviderMCP = "mcp"

// oauthPendingTTL bounds how long an authorize flow may stay pending.
const oauthPendingTTL = 15 * time.Minute

// Sentinel errors of the MCP OAuth connect flow, mapped to HTTP statuses by the
// handler.
var (
	// ErrOAuthNotConfigured marks a deployment without a public redirect origin
	// (MCP_REGISTRY_OAUTH_REDIRECT_BASE_URL). Handler maps it to 400 with an
	// actionable message.
	ErrOAuthNotConfigured = errors.New("MCP OAuth connect is not configured for this deployment (MCP_REGISTRY_OAUTH_REDIRECT_BASE_URL is unset)")
	// ErrOAuthNotSupported marks a server that cannot use the browser flow
	// (a non-remote transport). Handler maps it to 400.
	ErrOAuthNotSupported = errors.New("this MCP server does not support the OAuth connect flow (only remote http/sse servers do)")
	// ErrOAuthNotRequired marks a server whose connection attempt was not
	// rejected with an OAuth challenge. Handler maps it to 400.
	ErrOAuthNotRequired = errors.New("this MCP server did not report an authorization requirement")
	// ErrOAuthClientIDRequired marks a client-configuration request that has no
	// row to attach a requested scope set to (or, with an existing row, nothing
	// to clear). Handler maps it to 400.
	ErrOAuthClientIDRequired = errors.New("clientId is required until an OAuth client or scope set is configured")
	// ErrOAuthClientSecretRequiresClientID marks a scope-only configuration
	// request that also carried a client secret: a secret has no owner without a
	// client id, and silently dropping it would misrepresent what was saved.
	ErrOAuthClientSecretRequiresClientID = errors.New("a client secret requires a client id")
	// ErrOAuthSharedConnection marks a disconnect of a project-wide (shared)
	// credential by a signed-in user. Only an administrative disconnect
	// (empty user id) may remove it.
	ErrOAuthSharedConnection = errors.New("this OAuth connection is shared; an administrator must disconnect it")
	// ErrOAuthClientRequired marks an authorization server that will not issue
	// us a client: dynamic client registration is unavailable or was rejected.
	// It is the sentinel behind both a registration-endpoint-free metadata
	// document and a non-2xx dynamic client registration response. Handler maps
	// it to 400 with an actionable message: provision a pre-registered client.
	ErrOAuthClientRequired = errors.New("this authorization server will not issue a client — dynamic client registration is unavailable or was rejected")
	// ErrOAuthMetadataUnavailable marks a provider whose authorization server
	// publishes no discoverable OAuth metadata at all (no RFC 8414 document and
	// no OpenID Connect document). Handler maps it to 422 with an actionable
	// message: configure a token header instead, because the browser connect
	// flow cannot work without discoverable endpoints.
	ErrOAuthMetadataUnavailable = errors.New("this MCP server's provider publishes no OAuth authorization metadata")
	// ErrOAuthDiscoveryFailed marks a probe or metadata fetch that could not be
	// completed (unreachable server, unusable metadata). Handler maps it to 503
	// (Service Unavailable): the failure is upstream, not bad input. A permanent
	// absence of metadata is the separate ErrOAuthMetadataUnavailable → 422 case.
	ErrOAuthDiscoveryFailed = errors.New("could not determine the MCP server's authorization requirement")
	// ErrOAuthStateInvalid marks a callback whose state is unknown, expired, or
	// already consumed. Handler redirects with an error indication.
	ErrOAuthStateInvalid = errors.New("oauth state is invalid, expired, or already used")
	// ErrOAuthNeedsReconnect marks a stored credential that can no longer be
	// refreshed. Callers surface it as "authorize this server again".
	ErrOAuthNeedsReconnect = errors.New("MCP server authorization expired — reconnect the server")
)

// OAuthConfig is the deployment-level configuration of the MCP OAuth connect
// flow: the public origin of this API (which the authorization server redirects
// back to) and the UI URL the browser lands on afterwards.
type OAuthConfig struct {
	// RedirectBaseURL is the externally reachable origin of the API, e.g.
	// https://api.example.com. The registered redirect URI is
	// <RedirectBaseURL>/api/mcp-servers/oauth/callback. Empty disables the flow.
	RedirectBaseURL string
	// LandingURL is the browser-facing URL the callback redirects to. It should
	// be the gateway's scope-agnostic landing route, e.g.
	// https://admin.example.com/oauth/landing/mcp-servers: this API server has no
	// org/project scope, so a scoped UI path cannot be configured here. Empty
	// falls back to <RedirectBaseURL>/oauth/landing/mcp-servers (single-origin
	// setups).
	LandingURL string
	// ClientName is the client_name presented at dynamic client registration.
	ClientName string
}

// redirectURI is the absolute callback URL registered with the authorization
// server. It must stay stable per deployment: dynamic client registration binds
// the redirect URI at registration time.
func (c OAuthConfig) redirectURI() string {
	base := strings.TrimRight(strings.TrimSpace(c.RedirectBaseURL), "/")
	if base == "" {
		return ""
	}
	return base + "/api/mcp-servers/oauth/callback"
}

// scopeProjectParam names the untrusted project hint carried on the
// post-callback landing URL. The web UI (a separate Go module) validates it
// against the caller's access tree and ignores it when it does not resolve — it
// is never an authorization input. Keep the name in sync with
// apps/web-ui/gateway/oauth_landing.go.
const scopeProjectParam = "scope_project"

// landingURL builds the post-callback UI URL with a one-shot outcome parameter.
// projectID is the flow's project when it is known (the state was consumed); it
// is forwarded as an untrusted hint so the UI can land the caller back in that
// scope. An empty projectID omits the hint.
func (c OAuthConfig) landingURL(outcome, projectID string) string {
	raw := strings.TrimSpace(c.LandingURL)
	if raw == "" {
		base := strings.TrimRight(strings.TrimSpace(c.RedirectBaseURL), "/")
		if base == "" {
			return ""
		}
		raw = base + "/oauth/landing/mcp-servers"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("mcp_oauth", outcome)
	if pid := strings.TrimSpace(projectID); pid != "" {
		q.Set(scopeProjectParam, pid)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// --- Discovery ---

// DiscoverAuthRequirement probes a remote MCP server without credentials and
// reports its authorization requirement. It returns ErrOAuthNotRequired when the
// server does not answer with an OAuth challenge, and wraps metadata failures.
//
// The probe is a single `initialize` POST: a protected MCP server answers it
// with 401 (or 403) and a `WWW-Authenticate: Bearer` challenge naming its
// protected-resource metadata.
func (s *Service) DiscoverAuthRequirement(ctx context.Context, server *MCPServer) (*authRequirement, error) {
	return discoverServerAuthRequirement(ctx, server)
}

// discoverServerAuthRequirement is the package-level probe used by both the
// service and the proxy's inspect classification.
func discoverServerAuthRequirement(ctx context.Context, server *MCPServer) (*authRequirement, error) {
	if server == nil {
		return nil, fmt.Errorf("server is required")
	}
	if server.Type != ServerTypeHTTP && server.Type != ServerTypeSSE {
		return nil, ErrOAuthNotSupported
	}
	if server.URL == nil || strings.TrimSpace(*server.URL) == "" {
		return nil, fmt.Errorf("server has no URL")
	}
	resourceURL := strings.TrimSpace(*server.URL)

	if err := oauthValidateURL(ctx, resourceURL); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOutboundURLNotAllowed, err)
	}
	ch, err := probeAuthChallenge(ctx, resourceURL)
	if err != nil {
		if errors.Is(err, ErrOAuthNotRequired) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", ErrOAuthDiscoveryFailed, err)
	}
	req, err := discoverAuthRequirement(ctx, resourceURL, ch)
	if err != nil {
		if errors.Is(err, ErrOAuthMetadataUnavailable) {
			// A permanent absence of metadata is actionable (the provider simply
			// does not publish it), not an upstream outage.
			return nil, fmt.Errorf("%w: %v", ErrOAuthMetadataUnavailable, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrOAuthDiscoveryFailed, err)
	}
	return req, nil
}

// probeAuthChallenge performs an unauthenticated MCP `initialize` against a
// remote server and returns the Bearer challenge from its 401/403 response.
// ErrOAuthNotRequired is returned when no challenge is advertised.
func probeAuthChallenge(ctx context.Context, resourceURL string) (authChallenge, error) {
	ch, found, err := mcpauth.ProbeResource(ctx, mcpauth.ProbeConfig{
		Resource:  resourceURL,
		ClientFor: oauthProbeHTTPClient,
		Validate:  oauthValidateURL,
	})
	if err != nil {
		return authChallenge{}, fmt.Errorf("probing MCP server: %w", err)
	}
	if !found {
		return authChallenge{}, ErrOAuthNotRequired
	}
	return ch, nil
}

// --- Connect flow ---

// StartMCPOAuth begins an authorization flow for a server: discovery, dynamic
// client registration (once), PKCE + state, and the authorization URL.
func (s *Service) StartMCPOAuth(ctx context.Context, projectID, serverID, userID string) (*StartMCPOAuthResponseDTO, error) {
	redirectURI := s.oauthCfg.redirectURI()
	if redirectURI == "" {
		return nil, ErrOAuthNotConfigured
	}

	server, err := s.repo.FindServerByID(ctx, serverID, &projectID)
	if err != nil {
		return nil, fmt.Errorf("loading MCP server: %w", err)
	}
	if server == nil {
		return nil, ErrServerNotFound
	}

	req, err := s.DiscoverAuthRequirement(ctx, server)
	if err != nil {
		return nil, err
	}

	row, err := s.repo.FindOAuthForUser(ctx, server.ID, userID)
	if err != nil {
		return nil, fmt.Errorf("loading MCP server authorization: %w", err)
	}
	// An operator-configured scope set wins over discovery. The provider's
	// advertised scopes_supported is a capability list, not a request set
	// (RFC 9728 §7.2), so it is never requested implicitly; a challenge scope
	// stays authoritative and dynamic when no operator set is configured.
	if row != nil {
		if configured := splitScope(row.RequestedScope); len(configured) > 0 {
			req.Scopes = configured
		}
	}
	if row == nil {
		row = &MCPServerOAuth{
			ServerID:             server.ID,
			UserID:               userIDOrNil(userID),
			ProjectID:            projectID,
			AuthorizationServer:  req.AuthorizationServer,
			TokenEndpoint:        req.TokenEndpoint,
			RegistrationEndpoint: req.RegistrationEndpoint,
			Resource:             req.Resource,
		}
	}

	// Register a client once. A stored client id (from a previous registration or
	// an operator's configuration) is reused as-is: the redirect URI is bound at
	// registration time, so re-registering on every connect would leak clients.
	if strings.TrimSpace(row.ClientID) == "" {
		if req.RegistrationSupported {
			identity, regErr := registerClient(ctx, req.RegistrationEndpoint, s.oauthClientName(), redirectURI, req.Scopes)
			if regErr != nil {
				return nil, fmt.Errorf("registering an OAuth client with the authorization server: %w", regErr)
			}
			row.ClientID = identity.ClientID
			secret, encErr := encryptSecretValue(s.encryptor, identity.ClientSecret)
			if encErr != nil {
				return nil, encErr
			}
			row.ClientSecret = secret
		} else {
			return nil, fmt.Errorf("%w: the authorization server %q requires a pre-registered OAuth client (dynamic client registration is not offered)", ErrOAuthClientRequired, req.AuthorizationServer)
		}
	}
	// Refresh the discovered endpoints on every start so a moved endpoint heals.
	row.AuthorizationServer = req.AuthorizationServer
	row.TokenEndpoint = req.TokenEndpoint
	row.RegistrationEndpoint = req.RegistrationEndpoint
	row.Resource = req.Resource
	if err := s.repo.UpsertOAuth(ctx, row); err != nil {
		return nil, err
	}

	state, err := newOAuthState()
	if err != nil {
		return nil, err
	}
	verifier, err := newCodeVerifier()
	if err != nil {
		return nil, err
	}

	pending := &mcpOAuthPendingState{
		State:        state,
		ProjectID:    projectID,
		Provider:     oauthProviderMCP,
		CodeVerifier: verifier,
		RedirectURI:  redirectURI,
		Config:       map[string]any{"serverId": server.ID},
		Scopes:       req.Scopes,
		ExpiresAt:    time.Now().UTC().Add(oauthPendingTTL),
	}
	if userID != "" {
		pending.UserID = &userID
	}
	if err := s.repo.CreateOAuthPendingState(ctx, pending); err != nil {
		return nil, err
	}

	authorizeURL, err := buildAuthorizeURL(req, &oauthClientIdentity{ClientID: row.ClientID}, redirectURI, state, codeChallengeS256(verifier))
	if err != nil {
		return nil, err
	}

	s.log.Info("started MCP OAuth connect",
		slog.String("project_id", projectID),
		slog.String("server_id", server.ID),
		slog.String("authorization_server", req.AuthorizationServer),
		slog.Any("requested_scopes", req.Scopes),
		slog.Bool("scopes_available_not_requested", len(req.Scopes) == 0 && len(req.ScopesAvailable) > 0),
	)

	return &StartMCPOAuthResponseDTO{AuthorizeURL: authorizeURL, State: state}, nil
}

// oauthClientName returns the client name used at dynamic client registration.
func (s *Service) oauthClientName() string {
	if name := strings.TrimSpace(s.oauthCfg.ClientName); name != "" {
		return name
	}
	return "Memory"
}

// CompleteMCPOAuth consumes a pending authorize flow, exchanges the code, and
// persists the token set. It returns the authorized server id and the project it
// belongs to, so the handler can land the browser back on the right project.
func (s *Service) CompleteMCPOAuth(ctx context.Context, state, code string) (serverID, projectID string, err error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return "", "", ErrOAuthStateInvalid
	}
	pending, err := s.repo.ConsumeOAuthPendingState(ctx, state)
	if err != nil {
		return "", "", err
	}
	if pending == nil || pending.Provider != oauthProviderMCP {
		return "", "", ErrOAuthStateInvalid
	}
	// The state was consumed: from here the flow's project is known, so every
	// failure below still lands the browser back in that scope.
	projectID = pending.ProjectID
	serverID, _ = pending.Config["serverId"].(string)
	if serverID == "" {
		return "", projectID, ErrOAuthStateInvalid
	}
	userID := ""
	if pending.UserID != nil {
		userID = *pending.UserID
	}

	server, err := s.repo.FindServerByID(ctx, serverID, &projectID)
	if err != nil {
		return serverID, projectID, fmt.Errorf("loading MCP server: %w", err)
	}
	if server == nil {
		return serverID, projectID, ErrServerNotFound
	}
	row, err := s.repo.FindOAuthForUser(ctx, serverID, userID)
	if err != nil {
		return serverID, projectID, fmt.Errorf("loading MCP server authorization: %w", err)
	}
	if row == nil {
		return serverID, projectID, ErrOAuthStateInvalid
	}

	clientSecret, err := decryptSecretValue(s.encryptor, row.ClientSecret)
	if err != nil {
		return serverID, projectID, err
	}
	identity := &oauthClientIdentity{ClientID: row.ClientID, ClientSecret: clientSecret}
	req := &authRequirement{
		Resource:      row.Resource,
		TokenEndpoint: row.TokenEndpoint,
	}

	tokens, err := exchangeCode(ctx, req, identity, code, pending.CodeVerifier, pending.RedirectURI)
	if err != nil {
		return serverID, projectID, fmt.Errorf("exchanging the authorization code: %w", err)
	}
	if err := s.storeTokens(ctx, row, tokens, pending.Scopes); err != nil {
		return serverID, projectID, err
	}

	s.log.Info("completed MCP OAuth connect",
		slog.String("project_id", projectID),
		slog.String("server_id", serverID),
	)

	// The credential is now usable: refresh the tool list so the newly
	// authorized server's tools appear. Best-effort — the connect itself
	// already succeeded. Discovery is run as the connecting user so it resolves
	// that user's freshly stored credential.
	s.invalidateToolPool(projectID)
	discoverCtx := auth.ContextWithUser(ctx, &auth.AuthUser{ID: userID})
	if _, err := s.DiscoverAndSyncTools(discoverCtx, serverID, projectID); err != nil {
		s.log.Warn("tool discovery after MCP OAuth connect failed",
			slog.String("server_id", serverID),
			slog.String("error", err.Error()),
		)
	}
	return serverID, projectID, nil
}

// storeTokens encrypts and persists a token set. A response without a refresh
// token keeps the previously stored one (some authorization servers only issue
// a refresh token on the first exchange). requested is the scope set the flow
// asked for: RFC 6749 §5.1 requires the token response to echo `scope` when it
// differs from the request, so an omitted value means the request was granted.
// No other source is consulted: the granted scope must describe the token being
// stored, not a configuration value that may have changed mid-flow.
func (s *Service) storeTokens(ctx context.Context, row *MCPServerOAuth, tokens *oauthTokenSet, requested []string) error {
	access, err := encryptSecretValue(s.encryptor, tokens.AccessToken)
	if err != nil {
		return err
	}
	refreshSecret := row.RefreshToken
	if tokens.RefreshToken != "" {
		refreshSecret, err = encryptSecretValue(s.encryptor, tokens.RefreshToken)
		if err != nil {
			return err
		}
	}
	var expiresAt *time.Time
	if !tokens.ExpiresAt.IsZero() {
		t := tokens.ExpiresAt.UTC()
		expiresAt = &t
	}
	scope := tokens.Scope
	if scope == "" {
		scope = strings.Join(requested, " ")
	}
	if err := s.repo.UpdateOAuthTokens(ctx, row.ServerID, oauthUserID(row), access, refreshSecret, scope, expiresAt); err != nil {
		return err
	}
	row.AccessToken = access
	row.RefreshToken = refreshSecret
	row.ExpiresAt = expiresAt
	row.Scope = scope
	return nil
}

// --- Status / disconnect ---

// MCPOAuthStatus reports the stored authorization state of one server, resolved
// for the requesting user, without ever exposing token material.
func (s *Service) MCPOAuthStatus(ctx context.Context, projectID, serverID, userID string) (*MCPServerOAuthStatusDTO, error) {
	server, err := s.repo.FindServerByID(ctx, serverID, &projectID)
	if err != nil {
		return nil, fmt.Errorf("loading MCP server: %w", err)
	}
	if server == nil {
		return nil, ErrServerNotFound
	}
	status := &MCPServerOAuthStatusDTO{
		ServerID: server.ID,
		Scopes:   []string{},
	}
	row, err := s.repo.FindOAuthForUser(ctx, serverID, userID)
	if err != nil {
		return nil, fmt.Errorf("loading MCP server authorization: %w", err)
	}
	if row == nil {
		return status, nil
	}
	status.Configured = true
	status.Connected = row.AccessToken != nil
	status.Shared = row.UserID == nil
	if row.UserID != nil {
		status.UserID = row.UserID
	}
	status.ClientConfigured = strings.TrimSpace(row.ClientID) != ""
	if row.AuthorizationServer != "" {
		server := row.AuthorizationServer
		status.AuthorizationServer = &server
	}
	if row.TokenEndpoint != "" {
		tokenEndpoint := row.TokenEndpoint
		status.TokenEndpoint = &tokenEndpoint
	}
	status.RegistrationSupported = row.RegistrationEndpoint != ""
	if row.Scope != "" {
		scope := row.Scope
		status.Scope = &scope
		status.Scopes = splitScope(scope)
	}
	if row.RequestedScope != "" {
		requested := row.RequestedScope
		status.RequestedScope = &requested
	}
	status.ExpiresAt = row.ExpiresAt
	updated := row.UpdatedAt
	status.UpdatedAt = &updated
	return status, nil
}

// DisconnectMCPOAuth deletes the requesting user's stored OAuth credential and
// drops any pooled connection that was using it. A user may only disconnect
// their own credential; the shared (project-wide) row is deleted only when
// userID is empty (an operator/administrative disconnect).
//
// An operator-configured requested scope set survives the disconnect: it is
// configuration, not a credential, and reconnecting must keep asking for it.
// When one is stored the row is kept with only that set (client and tokens
// cleared); otherwise the row is deleted as before.
func (s *Service) DisconnectMCPOAuth(ctx context.Context, projectID, serverID, userID string) error {
	server, err := s.repo.FindServerByID(ctx, serverID, &projectID)
	if err != nil {
		return fmt.Errorf("loading MCP server: %w", err)
	}
	if server == nil {
		return ErrServerNotFound
	}
	row, err := s.repo.FindOAuthForUser(ctx, serverID, userID)
	if err != nil {
		return fmt.Errorf("loading MCP server authorization: %w", err)
	}
	// A signed-in user may resolve the project-wide row, but it is not theirs to
	// disconnect: report it instead of silently doing nothing (the old path
	// deleted no row yet still closed pooled connections).
	if userID != "" && row != nil && row.UserID == nil {
		return ErrOAuthSharedConnection
	}
	// Only the row this caller may delete is affected: their personal row, or
	// the shared row for an empty userID.
	if row != nil && oauthUserID(row) == userID && strings.TrimSpace(row.RequestedScope) != "" {
		if err := s.repo.ClearOAuthClient(ctx, serverID, oauthUserID(row)); err != nil {
			return err
		}
	} else if err := s.repo.DeleteOAuthByServerID(ctx, serverID, userID); err != nil {
		return err
	}
	s.proxy.CloseServer(serverID)
	s.invalidateToolPool(projectID)
	s.log.Info("disconnected MCP OAuth credential",
		slog.String("project_id", projectID),
		slog.String("server_id", serverID),
	)
	return nil
}

// ProvisionOAuthClient stores a pre-registered OAuth client identity for a
// remote server and user (the shared row when userID is empty), clearing any
// token set issued to a previous client (tokens are bound to the client they
// were issued to). It returns the server's refreshed authorization status,
// which never carries token material.
//
// A blank clientID with a non-blank requestedScope sets the requested scope set
// without a client (creating a configuration-only row when none exists, so
// scopes can be chosen before the first connect); a blank clientID and blank
// scope clears an existing set. A changed set clears the stored token, because
// its grant was issued for the previous request.
func (s *Service) ProvisionOAuthClient(ctx context.Context, projectID, serverID, userID, clientID, clientSecret, requestedScope string) (*MCPServerOAuthStatusDTO, error) {
	server, err := s.repo.FindServerByID(ctx, serverID, &projectID)
	if err != nil {
		return nil, fmt.Errorf("loading MCP server: %w", err)
	}
	if server == nil {
		return nil, ErrServerNotFound
	}
	if server.Type != ServerTypeHTTP && server.Type != ServerTypeSSE {
		return nil, ErrOAuthNotSupported
	}

	clientID = strings.TrimSpace(clientID)
	requestedScope = strings.Join(strings.Fields(requestedScope), " ")
	if clientID == "" {
		if clientSecret != "" {
			return nil, ErrOAuthClientSecretRequiresClientID
		}
		return s.updateRequestedScope(ctx, projectID, serverID, userID, requestedScope)
	}

	var secret *EncryptedSecret
	if clientSecret != "" {
		secret, err = encryptSecretValue(s.encryptor, clientSecret)
		if err != nil {
			return nil, err
		}
	}

	if err := s.repo.UpsertOAuthClient(ctx, server.ID, projectID, userID, clientID, secret, requestedScope); err != nil {
		return nil, err
	}

	// Any token issued to a previous client is now invalid: drop pooled
	// connections that were using it and force a tool-pool rebuild.
	s.proxy.CloseServer(serverID)
	s.invalidateToolPool(projectID)

	return s.MCPOAuthStatus(ctx, projectID, serverID, userID)
}

// updateRequestedScope stores the operator's requested scope set without
// touching the client identity. It creates a configuration-only row when none
// exists, so an operator can choose scopes before the first connect (the
// dynamic client registration and the authorize URL then both request them). A
// blank scope clears the stored set on an existing row; with no row at all
// there is nothing to clear and the request is rejected as incomplete.
//
// When the set actually changes and a token is stored, the token is cleared:
// its grant was issued for the previous request, so keeping the connection
// "Connected" would misrepresent the server's access. The UI then offers
// Connect again, which re-authorizes with the new set.
func (s *Service) updateRequestedScope(ctx context.Context, projectID, serverID, userID, requestedScope string) (*MCPServerOAuthStatusDTO, error) {
	row, err := s.repo.FindOAuthForUser(ctx, serverID, userID)
	if err != nil {
		return nil, fmt.Errorf("loading MCP server authorization: %w", err)
	}
	if row == nil {
		if requestedScope == "" {
			return nil, ErrOAuthClientIDRequired
		}
		if err := s.repo.UpsertOAuthRequestedScope(ctx, serverID, projectID, userID, requestedScope, false); err != nil {
			return nil, err
		}
		return s.MCPOAuthStatus(ctx, projectID, serverID, userID)
	}

	owner := oauthUserID(row)
	// A signed-in user may resolve the project-wide row; editing its requested
	// set is allowed, but dropping the shared token would disconnect every user
	// of the project. The configuration applies on the next project-wide
	// reconnect instead.
	sharedForUser := userID != "" && row.UserID == nil
	changed := strings.Join(strings.Fields(row.RequestedScope), " ") != requestedScope
	clearTokens := changed && row.AccessToken != nil && !sharedForUser
	if err := s.repo.UpsertOAuthRequestedScope(ctx, serverID, projectID, owner, requestedScope, clearTokens); err != nil {
		return nil, err
	}
	if clearTokens {
		s.proxy.CloseServer(serverID)
		s.invalidateToolPool(projectID)
	}
	return s.MCPOAuthStatus(ctx, projectID, serverID, userID)
}

// ClearOAuthClient removes the requesting user's stored client identity and any
// token set, leaving the discovered endpoints in place so a later authorize can
// re-provision or re-register.
func (s *Service) ClearOAuthClient(ctx context.Context, projectID, serverID, userID string) error {
	server, err := s.repo.FindServerByID(ctx, serverID, &projectID)
	if err != nil {
		return fmt.Errorf("loading MCP server: %w", err)
	}
	if server == nil {
		return ErrServerNotFound
	}
	if err := s.repo.ClearOAuthClient(ctx, serverID, userID); err != nil {
		return err
	}
	s.proxy.CloseServer(serverID)
	s.invalidateToolPool(projectID)
	return nil
}

// --- DTO helpers ---

// withDeviceAuthorization overlays an OAuth bearer onto a device server's header
// set, replacing any statically configured Authorization header so the device
// cannot end up sending two conflicting credentials. The returned map is always
// non-nil so the pushed config is explicit about its headers.
func withDeviceAuthorization(headers map[string]any, auth string) map[string]any {
	out := make(map[string]any, len(headers)+1)
	for key, value := range headers {
		if strings.EqualFold(key, "Authorization") {
			continue
		}
		out[key] = value
	}
	out["Authorization"] = auth
	return out
}

// oauthSummaries returns the authorization summary per server id for a set of
// project servers, resolved for the requesting user (personal row first, shared
// row fallback). Servers that cannot use the flow (non-remote transports) are
// omitted so the UI only offers Connect where it can work.
func (s *Service) oauthSummaries(ctx context.Context, projectID, userID string, servers []*MCPServer) (map[string]*MCPServerOAuthSummaryDTO, error) {
	ids := make([]string, 0, len(servers))
	for _, srv := range servers {
		if srv == nil || srv.Type == ServerTypeBuiltin || srv.Type == ServerTypeStdio {
			continue
		}
		ids = append(ids, srv.ID)
	}
	rows, err := s.repo.FindOAuthByServerIDs(ctx, projectID, ids, userID)
	if err != nil {
		return nil, fmt.Errorf("loading MCP server authorizations: %w", err)
	}

	out := make(map[string]*MCPServerOAuthSummaryDTO, len(ids))
	for _, id := range ids {
		summary := &MCPServerOAuthSummaryDTO{}
		if row, ok := rows[id]; ok {
			summary.Configured = true
			summary.Connected = row.AccessToken != nil
			summary.Shared = row.UserID == nil
			summary.UserID = row.UserID
			if row.AuthorizationServer != "" {
				server := row.AuthorizationServer
				summary.AuthorizationServer = &server
			}
			if row.Scope != "" {
				scope := row.Scope
				summary.Scope = &scope
			}
			if row.RequestedScope != "" {
				requested := row.RequestedScope
				summary.RequestedScope = &requested
			}
			summary.ExpiresAt = row.ExpiresAt
		}
		out[id] = summary
	}
	return out, nil
}

// attachOAuthSummaries decorates server DTOs with their authorization summary.
func attachOAuthSummaries(dtos []*MCPServerDTO, summaries map[string]*MCPServerOAuthSummaryDTO) {
	for _, dto := range dtos {
		if dto == nil {
			continue
		}
		if summary, ok := summaries[dto.ID]; ok {
			dto.OAuth = summary
		}
	}
}

// DecorateOAuthSummaries fills the per-server authorization summary (whether a
// credential is registered/connected) on the DTOs for the given server rows,
// resolved for the requesting user. An error is returned only for a store
// failure: the summary is presentation detail, so callers may log-and-continue.
func (s *Service) DecorateOAuthSummaries(ctx context.Context, projectID, userID string, servers []*MCPServer, dtos []*MCPServerDTO) error {
	summaries, err := s.oauthSummaries(ctx, projectID, userID, servers)
	if err != nil {
		return err
	}
	attachOAuthSummaries(dtos, summaries)
	return nil
}

// logOAuthRefreshFailure records a refresh failure at warn level without the
// token material (a slog error value is already sanitized by being an error).
func logOAuthRefreshFailure(log *slog.Logger, serverID string, err error) {
	log.Warn("refreshing MCP server OAuth token failed",
		slog.String("server_id", serverID),
		logger.Error(err),
	)
}
