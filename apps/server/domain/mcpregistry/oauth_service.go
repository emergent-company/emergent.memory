package mcpregistry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
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
	// completed (unreachable server, unusable metadata). Handler maps it to 502:
	// the failure is upstream, not bad input.
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
	// https://api.dev.emergent-company.ai. The registered redirect URI is
	// <RedirectBaseURL>/api/mcp-servers/oauth/callback. Empty disables the flow.
	RedirectBaseURL string
	// LandingURL is the browser-facing URL the callback redirects to, e.g.
	// https://admin.dev.emergent-company.ai/settings/mcp-servers. Empty falls
	// back to <RedirectBaseURL>/settings/mcp-servers (single-origin setups).
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

// landingURL builds the post-callback UI URL with a one-shot outcome parameter.
func (c OAuthConfig) landingURL(outcome string) string {
	raw := strings.TrimSpace(c.LandingURL)
	if raw == "" {
		base := strings.TrimRight(strings.TrimSpace(c.RedirectBaseURL), "/")
		if base == "" {
			return ""
		}
		raw = base + "/settings/mcp-servers"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("mcp_oauth", outcome)
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
	const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"memory-mcp-oauth-probe","version":"1"}}}`

	client := oauthProbeHTTPClient(resourceURL)
	client.Timeout = oauthHTTPTimeout

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resourceURL, strings.NewReader(initBody))
	if err != nil {
		return authChallenge{}, fmt.Errorf("building probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		return authChallenge{}, fmt.Errorf("probing MCP server: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		return authChallenge{}, ErrOAuthNotRequired
	}
	ch, ok := parseBearerChallenge(resp.Header.Get("WWW-Authenticate"))
	if !ok {
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
	if row == nil {
		row = &MCPServerOAuth{
			ServerID:             server.ID,
			UserID:               userIDOrNil(userID),
			ProjectID:            projectID,
			AuthorizationServer:  req.AuthorizationServer,
			TokenEndpoint:        req.TokenEndpoint,
			RegistrationEndpoint: req.RegistrationEndpoint,
			Resource:             req.Resource,
			Scope:                strings.Join(req.Scopes, " "),
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
	if row.Scope == "" {
		row.Scope = strings.Join(req.Scopes, " ")
	}
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
	)

	return &StartMCPOAuthResponseDTO{AuthorizeURL: authorizeURL, State: state}, nil
}

// oauthClientName returns the client name used at dynamic client registration.
func (s *Service) oauthClientName() string {
	if name := strings.TrimSpace(s.oauthCfg.ClientName); name != "" {
		return name
	}
	return "Emergent Memory"
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
	serverID, _ = pending.Config["serverId"].(string)
	if serverID == "" {
		return "", "", ErrOAuthStateInvalid
	}
	projectID = pending.ProjectID
	userID := ""
	if pending.UserID != nil {
		userID = *pending.UserID
	}

	server, err := s.repo.FindServerByID(ctx, serverID, &projectID)
	if err != nil {
		return "", "", fmt.Errorf("loading MCP server: %w", err)
	}
	if server == nil {
		return "", "", ErrServerNotFound
	}
	row, err := s.repo.FindOAuthForUser(ctx, serverID, userID)
	if err != nil {
		return "", "", fmt.Errorf("loading MCP server authorization: %w", err)
	}
	if row == nil {
		return "", "", ErrOAuthStateInvalid
	}

	clientSecret, err := decryptSecretValue(s.encryptor, row.ClientSecret)
	if err != nil {
		return "", "", err
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
	if err := s.storeTokens(ctx, row, tokens); err != nil {
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
// a refresh token on the first exchange).
func (s *Service) storeTokens(ctx context.Context, row *MCPServerOAuth, tokens *oauthTokenSet) error {
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
		scope = row.Scope
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
	status.ExpiresAt = row.ExpiresAt
	updated := row.UpdatedAt
	status.UpdatedAt = &updated
	return status, nil
}

// DisconnectMCPOAuth deletes the requesting user's stored OAuth credential and
// drops any pooled connection that was using it. A user may only disconnect
// their own credential; the shared (project-wide) row is deleted only when
// userID is empty (an operator/administrative disconnect).
func (s *Service) DisconnectMCPOAuth(ctx context.Context, projectID, serverID, userID string) error {
	server, err := s.repo.FindServerByID(ctx, serverID, &projectID)
	if err != nil {
		return fmt.Errorf("loading MCP server: %w", err)
	}
	if server == nil {
		return ErrServerNotFound
	}
	if err := s.repo.DeleteOAuthByServerID(ctx, serverID, userID); err != nil {
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
func (s *Service) ProvisionOAuthClient(ctx context.Context, projectID, serverID, userID, clientID, clientSecret string) (*MCPServerOAuthStatusDTO, error) {
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

	var secret *EncryptedSecret
	if clientSecret != "" {
		secret, err = encryptSecretValue(s.encryptor, clientSecret)
		if err != nil {
			return nil, err
		}
	}

	if err := s.repo.UpsertOAuthClient(ctx, server.ID, projectID, userID, clientID, secret); err != nil {
		return nil, err
	}

	// Any token issued to a previous client is now invalid: drop pooled
	// connections that were using it and force a tool-pool rebuild.
	s.proxy.CloseServer(serverID)
	s.invalidateToolPool(projectID)

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
