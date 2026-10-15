package mcpregistry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// This file wires a server's stored OAuth credential into the outbound path:
// resolving the header set for a connection, refreshing an expired token, and
// classifying an authorization failure during inspect.

// userIDFromContext extracts the authenticated user's id from the request
// context. It returns "" for a context with no user (system/background runs,
// device config resolution), which selects the shared/project-wide credential.
func userIDFromContext(ctx context.Context) string {
	if u := auth.UserFromContext(ctx); u != nil {
		return u.ID
	}
	return ""
}

// oauthPoolKey builds the connection-pool key for a server + caller. A caller
// with no user id shares the plain server key (the shared credential), while a
// caller with a user id gets a per-user key so two users never share a pooled
// connection carrying one user's bearer.
func oauthPoolKey(serverID, userID string) string {
	if userID == "" {
		return serverID
	}
	return serverID + "\x00" + userID
}

// oauthRefreshLocks serializes token refreshes per server. Two concurrent tool
// calls on an expired token must not both hit the token endpoint (the second
// refresh could invalidate the first token).
type oauthRefreshLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (l *oauthRefreshLocks) get(serverID string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.locks == nil {
		l.locks = make(map[string]*sync.Mutex)
	}
	if m, ok := l.locks[serverID]; ok {
		return m
	}
	m := &sync.Mutex{}
	l.locks[serverID] = m
	return m
}

// refreshLocks guards per-server token refreshes.
var refreshLocks = oauthRefreshLocks{}

// requestHeaders resolves the header set for an outbound connection to a server:
// the configured plaintext headers, the decrypted secret headers, and — when the
// server has a stored OAuth credential — a valid Bearer token.
//
// A stored OAuth credential owns the Authorization header: a statically
// configured Authorization value is dropped rather than sent alongside the
// bearer, so the two can never conflict at the resource server.
func (pm *ProxyManager) requestHeaders(ctx context.Context, server *MCPServer) (map[string]string, error) {
	headers, err := pm.decryptHeaders(server)
	if err != nil {
		return nil, err
	}
	auth, err := pm.oauthAuthorizationHeader(ctx, server)
	if err != nil {
		return nil, err
	}
	if auth == "" {
		return headers, nil
	}
	if headers == nil {
		headers = make(map[string]string, 1)
	}
	for key := range headers {
		if strings.EqualFold(key, "Authorization") {
			delete(headers, key)
		}
	}
	headers["Authorization"] = auth
	return headers, nil
}

// oauthAuthorizationHeader returns the `Authorization: Bearer <token>` value for
// a server with a stored OAuth credential, refreshing it when expired or close
// to expiry. It returns "" for a server with no credential.
//
// A refresh that cannot succeed (no refresh token, or a rejected one) surfaces
// ErrOAuthNeedsReconnect so the caller can tell the user to authorize the server
// again rather than reporting a bare 401.
func (pm *ProxyManager) oauthAuthorizationHeader(ctx context.Context, server *MCPServer) (string, error) {
	if server == nil || server.Type == ServerTypeStdio || server.Type == ServerTypeBuiltin {
		return "", nil
	}
	userID := userIDFromContext(ctx)
	row, err := pm.repo.FindOAuthForUser(ctx, server.ID, userID)
	if err != nil {
		return "", fmt.Errorf("loading MCP server authorization: %w", err)
	}
	if row == nil || row.AccessToken == nil {
		return "", nil
	}

	if !oauthTokenExpired(row.ExpiresAt) {
		token, err := decryptSecretValue(pm.encryptor, row.AccessToken)
		if err != nil {
			return "", err
		}
		if token != "" {
			return "Bearer " + token, nil
		}
	}

	// Refresh under a per-server lock so concurrent calls issue one refresh.
	lock := refreshLocks.get(server.ID)
	lock.Lock()
	defer lock.Unlock()

	// Re-read: another goroutine may have refreshed while we waited.
	row, err = pm.repo.FindOAuthForUser(ctx, server.ID, userID)
	if err != nil {
		return "", fmt.Errorf("loading MCP server authorization: %w", err)
	}
	if row == nil || row.AccessToken == nil {
		return "", ErrOAuthNeedsReconnect
	}
	if !oauthTokenExpired(row.ExpiresAt) {
		token, err := decryptSecretValue(pm.encryptor, row.AccessToken)
		if err != nil {
			return "", err
		}
		if token != "" {
			return "Bearer " + token, nil
		}
	}

	if row.RefreshToken == nil || row.TokenEndpoint == "" {
		return "", ErrOAuthNeedsReconnect
	}
	refreshToken, err := decryptSecretValue(pm.encryptor, row.RefreshToken)
	if err != nil {
		return "", err
	}
	if refreshToken == "" {
		return "", ErrOAuthNeedsReconnect
	}
	clientSecret, err := decryptSecretValue(pm.encryptor, row.ClientSecret)
	if err != nil {
		return "", err
	}

	tokens, err := refreshAccessToken(ctx, row.TokenEndpoint,
		&oauthClientIdentity{ClientID: row.ClientID, ClientSecret: clientSecret},
		refreshToken, row.Resource)
	if err != nil {
		logOAuthRefreshFailure(pm.log, server.ID, err)
		return "", fmt.Errorf("%w: %v", ErrOAuthNeedsReconnect, err)
	}

	access, err := encryptSecretValue(pm.encryptor, tokens.AccessToken)
	if err != nil {
		return "", err
	}
	refreshSecret := row.RefreshToken
	if tokens.RefreshToken != "" {
		refreshSecret, err = encryptSecretValue(pm.encryptor, tokens.RefreshToken)
		if err != nil {
			return "", err
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
	if err := pm.repo.UpdateOAuthTokens(ctx, server.ID, oauthUserID(row), access, refreshSecret, scope, expiresAt); err != nil {
		return "", err
	}
	// Every pooled connection to this server may still carry the previous
	// bearer, so drop them all; the next call reconnects with the refreshed one.
	pm.CloseServer(server.ID)

	pm.log.Info("refreshed MCP server OAuth token", slog.String("server_id", server.ID))
	return "Bearer " + tokens.AccessToken, nil
}

// oauthTokenExpired reports whether a stored expiry is within the refresh skew
// of now. A nil expiry (an authorization server that issued no expires_in) is
// never treated as expired.
func oauthTokenExpired(expiresAt *time.Time) bool {
	if expiresAt == nil {
		return false
	}
	return time.Now().UTC().Add(oauthExpirySkew).After(*expiresAt)
}

// classifyAuthRequired maps a failed connection to an authorization requirement
// when the server answered with an OAuth challenge. It returns nil when the
// failure is not an OAuth rejection, leaving the original error in place.
func classifyAuthRequired(ctx context.Context, pm *ProxyManager, server *MCPServer, connErr error) *InspectAuthDTO {
	if !isAuthRejection(connErr) {
		return nil
	}
	req, err := discoverServerAuthRequirement(ctx, server)
	if err != nil {
		// A provider that publishes no discoverable OAuth metadata is still an
		// authorization requirement — the server rejected us — but the browser
		// connect flow cannot work. Report it as unavailable (with the
		// token-header remedy) so the UI can disable Connect instead of offering
		// a flow that would fail.
		if errors.Is(err, ErrOAuthMetadataUnavailable) {
			return &InspectAuthDTO{Unavailable: true, Remedy: oauthMetadataUnavailableRemedy}
		}
		// The server rejected us but advertised nothing usable: keep the
		// original credential error rather than inventing a Connect affordance.
		if !isAuthRejection(err) {
			pm.log.Debug("inspecting OAuth metadata after auth rejection failed",
				slog.String("server_id", server.ID),
				slog.String("error", err.Error()))
		}
		return nil
	}
	return &InspectAuthDTO{
		ResourceMetadataURL:   req.ResourceMetadataURL,
		AuthorizationServer:   req.AuthorizationServer,
		Scopes:                nonNilStrings(req.Scopes),
		RegistrationSupported: req.RegistrationSupported,
	}
}

// isAuthRejection reports whether an error chain contains an HTTP 401/403
// rejection from the remote MCP server.
func isAuthRejection(err error) bool {
	var rejected *ErrMCPAuthRejected
	return err != nil && errors.As(err, &rejected)
}

// classifyInstallAuth classifies a tool-discovery error for the registry
// install flow into the install result's auth flags. It mirrors
// classifyAuthRequired (the inspect path) but returns booleans the install
// result surfaces: requiresAuthorization when the server answered an OAuth
// challenge with discoverable metadata (the connect flow works), and
// requiresTokenHeader when the server rejected us but publishes no discoverable
// OAuth metadata (the browser connect flow cannot work — a token header is the
// remedy). A non-auth error or an unusable challenge yields neither flag.
func classifyInstallAuth(ctx context.Context, server *MCPServer, err error) (requiresAuthorization, requiresTokenHeader bool) {
	if !isAuthRejection(err) {
		return false, false
	}
	if _, authErr := discoverServerAuthRequirement(ctx, server); authErr == nil {
		return true, false
	} else if errors.Is(authErr, ErrOAuthMetadataUnavailable) {
		return false, true
	}
	return false, false
}

// nonNilStrings returns a non-nil slice so JSON emits [] rather than null.
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
