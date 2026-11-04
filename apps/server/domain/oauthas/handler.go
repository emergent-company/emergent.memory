package oauthas

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Handler serves the OAuth 2.1 authorization-server HTTP surface.
type Handler struct {
	svc *Service
	cfg *config.Config
	log *slog.Logger

	// In-memory, per-IP token buckets. This is intentionally process-local: a
	// multi-instance deployment enforces the limit per instance, not globally.
	// It is a cheap abuse guard, not a security boundary.
	registerLimiter *ipRateLimiter
	tokenLimiter    *ipRateLimiter
}

// NewHandler creates the OAuth authorization-server handler.
func NewHandler(svc *Service, cfg *config.Config, log *slog.Logger) *Handler {
	return &Handler{
		svc: svc,
		cfg: cfg,
		log: log.With(logger.Scope("oauthas.handler")),
		// 10 registrations/minute and 60 token requests/minute per IP.
		registerLimiter: newIPRateLimiter(10, 10.0/60.0),
		tokenLimiter:    newIPRateLimiter(60, 1),
	}
}

// Metadata serves RFC 8414 authorization-server metadata.
// @Summary      OAuth authorization-server metadata
// @Description  RFC 8414 metadata for the built-in project-scoped authorization server.
// @Tags         oauth
// @Produce      json
// @Success      200 "Authorization-server metadata"
// @Router       /.well-known/oauth-authorization-server [get]
func (h *Handler) Metadata(c echo.Context) error {
	issuer := h.issuer(c)
	authorizeBase := issuer
	if h.cfg != nil && strings.TrimSpace(h.cfg.OAuthAS.AuthorizeURL) != "" {
		authorizeBase = strings.TrimRight(strings.TrimSpace(h.cfg.OAuthAS.AuthorizeURL), "/")
	}

	return c.JSON(http.StatusOK, map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                authorizeBase + "/oauth/authorize",
		"token_endpoint":                        issuer + "/api/oauth/token",
		"registration_endpoint":                 issuer + "/api/oauth/register",
		"revocation_endpoint":                   issuer + "/api/oauth/revoke",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      append([]string(nil), SupportedScopes...),
	})
}

// Register implements RFC 7591 dynamic client registration.
// @Summary      Register an OAuth client
// @Description  RFC 7591 dynamic client registration. Only public clients (token_endpoint_auth_method=none) using the authorization_code grant are accepted.
// @Tags         oauth
// @Accept       json
// @Produce      json
// @Param        request body ClientRegistrationRequest true "Client registration request"
// @Success      201 {object} ClientRegistrationResponse "Registered public client"
// @Failure      400 {object} OAuthErrorResponse "Invalid client metadata"
// @Failure      401 {object} OAuthErrorResponse "Invalid registration secret"
// @Failure      403 {object} OAuthErrorResponse "Registration disabled"
// @Failure      429 {object} OAuthErrorResponse "Rate limited"
// @Router       /api/oauth/register [post]
func (h *Handler) Register(c echo.Context) error {
	if !h.cfg.OAuthAS.RegistrationEnabled {
		return c.JSON(http.StatusForbidden, OAuthErrorResponse{
			Error:            "registration_disabled",
			ErrorDescription: "dynamic client registration is disabled",
		})
	}
	if secret := h.cfg.OAuthAS.RegistrationSecret; secret != "" {
		got := c.Request().Header.Get("X-Registration-Secret")
		if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
			return c.JSON(http.StatusUnauthorized, OAuthErrorResponse{
				Error:            "invalid_client",
				ErrorDescription: "invalid registration secret",
			})
		}
	}
	if !h.registerLimiter.allow(c.RealIP()) {
		return c.JSON(http.StatusTooManyRequests, OAuthErrorResponse{
			Error:            "rate_limited",
			ErrorDescription: "too many registration requests",
		})
	}

	var req ClientRegistrationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, OAuthErrorResponse{
			Error:            "invalid_client_metadata",
			ErrorDescription: "invalid JSON body",
		})
	}

	resp, err := h.svc.RegisterClient(c.Request().Context(), req)
	if err != nil {
		return writeOAuthError(c, err)
	}
	return c.JSON(http.StatusCreated, resp)
}

// Token implements the authorization_code token exchange (RFC 6749 §4.1.3).
// @Summary      Exchange an authorization code
// @Description  Redeems a single-use authorization code (PKCE S256) for a project-scoped emt_ access token. No refresh token is issued.
// @Tags         oauth
// @Accept       x-www-form-urlencoded
// @Produce      json
// @Param        grant_type formData string true "Must be authorization_code"
// @Param        code formData string true "Authorization code"
// @Param        redirect_uri formData string true "Redirect URI used at authorization time"
// @Param        client_id formData string true "Registered client id"
// @Param        code_verifier formData string true "PKCE code verifier"
// @Success      200 {object} TokenResponse "Issued project token"
// @Failure      400 {object} OAuthErrorResponse "Invalid or expired code"
// @Failure      429 {object} OAuthErrorResponse "Rate limited"
// @Router       /api/oauth/token [post]
func (h *Handler) Token(c echo.Context) error {
	if !h.tokenLimiter.allow(c.RealIP()) {
		return c.JSON(http.StatusTooManyRequests, OAuthErrorResponse{
			Error:            "rate_limited",
			ErrorDescription: "too many token requests",
		})
	}
	if err := c.Request().ParseForm(); err != nil {
		return writeOAuthError(c, errInvalidRequest("invalid form body"))
	}

	req := TokenRequest{
		GrantType:    c.FormValue("grant_type"),
		Code:         c.FormValue("code"),
		RedirectURI:  c.FormValue("redirect_uri"),
		ClientID:     c.FormValue("client_id"),
		CodeVerifier: c.FormValue("code_verifier"),
	}
	if req.GrantType != "authorization_code" {
		return writeOAuthError(c, newOAuthError(400, "unsupported_grant_type", "only authorization_code is supported"))
	}

	resp, err := h.svc.ExchangeCode(c.Request().Context(), req)
	if err != nil {
		return writeOAuthError(c, err)
	}

	c.Response().Header().Set("Cache-Control", "no-store")
	c.Response().Header().Set("Pragma", "no-cache")
	return c.JSON(http.StatusOK, resp)
}

// Revoke implements RFC 7009 token revocation. Unknown tokens return 200 so
// the endpoint cannot be used to probe token validity.
// @Summary      Revoke a token
// @Description  RFC 7009 revocation. Unknown tokens are reported as success so the endpoint cannot be used to probe token validity.
// @Tags         oauth
// @Accept       x-www-form-urlencoded
// @Param        token formData string true "Access token to revoke"
// @Param        client_id formData string false "Registered client id"
// @Success      200 "Token revoked (or already unknown)"
// @Failure      400 {object} OAuthErrorResponse "Invalid form body"
// @Router       /api/oauth/revoke [post]
func (h *Handler) Revoke(c echo.Context) error {
	if err := c.Request().ParseForm(); err != nil {
		return writeOAuthError(c, errInvalidRequest("invalid form body"))
	}
	err := h.svc.RevokeToken(
		c.Request().Context(),
		c.FormValue("client_id"),
		c.FormValue("token"),
	)
	if err != nil {
		return writeOAuthError(c, err)
	}
	return c.NoContent(http.StatusOK)
}

// AuthorizeContext returns the data the gateway consent page needs.
// @Summary      Get consent-page context
// @Description  Returns the validated client, fixed redirect URI, supported scopes, and projects the caller can write to.
// @Tags         oauth
// @Produce      json
// @Param        client_id query string true "Registered client id"
// @Param        redirect_uri query string true "Client redirect URI"
// @Success      200 {object} AuthorizeContextResponse "Consent-page bootstrap payload"
// @Failure      400 {object} OAuthErrorResponse "Invalid client or redirect URI"
// @Failure      401 {object} OAuthErrorResponse "Authentication required"
// @Router       /api/oauth/authorize/context [get]
// @Security     bearerAuth
func (h *Handler) AuthorizeContext(c echo.Context) error {
	user, err := requireOAuthUser(c)
	if err != nil {
		return writeOAuthError(c, err)
	}

	resp, err := h.svc.AuthorizeContext(
		c.Request().Context(),
		user.ID,
		c.QueryParam("client_id"),
		c.QueryParam("redirect_uri"),
	)
	if err != nil {
		return writeAuthorizeError(c, err)
	}
	return c.JSON(http.StatusOK, resp)
}

// Authorize records the user's consent and returns the redirect carrying the
// one-time code.
// @Summary      Approve an OAuth authorization request
// @Description  Records consent after re-checking project write membership and returns the redirect with a single-use code and the caller's state.
// @Tags         oauth
// @Accept       json
// @Produce      json
// @Param        request body AuthorizeRequest true "Consent decision"
// @Success      200 {object} AuthorizeResponse "Redirect back to the client with a code"
// @Failure      400 {object} OAuthErrorResponse "Invalid authorization request"
// @Failure      401 {object} OAuthErrorResponse "Authentication required"
// @Failure      403 {object} OAuthErrorResponse "No write access to the project"
// @Router       /api/oauth/authorize [post]
// @Security     bearerAuth
func (h *Handler) Authorize(c echo.Context) error {
	user, err := requireOAuthUser(c)
	if err != nil {
		return writeOAuthError(c, err)
	}

	var req AuthorizeRequest
	if err := c.Bind(&req); err != nil {
		return writeOAuthError(c, errInvalidRequest("invalid JSON body"))
	}

	resp, err := h.svc.Authorize(c.Request().Context(), user.ID, req)
	if err != nil {
		return writeAuthorizeError(c, err)
	}
	return c.JSON(http.StatusOK, resp)
}

// ListGrants returns the caller's OAuth grants.
// @Summary      List connected apps
// @Description  Returns the caller's OAuth grants with client/project names and revocation state.
// @Tags         oauth
// @Produce      json
// @Success      200 {array} GrantDTO "OAuth grants"
// @Failure      401 {object} OAuthErrorResponse "Authentication required"
// @Router       /api/oauth/grants [get]
// @Security     bearerAuth
func (h *Handler) ListGrants(c echo.Context) error {
	user, err := requireOAuthUser(c)
	if err != nil {
		return writeOAuthError(c, err)
	}
	grants, err := h.svc.ListGrants(c.Request().Context(), user.ID)
	if err != nil {
		return writeOAuthError(c, err)
	}
	return c.JSON(http.StatusOK, grants)
}

// RevokeGrant revokes a grant (and its API token) by id.
// @Summary      Revoke a connected app
// @Description  Revokes one of the caller's OAuth grants and the API token minted for it.
// @Tags         oauth
// @Param        id path string true "Grant ID (UUID)"
// @Success      204 "Grant revoked"
// @Failure      401 {object} OAuthErrorResponse "Authentication required"
// @Failure      404 {object} OAuthErrorResponse "Grant not found"
// @Router       /api/oauth/grants/{id} [delete]
// @Security     bearerAuth
func (h *Handler) RevokeGrant(c echo.Context) error {
	user, err := requireOAuthUser(c)
	if err != nil {
		return writeOAuthError(c, err)
	}
	if err := h.svc.RevokeGrant(c.Request().Context(), user.ID, c.Param("id")); err != nil {
		return writeOAuthError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// issuer returns the configured issuer, or derives scheme+host from the
// request (honouring the proxy's forwarding headers).
func (h *Handler) issuer(c echo.Context) string {
	if h.cfg != nil && strings.TrimSpace(h.cfg.OAuthAS.IssuerURL) != "" {
		return strings.TrimRight(strings.TrimSpace(h.cfg.OAuthAS.IssuerURL), "/")
	}

	scheme := "https"
	if c.Request().TLS == nil {
		scheme = "http"
	}
	if xfp := firstForwarded(c.Request().Header.Get("X-Forwarded-Proto")); xfp != "" {
		scheme = xfp
	}
	host := c.Request().Host
	if xfh := firstForwarded(c.Request().Header.Get("X-Forwarded-Host")); xfh != "" {
		host = xfh
	}
	return scheme + "://" + host
}

func firstForwarded(raw string) string {
	if raw == "" {
		return ""
	}
	if i := strings.IndexByte(raw, ','); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(raw)
}

// writeOAuthError renders an OAuthError shape, falling back to a generic
// server_error for unexpected failures.
// requireOAuthUser returns the session user for the consent/grant surface. The
// routes are auth-gated; RequireUser additionally fails closed if a handler is
// ever mounted without the middleware, matching the rest of the service layer.
func requireOAuthUser(c echo.Context) (*auth.AuthUser, error) {
	user, err := auth.RequireUser(c.Request().Context())
	if err != nil {
		return nil, newOAuthError(http.StatusUnauthorized, "invalid_request", "authentication required")
	}
	return user, nil
}

func writeOAuthError(c echo.Context, err error) error {
	var oe *OAuthError
	if errors.As(err, &oe) {
		return c.JSON(oe.Status, OAuthErrorResponse{
			Error:            oe.Code,
			ErrorDescription: oe.Description,
		})
	}
	return c.JSON(http.StatusInternalServerError, OAuthErrorResponse{
		Error:            "server_error",
		ErrorDescription: "internal error",
	})
}

// writeAuthorizeError keeps authorize-surface failures at 400/403 even when the
// underlying code is invalid_client (which is a 401 at the token endpoint).
func writeAuthorizeError(c echo.Context, err error) error {
	var oe *OAuthError
	if errors.As(err, &oe) {
		status := oe.Status
		if status == http.StatusUnauthorized {
			status = http.StatusBadRequest
		}
		return c.JSON(status, OAuthErrorResponse{
			Error:            oe.Code,
			ErrorDescription: oe.Description,
		})
	}
	return writeOAuthError(c, err)
}

// ipRateLimiter is a small per-key token bucket.
type ipRateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*rateBucket
	rate     float64 // tokens added per second
	capacity float64
	lastGC   time.Time
}

type rateBucket struct {
	tokens float64
	last   time.Time
}

func newIPRateLimiter(burst int, ratePerSecond float64) *ipRateLimiter {
	return &ipRateLimiter{
		buckets:  make(map[string]*rateBucket),
		rate:     ratePerSecond,
		capacity: float64(burst),
		lastGC:   time.Now(),
	}
}

func (l *ipRateLimiter) allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Bound the map: drop buckets untouched for 10 minutes.
	if now.Sub(l.lastGC) > 10*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.last) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &rateBucket{tokens: l.capacity, last: now}
		l.buckets[key] = b
	}

	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * l.rate
		if b.tokens > l.capacity {
			b.tokens = l.capacity
		}
		b.last = now
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
