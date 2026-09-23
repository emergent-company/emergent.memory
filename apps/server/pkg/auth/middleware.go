package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
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

// HasScope returns true if the user has the given scope (respecting scope expansion).
func (u *AuthUser) HasScope(scope string) bool {
	return expandScopes(u.Scopes)[scope]
}

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

// tokenIntrospector is the subset of ZitadelService used by token validation.
// It is an interface so both the introspection and userinfo paths can be faked
// in unit tests.
type tokenIntrospector interface {
	Introspect(ctx context.Context, token string) (*IntrospectionResult, error)
	GetUserInfo(ctx context.Context, accessToken string) (*UserInfoResult, error)
}

// userProfileEnsurer is the subset of UserProfileService used by authentication.
// It is an interface so the token-validation pipeline can be exercised with a
// fake profile store in unit tests.
type userProfileEnsurer interface {
	GetByID(ctx context.Context, id string) (*UserProfile, error)
	EnsureProfile(ctx context.Context, subjectID string, info *UserProfileInfo) (*UserProfile, bool, error)
}

// Middleware handles authentication for routes
type Middleware struct {
	db               bun.IDB
	cfg              *config.Config
	log              *slog.Logger
	userSvc          userProfileEnsurer
	zitadelSvc       tokenIntrospector
	autoProvisionSvc AutoProvisionService
	debugToken       string

	// roleLookup is a test seam for the project membership role query.
	// When nil, dbProjectRole is used.
	roleLookup projectRoleLookup
}

// MiddlewareParams holds the dependencies for creating the auth middleware.
type MiddlewareParams struct {
	fx.In
	DB               bun.IDB
	Cfg              *config.Config
	Log              *slog.Logger
	UserSvc          *UserProfileService
	AutoProvisionSvc AutoProvisionService `optional:"true"`
}

// NewMiddleware creates a new auth middleware
func NewMiddleware(p MiddlewareParams) *Middleware {
	m := &Middleware{
		db:               p.DB,
		cfg:              p.Cfg,
		log:              p.Log.With(logger.Scope("auth")),
		userSvc:          p.UserSvc,
		zitadelSvc:       NewZitadelService(p.DB, p.Cfg, p.Log),
		autoProvisionSvc: p.AutoProvisionSvc,
	}

	// Set up debug token for development
	if p.Cfg.Debug && p.Cfg.Zitadel.DebugToken != "" {
		m.debugToken = "Bearer " + p.Cfg.Zitadel.DebugToken
	}

	m.warnIfOIDCAllGrantActive()

	return m
}

// shareAgentChatScope is the reserved marker scope minted on public agent-share
// link keys. It is only valid under /api/share/agent (see RequireAuth).
const shareAgentChatScope = "share:agent-chat"

// hasShareChatScope reports whether scopes contains the share:agent-chat marker.
func hasShareChatScope(scopes []string) bool {
	for _, s := range scopes {
		if s == shareAgentChatScope {
			return true
		}
	}
	return false
}

// rejectShareTokenOutsideSurface returns a 403 error when the request path is
// outside /api/share/agent but the token carries the share:agent-chat marker.
// It is a pure function so it can be unit-tested independently of the auth
// pipeline.
func rejectShareTokenOutsideSurface(path string, scopes []string) error {
	if !hasShareChatScope(scopes) {
		return nil
	}
	if strings.HasPrefix(path, "/api/share/agent") {
		return nil
	}
	return apperror.NewForbidden("share:agent-chat credentials are only valid on share endpoints")
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

			// If authenticated via API token, normalize the project ID so all handlers
			// can use user.ProjectID consistently without checking APITokenProjectID.
			if user.ProjectID == "" && user.APITokenProjectID != "" {
				user.ProjectID = user.APITokenProjectID
			}

			// If OrgID is not set via header, resolve it from the project ID.
			// This handles standalone mode (no API token) and API token auth.
			projectIDForOrg := user.ProjectID
			if projectIDForOrg == "" {
				projectIDForOrg = user.APITokenProjectID
			}
			// Last resort: use the :projectId URL path param (e.g. /api/projects/:projectId/remember).
			// This covers standalone mode where neither header nor token carries a project ID.
			if projectIDForOrg == "" {
				projectIDForOrg = c.Param("projectId")
			}
			if user.OrgID == "" && projectIDForOrg != "" {
				var orgID string
				err := m.db.NewSelect().
					TableExpr("kb.projects").
					Column("organization_id").
					Where("id = ?", projectIDForOrg).
					Scan(c.Request().Context(), &orgID)
				if err == nil && orgID != "" {
					user.OrgID = orgID
				}
			}

			// Store user in Echo context (for handler-layer access via GetUser(c))
			c.Set(string(UserContextKey), user)

			// Fail closed: a share:agent-chat token is only valid under
			// /api/share/agent. Reject it anywhere else (defense in depth — the
			// key also carries no other scopes) so a leaked share key cannot reach
			// project/member endpoints. Owner-management routes use normal user
			// tokens and are unaffected.
			if err := rejectShareTokenOutsideSurface(c.Request().URL.Path, user.Scopes); err != nil {
				return m.authError(c, err)
			}

			// Inject auth data into the request's context.Context so
			// downstream service layers can access user, project ID,
			// and org ID without an Echo dependency.
			enrichedCtx := InjectAuthContext(c.Request().Context(), user)

			// Also store the raw bearer/API token so internal loopback
			// HTTP calls (e.g. MCP search-knowledge → /query) can forward
			// the original credential.
			if rawToken := m.extractToken(c.Request()); rawToken != "" {
				enrichedCtx = ContextWithRawToken(enrichedCtx, rawToken)
			}

			c.SetRequest(c.Request().WithContext(enrichedCtx))

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

// RequireProjectScope returns middleware that enforces API token project scope.
// For emt_* tokens, it validates that the :projectId URL param matches the token's project.
// For non-API-token auth (e.g. OAuth sessions), this is a no-op pass-through.
func (m *Middleware) RequireProjectScope() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetUser(c)
			if user == nil {
				return apperror.ErrUnauthorized
			}

			// Only enforce for API token auth (emt_* tokens)
			if user.APITokenProjectID == "" {
				return next(c)
			}

			// Check if the route has a :projectId param
			projectID := c.Param("projectId")
			if projectID == "" {
				return next(c)
			}

			// Validate the URL project matches the token's project
			if projectID != user.APITokenProjectID {
				return echo.NewHTTPError(http.StatusForbidden, map[string]any{
					"error": map[string]any{
						"code":    "forbidden",
						"message": "API token is scoped to a different project",
						"details": map[string]any{
							"token_project_id":     user.APITokenProjectID,
							"requested_project_id": projectID,
						},
					},
				})
			}

			return next(c)
		}
	}
}

// ScopeImplies is the single canonical umbrella-scope implication relation: it
// maps each umbrella scope (e.g. "data:read") to the fine-grained scopes it
// covers. When an actor holds an umbrella scope, they effectively hold every
// scope it implies. This is the ONLY place the relation is defined — package
// consumers (e.g. domain/mcp) must derive their view from it via ExpandScopes
// instead of maintaining a parallel implication table.
var ScopeImplies = map[string][]string{
	"data:read": {
		"documents:read",
		"chunks:read",
		"search:read",
		"graph:read",
		"graph:search:read",
		"extraction:read",
		"schema:read",
		"tasks:read",
		"user-activity:read",
		"notifications:read",
		// MCP fine-grained
		"search",
		"journal:read",
	},
	"data:write": {
		"documents:write",
		"documents:delete",
		"chunks:write",
		"graph:write",
		"ingest:write",
		"extraction:write",
		"tasks:write",
		"user-activity:write",
		"notifications:write",
		"schema:write",
		// MCP fine-grained
		"journal:write",
	},
	"schema:write": {
		// schema:write also grants migration access
		"schema:migrate",
	},
	"agents:read": {
		"chat:use",
		// MCP fine-grained
		"skills:read",
	},
	"agents:write": {
		"chat:admin",
		// MCP fine-grained
		"skills:write",
	},
	"projects:write": {
		"projects:read",
	},
	// MCP fine-grained umbrella scopes
	"graph:write": {
		"graph:read",
	},
	"branches:write": {
		"branches:read",
	},
	"journal:write": {
		"journal:read",
	},
	"skills:write": {
		"skills:read",
	},
	"documents:write": {
		"documents:read",
	},
	"admin:all": {
		"admin", "admin:read", "admin:write",
		"agents:read", "agents:write",
		"branches:read", "branches:write",
		"chat:admin", "chat:use",
		"chunks:read", "chunks:write",
		"data:read", "data:write",
		"discovery:read", "discovery:write",
		"documents:delete", "documents:read", "documents:write",
		"extraction:read", "extraction:write",
		"graph:read", "graph:search:debug", "graph:search:read", "graph:write",
		"ingest:write",
		"journal:read", "journal:write",
		"mcp:admin",
		"notifications:read", "notifications:write",
		"org:invite:create", "org:project:create", "org:project:delete", "org:read",
		"project:invite:create", "project:read",
		"projects:read", "projects:write",
		"schema:migrate", "schema:read", "schema:write",
		"search", "search:debug", "search:read",
		"skills:read", "skills:write",
		"tasks:read", "tasks:write",
		"user-activity:read", "user-activity:write",
	},
}

// expandScopes returns the full set of scopes a user effectively has,
// including all scopes implied by umbrella scopes like "data:read".
func expandScopes(scopes []string) map[string]bool {
	result := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		result[s] = true
		for _, implied := range ScopeImplies[s] {
			result[implied] = true
		}
	}
	return result
}

// ExpandScopes returns the full set of scopes an actor effectively has,
// including every scope implied by an umbrella scope (see ScopeImplies).
// ExpandScopes is the single source of truth for umbrella-scope expansion;
// package consumers (e.g. domain/mcp) MUST derive their view from it instead
// of maintaining a parallel implication table.
func ExpandScopes(scopes []string) map[string]bool {
	return expandScopes(scopes)
}

// RequireAPITokenScopes returns middleware that requires specific scopes ONLY when the
// request is authenticated via an emt_* API token. For Zitadel/OAuth sessions the check
// is skipped, preserving backward compatibility.
// Use this on routes that should be accessible to account-level API tokens but only when
// the token explicitly carries the right scope (e.g. projects:read on GET /api/projects).
func (m *Middleware) RequireAPITokenScopes(scopes ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetUser(c)
			if user == nil {
				return apperror.ErrUnauthorized
			}

			// Only enforce for API token auth (emt_* tokens identified by APITokenID)
			if user.APITokenID == "" {
				return next(c)
			}

			// Build the effective scope set, expanding umbrella scopes.
			userScopes := expandScopes(user.Scopes)

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

// RequireScopes returns middleware that requires specific scopes
func (m *Middleware) RequireScopes(scopes ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetUser(c)
			if user == nil {
				return apperror.ErrUnauthorized
			}

			// Build the effective scope set, expanding umbrella scopes.
			userScopes := expandScopes(user.Scopes)

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
		// Also accept the standalone API key presented as a Bearer token
		// (e.g. from the CLI which uses Authorization: Bearer for emt_* tokens).
		if token := m.extractToken(c.Request()); token != "" && token == m.cfg.Standalone.APIKey {
			if user := m.checkStandaloneAPIKey(m.requestWithXAPIKey(c.Request(), token)); user != nil {
				return user, nil
			}
		}
	}

	token := m.extractToken(c.Request())
	if token == "" {
		return nil, apperror.ErrMissingToken
	}

	// The declared project (X-Project-ID) is used to derive role-based scopes
	// for OIDC sessions. It may be empty for account-level requests.
	return m.validateToken(c.Request().Context(), token, c.Request().Header.Get("X-Project-ID"))
}

// requestWithXAPIKey returns a shallow copy of r with X-API-Key set to key.
func (m *Middleware) requestWithXAPIKey(r *http.Request, key string) *http.Request {
	r2 := r.Clone(r.Context())
	r2.Header.Set("X-API-Key", key)
	return r2
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

	// Check X-API-Key header (accepted for emt_* API tokens; used by MCP clients like mcp-remote)
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		return apiKey
	}

	// Fall back to query parameter (for SSE endpoints)
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	return ""
}

// validateToken validates the token and returns the authenticated user.
// projectID is the request's declared project (X-Project-ID) and is used to
// derive role-based scopes for OIDC sessions; it may be empty.
func (m *Middleware) validateToken(ctx context.Context, token, projectID string) (*AuthUser, error) {
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
		return m.finalizeOIDCUser(ctx, cached, projectID)
	}

	// 4. Zitadel introspection (if not disabled)
	if !m.cfg.Zitadel.DisableIntrospection {
		introspection, err := m.introspectToken(ctx, token)
		if err == nil && introspection != nil {
			// Cache raw claims only; derived scopes are re-resolved per request.
			_ = m.cacheIntrospection(ctx, token, introspection)
			return m.finalizeOIDCUser(ctx, introspection, projectID)
		}
		// Log but continue to userinfo fallback
		if err != nil {
			m.log.Debug("introspection failed, trying userinfo", logger.Error(err))
		}
	}

	// 5. Userinfo endpoint as fallback (simpler, doesn't require introspection permissions).
	// The userinfo response carries no scopes; they are resolved by finalizeOIDCUser.
	userInfo, err := m.zitadelSvc.GetUserInfo(ctx, token)
	if err == nil && userInfo != nil && userInfo.Sub != "" {
		claims := &TokenClaims{
			Sub:        userInfo.Sub,
			Email:      userInfo.Email,
			ExpiresAt:  time.Now().Add(1 * time.Hour), // Default expiry
			GivenName:  userInfo.GivenName,
			FamilyName: userInfo.FamilyName,
			Name:       userInfo.Name,
			AuthSource: authSourceUserinfo,
		}
		// Cache raw identity claims only — never derived scopes.
		_ = m.cacheIntrospection(ctx, token, claims)
		return m.finalizeOIDCUser(ctx, claims, projectID)
	}

	// 6. Local JWT verification as final fallback
	claims, err := m.verifyJWT(ctx, token)
	if err != nil {
		return nil, apperror.ErrInvalidToken.WithInternal(err)
	}

	return m.finalizeOIDCUser(ctx, claims, projectID)
}

// finalizeOIDCUser ensures the user profile exists, then resolves the effective
// Memory scopes for the session. Resolution happens here (not in the cache) so
// that cached raw claims are re-derived on every request and role changes take
// effect immediately.
func (m *Middleware) finalizeOIDCUser(ctx context.Context, claims *TokenClaims, projectID string) (*AuthUser, error) {
	user, err := m.ensureUserProfile(ctx, claims)
	if err != nil {
		return nil, err
	}

	// Legacy all-or-nothing grant: only for the userinfo path and only while
	// introspection is unconfigured (single-user pilot posture). Enabling
	// introspection disables it.
	if claims.AuthSource == authSourceUserinfo && m.oidcAllGrantEnabled() {
		user.Scopes = GetAllScopes()
		return user, nil
	}

	user.Scopes = m.resolveOIDCScopes(ctx, user.ID, projectID, claims.Scopes)
	return user, nil
}

// oidcAuthSource records which validation path produced a set of claims, so the
// legacy userinfo all-grant can be applied consistently across cache hits.
type oidcAuthSource string

const (
	authSourceIntrospection oidcAuthSource = "introspection"
	authSourceUserinfo      oidcAuthSource = "userinfo"
)

// oidcAllGrantWarningText is the operator-facing warning emitted when the legacy
// userinfo all-grant is active. It names the effect and both remediations.
const oidcAllGrantWarningText = "OIDC all-scope grant is ACTIVE: ZITADEL_USERINFO_GRANT_ALL_SCOPES is enabled and token introspection is not configured, so every OIDC user authenticated via the userinfo fallback receives the full Memory scope catalogue (GetAllScopes). Remediate by configuring ZITADEL_CLIENT_JWT (or ZITADEL_CLIENT_JWT_PATH) to enable introspection (DISABLE_ZITADEL_INTROSPECTION must not be enabled), or by setting ZITADEL_USERINFO_GRANT_ALL_SCOPES=false."

// oidcAllGrantWarning returns the startup warning to emit when the legacy
// userinfo all-grant is active, or "" when it is not (flag disabled, or
// introspection configured).
func oidcAllGrantWarning(z *config.ZitadelConfig) string {
	if !z.UserinfoAllGrantActive() {
		return ""
	}
	return oidcAllGrantWarningText
}

// warnIfOIDCAllGrantActive emits the loud startup warning when the legacy
// userinfo all-grant is in effect. Visibility only — it changes no behaviour.
func (m *Middleware) warnIfOIDCAllGrantActive() {
	if m.cfg == nil {
		return
	}
	if msg := oidcAllGrantWarning(&m.cfg.Zitadel); msg != "" {
		m.log.Warn(msg,
			slog.String("config", "ZITADEL_USERINFO_GRANT_ALL_SCOPES"),
			slog.Bool("introspection_configured", false),
		)
	}
}

// TokenClaims represents parsed token claims
type TokenClaims struct {
	Sub        string    // Subject (user ID)
	Email      string    // Email address
	Scopes     []string  // Token scopes
	ExpiresAt  time.Time // Token expiration
	GivenName  string    // First name from OIDC claims
	FamilyName string    // Last name from OIDC claims
	Name       string    // Display name from OIDC claims

	// AuthSource identifies the validation path that produced these claims.
	AuthSource oidcAuthSource
}

// validateAPIToken validates an API token (emt_* prefix)
func (m *Middleware) validateAPIToken(ctx context.Context, token string) (*AuthUser, error) {
	// Hash the token for lookup (SHA256 = 64 hex chars fits varchar(64))
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	// Query the api_tokens table
	var result struct {
		ID        string   `bun:"id"`
		UserID    *string  `bun:"user_id"`    // nullable: nil for ephemeral sandbox tokens
		ProjectID *string  `bun:"project_id"` // nullable: nil for account-level tokens
		Scopes    []string `bun:"scopes,array"`
	}

	err := m.db.NewSelect().
		TableExpr("core.api_tokens").
		Column("id", "user_id", "project_id", "scopes").
		Where("token_hash = ?", tokenHash).
		Where("revoked_at IS NULL").
		Where("(expires_at IS NULL OR expires_at > NOW())").
		Scan(ctx, &result)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			m.log.Warn("API token not found or expired", slog.String("token_prefix", token[:min(8, len(token))]))
		} else {
			m.log.Error("API token DB lookup failed", logger.Error(err))
		}
		return nil, apperror.ErrInvalidToken.WithInternal(err)
	}

	// Resolve project ID: account-level tokens have a nil project_id
	projectID := ""
	if result.ProjectID != nil {
		projectID = *result.ProjectID
	}

	// Ephemeral sandbox tokens have user_id = NULL (no real user owner).
	// For these tokens we construct an AuthUser directly without a profile lookup.
	if result.UserID == nil {
		return &AuthUser{
			ID:                "",
			Sub:               "",
			Scopes:            result.Scopes,
			APITokenProjectID: projectID,
			APITokenID:        result.ID,
		}, nil
	}

	// Get user profile
	user, err := m.userSvc.GetByID(ctx, *result.UserID)
	if err != nil {
		return nil, apperror.ErrInvalidToken.WithInternal(err)
	}

	return &AuthUser{
		ID:                user.ID,
		Sub:               user.ZitadelUserID,
		Email:             user.Email,
		Scopes:            result.Scopes,
		APITokenProjectID: projectID,
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
		user, _, err := m.userSvc.EnsureProfile(ctx, config.sub, nil)
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
		user, _, err := m.userSvc.EnsureProfile(ctx, token, nil)
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

	if m.db == nil {
		// No database connection available — fall back to using "standalone" as ID
		return &AuthUser{
			ID:     "standalone",
			Sub:    "standalone",
			Email:  m.cfg.Standalone.UserEmail,
			Scopes: GetAllScopes(),
		}
	}

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
		Email:       claims.Email,
		FirstName:   claims.GivenName,
		LastName:    claims.FamilyName,
		DisplayName: claims.Name,
	}

	user, isNew, err := m.userSvc.EnsureProfile(ctx, claims.Sub, profile)
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	// Auto-provision default org and project for brand-new users
	if isNew && m.autoProvisionSvc != nil {
		if provErr := m.autoProvisionSvc.ProvisionNewUser(ctx, user.ID, profile); provErr != nil {
			// Log but do not block authentication
			m.log.Error("auto-provision failed for new user",
				slog.String("userID", user.ID),
				logger.Error(provErr),
			)
		}
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
	if m.db == nil {
		return nil, errors.New("auth: no database available for introspection cache")
	}

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

	// Rehydrate raw claims (never derived scopes).
	return claimsFromCacheData(result.IntrospectionData, result.ExpiresAt), nil
}

// claimsToCacheData serialises the raw claims persisted in the introspection
// cache. Derived scopes are never written: callers pass claims whose Scopes hold
// the raw OIDC scope list (introspection) or nothing (userinfo).
func claimsToCacheData(claims *TokenClaims) map[string]any {
	return map[string]any{
		"sub":         claims.Sub,
		"email":       claims.Email,
		"scope":       strings.Join(claims.Scopes, " "),
		"given_name":  claims.GivenName,
		"family_name": claims.FamilyName,
		"name":        claims.Name,
		"auth_source": string(claims.AuthSource),
	}
}

// claimsFromCacheData rehydrates TokenClaims from a cached entry.
//
// An entry with no auth_source predates this change. Pre-change, the userinfo
// path cached the full scope catalogue; replaying that as an "explicit Memory
// grant" would silently widen access right after a deploy. Legacy entries are
// therefore treated as identity-only introspection entries with NO scopes, which
// forces re-resolution (fail closed).
func claimsFromCacheData(data map[string]any, expiresAt time.Time) *TokenClaims {
	claims := &TokenClaims{ExpiresAt: expiresAt}
	if sub, ok := data["sub"].(string); ok {
		claims.Sub = sub
	}
	if email, ok := data["email"].(string); ok {
		claims.Email = email
	}
	if givenName, ok := data["given_name"].(string); ok {
		claims.GivenName = givenName
	}
	if familyName, ok := data["family_name"].(string); ok {
		claims.FamilyName = familyName
	}
	if name, ok := data["name"].(string); ok {
		claims.Name = name
	}

	src, _ := data["auth_source"].(string)
	if src == "" {
		claims.AuthSource = authSourceIntrospection
		return claims
	}
	claims.AuthSource = oidcAuthSource(src)
	if scope, ok := data["scope"].(string); ok {
		claims.Scopes = ParseScopes(scope)
	}
	return claims
}

// cacheIntrospection stores introspection result in cache
func (m *Middleware) cacheIntrospection(ctx context.Context, token string, claims *TokenClaims) error {
	if m.db == nil {
		return errors.New("auth: no database available for introspection cache")
	}

	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	// Calculate TTL (use token expiration or config TTL, whichever is sooner)
	ttl := m.cfg.Zitadel.IntrospectCacheTTL
	tokenTTL := time.Until(claims.ExpiresAt)
	if tokenTTL > 0 && tokenTTL < ttl {
		ttl = tokenTTL
	}

	expiresAt := time.Now().Add(ttl)

	raw, err := json.Marshal(claimsToCacheData(claims))
	if err != nil {
		return err
	}

	_, err = m.db.NewInsert().
		Model(&introspectionCacheEntry{
			TokenHash:         tokenHash,
			IntrospectionData: raw,
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
		Sub:        result.Sub,
		Email:      result.Email,
		Scopes:     ParseScopes(result.Scope),
		ExpiresAt:  time.Unix(result.Exp, 0),
		GivenName:  result.GivenName,
		FamilyName: result.FamilyName,
		Name:       result.Name,
		AuthSource: authSourceIntrospection,
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
		"agents:read",
		"agents:write",
	}
}

// MustGetUser retrieves the authenticated user from the Echo context.
// It is safe to call without a nil-check on routes protected by the auth middleware
// (Middleware.RequireAuth / RequireProjectID).
// Panics if called on a route where auth middleware was not applied and the user is nil.
func MustGetUser(c echo.Context) *AuthUser {
	user := GetUser(c)
	if user == nil {
		panic("auth: MustGetUser called on unauthenticated context — ensure RequireAuth/RequireProjectID middleware is applied")
	}
	return user
}

// GetProjectUUID extracts and UUID-parses the project ID from the authenticated user on the context.
func GetProjectUUID(c echo.Context) (uuid.UUID, error) {
	projectID, err := GetProjectID(c)
	if err != nil {
		return uuid.UUID{}, err
	}
	return uuid.Parse(projectID)
}
