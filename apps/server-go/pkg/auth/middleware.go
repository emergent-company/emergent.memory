package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
)

// AuthUser represents an authenticated user
type AuthUser struct {
	// Internal UUID primary key from user_profiles.id
	ID string `json:"id"`

	// External auth provider ID (e.g., Zitadel subject) from user_profiles.zitadel_user_id
	Sub string `json:"sub"`

	// User's email address
	Email string `json:"email,omitempty"`

	// Granted scopes from token
	Scopes []string `json:"scopes,omitempty"`

	// Project ID from X-Project-ID header
	ProjectID string `json:"projectId,omitempty"`

	// Organization ID from X-Org-ID header
	OrgID string `json:"orgId,omitempty"`

	// API token project ID (if authenticated via API token)
	APITokenProjectID string `json:"apiTokenProjectId,omitempty"`

	// API token ID (if authenticated via API token)
	APITokenID string `json:"apiTokenId,omitempty"`
}

// ContextKey for storing auth user in context
type contextKey string

const (
	UserContextKey    contextKey = "auth_user"
	ProjectContextKey contextKey = "project_context"
)

// GetUser retrieves the authenticated user from the Echo context
func GetUser(c echo.Context) *AuthUser {
	if user, ok := c.Get(string(UserContextKey)).(*AuthUser); ok {
		return user
	}
	return nil
}

// GetProjectID extracts and parses the project ID from the auth user context.
// Returns ErrUnauthorized if no user, or ErrBadRequest if no project ID.
func GetProjectID(c echo.Context) (string, error) {
	user := GetUser(c)
	if user == nil {
		return "", apperror.ErrUnauthorized
	}

	// First check API token project ID (automatically set for API token auth)
	if user.APITokenProjectID != "" {
		return user.APITokenProjectID, nil
	}

	// Then check X-Project-ID header
	if user.ProjectID == "" {
		return "", apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	return user.ProjectID, nil
}

// Middleware handles authentication for routes
type Middleware struct {
	db         bun.IDB
	cfg        *config.Config
	log        *slog.Logger
	userSvc    *UserProfileService
	zitadelSvc *ZitadelService
	debugToken string
}

// NewMiddleware creates a new auth middleware
func NewMiddleware(db bun.IDB, cfg *config.Config, log *slog.Logger, userSvc *UserProfileService) *Middleware {
	m := &Middleware{
		db:         db,
		cfg:        cfg,
		log:        log.With(logger.Scope("auth")),
		userSvc:    userSvc,
		zitadelSvc: NewZitadelService(db, cfg, log),
	}

	// Set up debug token for development
	if cfg.Debug && cfg.Zitadel.DebugToken != "" {
		m.debugToken = "Bearer " + cfg.Zitadel.DebugToken
	}

	return m
}

// RequireAuth returns middleware that requires authentication
func (m *Middleware) RequireAuth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user, err := m.authenticate(c)
			if err != nil {
				m.log.Warn("authentication failed", logger.Error(err))
				return m.authError(c, err)
			}

			// Extract project context from headers
			user.ProjectID = c.Request().Header.Get("X-Project-ID")
			user.OrgID = c.Request().Header.Get("X-Org-ID")

			// Store user in context
			c.Set(string(UserContextKey), user)

			return next(c)
		}
	}
}

// RequireProjectID returns middleware that requires X-Project-ID header
func (m *Middleware) RequireProjectID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetUser(c)
			if user == nil {
				return apperror.ErrUnauthorized
			}

			if user.ProjectID == "" {
				return echo.NewHTTPError(http.StatusBadRequest, map[string]any{
					"error": map[string]any{
						"code":    "bad_request",
						"message": "x-project-id header required",
					},
				})
			}

			return next(c)
		}
	}
}

// RequireScopes returns middleware that requires specific scopes
func (m *Middleware) RequireScopes(scopes ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetUser(c)
			if user == nil {
				return apperror.ErrUnauthorized
			}

			// Check if user has all required scopes
			userScopes := make(map[string]bool)
			for _, s := range user.Scopes {
				userScopes[s] = true
			}

			missing := []string{}
			for _, required := range scopes {
				if !userScopes[required] {
					missing = append(missing, required)
				}
			}

			if len(missing) > 0 {
				return echo.NewHTTPError(http.StatusForbidden, map[string]any{
					"error": map[string]any{
						"code":    "forbidden",
						"message": "Insufficient permissions",
						"details": map[string]any{
							"missing": missing,
						},
					},
				})
			}

			return next(c)
		}
	}
}

// authenticate extracts and validates the token from the request
func (m *Middleware) authenticate(c echo.Context) (*AuthUser, error) {
	if m.cfg.Standalone.IsEnabled() {
		if user := m.checkStandaloneAPIKey(c.Request()); user != nil {
			return user, nil
		}
	}

	token := m.extractToken(c.Request())
	if token == "" {
		return nil, apperror.ErrMissingToken
	}

	return m.validateToken(c.Request().Context(), token)
}

// extractToken extracts the bearer token from request
func (m *Middleware) extractToken(r *http.Request) string {
	// Check Authorization header first
	auth := r.Header.Get("Authorization")
	if auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
	}

	// Fall back to query parameter (for SSE endpoints)
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	return ""
}

// validateToken validates the token and returns the authenticated user
func (m *Middleware) validateToken(ctx context.Context, token string) (*AuthUser, error) {
	// 1. Check for API token (emt_ prefix)
	if strings.HasPrefix(token, "emt_") {
		return m.validateAPIToken(ctx, token)
	}

	// 2. Check for static test tokens (development only)
	if m.cfg.Debug || m.cfg.Environment != "production" {
		if user := m.checkTestToken(ctx, token); user != nil {
			return user, nil
		}
	}

	// 3. Check introspection cache
	cached, err := m.getCachedIntrospection(ctx, token)
	if err == nil && cached != nil {
		return m.ensureUserProfile(ctx, cached)
	}

	// 4. Zitadel introspection (if not disabled)
	if !m.cfg.Zitadel.DisableIntrospection {
		introspection, err := m.introspectToken(ctx, token)
		if err == nil && introspection != nil {
			// Cache the introspection result
			_ = m.cacheIntrospection(ctx, token, introspection)
			return m.ensureUserProfile(ctx, introspection)
		}
		// Log but continue to userinfo fallback
		if err != nil {
			m.log.Debug("introspection failed, trying userinfo", logger.Error(err))
		}
	}

	// 5. Userinfo endpoint as fallback (simpler, doesn't require introspection permissions)
	userInfo, err := m.zitadelSvc.GetUserInfo(ctx, token)
	if err == nil && userInfo != nil && userInfo.Sub != "" {
		claims := &TokenClaims{
			Sub:       userInfo.Sub,
			Email:     userInfo.Email,
			Scopes:    GetAllScopes(),                // Userinfo doesn't return scopes, grant all for now
			ExpiresAt: time.Now().Add(1 * time.Hour), // Default expiry
		}
		// Cache this result
		_ = m.cacheIntrospection(ctx, token, claims)
		return m.ensureUserProfile(ctx, claims)
	}

	// 6. Local JWT verification as final fallback
	claims, err := m.verifyJWT(ctx, token)
	if err != nil {
		return nil, apperror.ErrInvalidToken.WithInternal(err)
	}

	return m.ensureUserProfile(ctx, claims)
}

// TokenClaims represents parsed token claims
type TokenClaims struct {
	Sub       string    // Subject (user ID)
	Email     string    // Email address
	Scopes    []string  // Token scopes
	ExpiresAt time.Time // Token expiration
}

// validateAPIToken validates an API token (emt_* prefix)
func (m *Middleware) validateAPIToken(ctx context.Context, token string) (*AuthUser, error) {
	// Hash the token for lookup (SHA256 = 64 hex chars fits varchar(64))
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	// Query the api_tokens table
	var result struct {
		ID        string   `bun:"id"`
		UserID    string   `bun:"user_id"`
		ProjectID string   `bun:"project_id"`
		Scopes    []string `bun:"scopes,array"`
	}

	err := m.db.NewSelect().
		TableExpr("core.api_tokens").
		Column("id", "user_id", "project_id", "scopes").
		Where("token_hash = ?", tokenHash).
		Where("revoked_at IS NULL").
		Scan(ctx, &result)

	if err != nil {
		return nil, apperror.ErrInvalidToken.WithInternal(err)
	}

	// Get user profile
	user, err := m.userSvc.GetByID(ctx, result.UserID)
	if err != nil {
		return nil, apperror.ErrInvalidToken.WithInternal(err)
	}

	return &AuthUser{
		ID:                user.ID,
		Sub:               user.ZitadelUserID,
		Email:             user.Email,
		Scopes:            result.Scopes,
		APITokenProjectID: result.ProjectID,
		APITokenID:        result.ID,
	}, nil
}

// checkTestToken checks for static test tokens (development only)
// Test token mappings must match testutil.TestTokenConfigs for consistency.
func (m *Middleware) checkTestToken(ctx context.Context, token string) *AuthUser {
	// Test token patterns - keep in sync with testutil.TestTokenConfigs
	testTokens := map[string]struct {
		sub    string
		scopes []string
	}{
		// Simple tokens
		"no-scope":   {sub: "test-user-no-scope", scopes: []string{}},
		"with-scope": {sub: "test-user-with-scope", scopes: []string{"documents:read", "documents:write", "project:read"}},
		"read-only":  {sub: "test-user-read-only", scopes: []string{"documents:read", "project:read", "org:read", "chunks:read", "search:read", "graph:read"}},
		"graph-read": {sub: "test-user-graph-read", scopes: []string{"graph:read", "graph:search:read"}},
		"all-scopes": {sub: "test-user-all-scopes", scopes: GetAllScopes()},
		// E2E tokens - map to AdminUser fixture for predictable test user IDs
		"e2e-test-user":   {sub: "test-admin-user", scopes: GetAllScopes()},
		"e2e-query-token": {sub: "test-admin-user", scopes: GetAllScopes()},
	}

	// Check if token is in the known test tokens map
	if config, ok := testTokens[token]; ok {
		user, err := m.userSvc.EnsureProfile(ctx, config.sub, nil)
		if err != nil {
			m.log.Error("failed to ensure test user profile", logger.Error(err))
			return nil
		}

		return &AuthUser{
			ID:     user.ID,
			Sub:    config.sub,
			Scopes: config.scopes,
		}
	}

	// Check for dynamic e2e-* pattern (tokens not in the map above)
	// These create ad-hoc user profiles using the token as the subject ID
	if strings.HasPrefix(token, "e2e-") {
		user, err := m.userSvc.EnsureProfile(ctx, token, nil)
		if err != nil {
			m.log.Error("failed to ensure dynamic e2e user profile", logger.Error(err))
			return nil
		}

		return &AuthUser{
			ID:     user.ID,
			Sub:    token,
			Scopes: GetAllScopes(),
		}
	}

	return nil
}

func (m *Middleware) checkStandaloneAPIKey(r *http.Request) *AuthUser {
	if !m.cfg.Standalone.IsConfigured() {
		return nil
	}

	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		return nil
	}

	if apiKey != m.cfg.Standalone.APIKey {
		return nil
	}

	// Look up the standalone user's actual UUID
	ctx := r.Context()
	var userID string
	err := m.db.NewSelect().
		TableExpr("core.user_profiles").
		Column("id").
		Where("zitadel_user_id = ?", "standalone").
		Scan(ctx, &userID)

	if err != nil {
		m.log.Error("failed to lookup standalone user", logger.Error(err))
		return nil
	}

	return &AuthUser{
		ID:     userID, // Use actual UUID from database
		Sub:    "standalone",
		Email:  m.cfg.Standalone.UserEmail,
		Scopes: GetAllScopes(),
	}
}

// ensureUserProfile ensures the user has a profile and returns AuthUser
func (m *Middleware) ensureUserProfile(ctx context.Context, claims *TokenClaims) (*AuthUser, error) {
	profile := &UserProfileInfo{
		Email: claims.Email,
	}

	user, err := m.userSvc.EnsureProfile(ctx, claims.Sub, profile)
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	return &AuthUser{
		ID:     user.ID,
		Sub:    claims.Sub,
		Email:  claims.Email,
		Scopes: claims.Scopes,
	}, nil
}

// getCachedIntrospection retrieves cached introspection result
func (m *Middleware) getCachedIntrospection(ctx context.Context, token string) (*TokenClaims, error) {
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	var result struct {
		IntrospectionData map[string]any `bun:"introspection_data,type:jsonb"`
		ExpiresAt         time.Time      `bun:"expires_at"`
	}

	err := m.db.NewSelect().
		TableExpr("kb.auth_introspection_cache").
		Column("introspection_data", "expires_at").
		Where("token_hash = ?", tokenHash).
		Where("expires_at > NOW()").
		Scan(ctx, &result)

	if err != nil {
		return nil, err
	}

	// Parse cached data into TokenClaims
	claims := &TokenClaims{}
	if sub, ok := result.IntrospectionData["sub"].(string); ok {
		claims.Sub = sub
	}
	if email, ok := result.IntrospectionData["email"].(string); ok {
		claims.Email = email
	}
	if scope, ok := result.IntrospectionData["scope"].(string); ok {
		claims.Scopes = strings.Split(scope, " ")
	}
	claims.ExpiresAt = result.ExpiresAt

	return claims, nil
}

// cacheIntrospection stores introspection result in cache
func (m *Middleware) cacheIntrospection(ctx context.Context, token string, claims *TokenClaims) error {
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	// Calculate TTL (use token expiration or config TTL, whichever is sooner)
	ttl := m.cfg.Zitadel.IntrospectCacheTTL
	tokenTTL := time.Until(claims.ExpiresAt)
	if tokenTTL > 0 && tokenTTL < ttl {
		ttl = tokenTTL
	}

	expiresAt := time.Now().Add(ttl)

	data := map[string]any{
		"sub":   claims.Sub,
		"email": claims.Email,
		"scope": strings.Join(claims.Scopes, " "),
	}

	_, err := m.db.NewInsert().
		TableExpr("kb.auth_introspection_cache").
		Model(&struct {
			TokenHash         string         `bun:"token_hash"`
			IntrospectionData map[string]any `bun:"introspection_data,type:jsonb"`
			ExpiresAt         time.Time      `bun:"expires_at"`
		}{
			TokenHash:         tokenHash,
			IntrospectionData: data,
			ExpiresAt:         expiresAt,
		}).
		On("CONFLICT (token_hash) DO UPDATE").
		Set("introspection_data = EXCLUDED.introspection_data").
		Set("expires_at = EXCLUDED.expires_at").
		Exec(ctx)

	return err
}

// introspectToken calls Zitadel to introspect the token
func (m *Middleware) introspectToken(ctx context.Context, token string) (*TokenClaims, error) {
	result, err := m.zitadelSvc.Introspect(ctx, token)
	if err != nil {
		return nil, err
	}

	// nil result means introspection is disabled or unavailable
	if result == nil {
		return nil, errors.New("introspection unavailable")
	}

	// Inactive token
	if !result.Active {
		return nil, errors.New("token is inactive")
	}

	return &TokenClaims{
		Sub:       result.Sub,
		Email:     result.Email,
		Scopes:    ParseScopes(result.Scope),
		ExpiresAt: time.Unix(result.Exp, 0),
	}, nil
}

// verifyJWT verifies the token using local JWKS
func (m *Middleware) verifyJWT(ctx context.Context, token string) (*TokenClaims, error) {
	// For now, JWT verification is not implemented
	// The primary auth flow uses:
	// 1. Test tokens (development)
	// 2. API tokens (emt_* prefix)
	// 3. Cached introspection results
	// 4. Live introspection (if enabled)
	//
	// JWT verification would be a fallback using JWKS:
	// - Fetch JWKS from {issuer}/.well-known/jwks.json
	// - Verify token signature
	// - Validate claims (iss, aud, exp)
	//
	// This requires go-jose library and JWKS caching.
	// TODO: Implement if introspection is insufficient
	return nil, errors.New("JWT verification not implemented - enable introspection or use test tokens")
}

// authError returns a formatted authentication error
func (m *Middleware) authError(c echo.Context, err error) error {
	status, body := apperror.ToHTTPError(err)
	return c.JSON(status, body)
}

// GetAllScopes returns all available scopes (for test tokens)
func GetAllScopes() []string {
	return []string{
		"org:read",
		"org:project:create",
		"org:project:delete",
		"org:invite:create",
		"project:read",
		"project:invite:create",
		"documents:read",
		"documents:write",
		"documents:delete",
		"ingest:write",
		"search:read",
		"search:debug",
		"chunks:read",
		"chunks:write",
		"chat:use",
		"chat:admin",
		"graph:read",
		"graph:write",
		"graph:search:read",
		"graph:search:debug",
		"notifications:read",
		"notifications:write",
		"extraction:read",
		"extraction:write",
		"schema:read",
		"data:read",
		"data:write",
		"mcp:admin",
		"user-activity:read",
		"user-activity:write",
		"tasks:read",
		"tasks:write",
		"discovery:read",
		"discovery:write",
		"admin:read",
		"admin:write",
	}
}
