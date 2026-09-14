package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// validAPIKey reports whether the request carries a valid client API key.
// When TOKEN_API_KEY is empty (dev), every request is valid (routes open) —
// matching the memory standalone convention and keeping the browser UI usable
// without a device key. Otherwise it accepts the admin key or a registered
// per-device key (stored in memory under the ios_device_keys category).
// This "open when TOKEN_API_KEY is unset" path is dev-mode only and is never
// reached in session mode (session mode uses validAPIKeyValue).
func (s *Server) validAPIKey(c echo.Context) bool {
	if s.cfg.ClientAPIKey == "" {
		return true
	}
	return s.validAPIKeyValue(c)
}

// validAPIKeyValue reports whether the presented X-API-Key is the admin key or
// a registered device key. Unlike validAPIKey it has no "open when unset"
// semantics — used for the session-mode key fallback where an absent key must
// deny.
func (s *Server) validAPIKeyValue(c echo.Context) bool {
	key := c.Request().Header.Get("X-API-Key")
	if key == "" {
		return false
	}
	if s.cfg.ClientAPIKey != "" && subtle.ConstantTimeCompare([]byte(key), []byte(s.cfg.ClientAPIKey)) == 1 {
		return true
	}
	return s.deviceKeyValid(c.Request().Context(), key)
}

// requireClientKey gates client API routes (the programmatic / iOS path).
func (s *Server) requireClientKey(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if !s.validAPIKey(c) {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
		return next(c)
	}
}

// sessionCookie helpers ---

// currentSession reads + verifies the session cookie. Errors mean the request
// is unauthenticated (missing cookie, tampered, expired, or no secret set).
func (s *Server) currentSession(c echo.Context) (*sessionClaims, error) {
	if s.cfg.SessionSecret == "" {
		return nil, errors.New("session: no SessionSecret configured")
	}
	ck, err := c.Cookie(sessionCookieName)
	if err != nil {
		return nil, err // http.ErrNoCookie when absent
	}
	return verifySession(s.cfg.SessionSecret, ck.Value, time.Now())
}

// setSessionCookie signs claims and writes the session cookie. Expiry is taken
// from claims.ExpiresAt so re-issued cookies (project switch) keep the
// original access-token lifetime.
// cookieSecure reports whether auth/session cookies should carry the Secure
// flag. Secure cookies are required over HTTPS but are silently dropped by
// browsers over plain HTTP (e.g. a tailnet-only dev host served without TLS).
// An explicit PUBLIC_BASE_URL pins the decision (http:// disables Secure, any
// other scheme enables it). When unset, derive from the request scheme so a
// proxy terminating TLS (X-Forwarded-Proto) still gets Secure cookies while a
// plain-HTTP dev host does not.
func (s *Server) cookieSecure(c echo.Context) bool {
	if s.cfg.AuthMode != "session" {
		return false
	}
	if b := strings.TrimSpace(s.cfg.PublicBaseURL); b != "" {
		return !strings.HasPrefix(b, "http://")
	}
	return c.Scheme() == "https"
}

// maxSessionCookieValueBytes bounds the signed session cookie value below the
// browser's ~4096-byte per-cookie limit (name + value + attributes). A value
// over the limit is silently dropped by the browser, which would log the user
// out entirely, so the id_token is shed before that can happen.
const maxSessionCookieValueBytes = 3800

func (s *Server) setSessionCookie(c echo.Context, claims sessionClaims) {
	now := time.Now()
	// The session cookie already carries the access + refresh tokens; adding
	// the (JWT-sized) id_token on top can push the signed value past the
	// browser limit. id_token_hint is only a best-effort hint for
	// RP-initiated logout, so shed it (logout falls back to client_id) rather
	// than risk the browser dropping the whole session cookie.
	if claims.IDToken != "" {
		if v, err := issueSession(s.cfg.SessionSecret, claims, now); err == nil && len(v) > maxSessionCookieValueBytes {
			claims.IDToken = ""
		}
	}
	val, err := issueSession(s.cfg.SessionSecret, claims, now)
	if err != nil {
		return // only possible on marshal failure; nothing sane to do
	}
	// Browser retention is decoupled from the short-lived access token: use a
	// long fixed MaxAge (SessionMaxAge) so the refresh token survives in the
	// cookie and silently renews the access token. Server-side, verifySession
	// still enforces the access-token expiry from the signed claims, and
	// ensureFreshSession recovers via the refresh token. Zero SessionMaxAge
	// falls back to the access-token lifetime (legacy/test behavior).
	maxAge := int(claims.ExpiresAt - now.Unix())
	if s.cfg.SessionMaxAge > 0 {
		maxAge = int(s.cfg.SessionMaxAge.Seconds())
	}
	if maxAge < 0 {
		maxAge = 0
	}
	c.SetCookie(&http.Cookie{
		Name:     sessionCookieName,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure(c),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

// clearSessionCookie deletes the session cookie.
func (s *Server) clearSessionCookie(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure(c),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
}

// clearOAuthStateCookie deletes the one-time OAuth flow cookie.
func (s *Server) clearOAuthStateCookie(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     oauthStateCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure(c),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
}

// --- browser install-id cookie (multi-account registry key, design D7) ---

// installCookieName is the signed browser-install cookie that keys the
// in-memory account registry. It is stable per browser and never cleared on
// sign-out, so a re-login still sees the other cached accounts.
const installCookieName = "memory_install"

// installIDClaims is the signed payload of the install-id cookie.
type installIDClaims struct {
	ID string `json:"id"`
}

// issueInstallID signs an install id into a cookie value.
func issueInstallID(secret, id string) (string, error) {
	payload, err := json.Marshal(installIDClaims{ID: id})
	if err != nil {
		return "", err
	}
	return signCookieValue(secret, payload), nil
}

// verifyInstallID verifies the install-id cookie's signature and returns the id.
func verifyInstallID(secret, value string) (string, error) {
	payload, err := verifyCookieValue(secret, value)
	if err != nil {
		return "", err
	}
	var claims installIDClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("install id: %w", err)
	}
	if claims.ID == "" {
		return "", errors.New("install id: empty")
	}
	return claims.ID, nil
}

// readInstallID returns the browser's install id, or ok=false when absent,
// unset-secret, or invalid.
func (s *Server) readInstallID(c echo.Context) (string, bool) {
	if s.cfg.SessionSecret == "" {
		return "", false
	}
	ck, err := c.Cookie(installCookieName)
	if err != nil {
		return "", false
	}
	id, err := verifyInstallID(s.cfg.SessionSecret, ck.Value)
	if err != nil {
		return "", false
	}
	return id, true
}

// --- durable recent-projects cookie (per-account MRU project list) ---

// recentProjectsCookieName is the signed browser cookie tracking the most
// recently activated projects per account (keyed by Sub). Like the install
// cookie it is durable: never cleared on sign-out and issued with a 1-year
// MaxAge, so a re-login still sees the account's recent project list.
const recentProjectsCookieName = "memory_recent_projects"

// recentProjectsClaims is the signed payload of the recent-projects cookie.
// Recent is keyed by account Sub; each value is an ordered list of project ids
// (most-recent first, capped at 8 entries).
type recentProjectsClaims struct {
	Recent map[string][]string `json:"recent"`
}

// issueRecentProjects signs the recent-projects claims into a cookie value.
func issueRecentProjects(secret string, claims recentProjectsClaims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	return signCookieValue(secret, payload), nil
}

// verifyRecentProjects verifies the recent-projects cookie's signature and
// returns the claims.
func verifyRecentProjects(secret, value string) (recentProjectsClaims, error) {
	payload, err := verifyCookieValue(secret, value)
	if err != nil {
		return recentProjectsClaims{}, err
	}
	var claims recentProjectsClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return recentProjectsClaims{}, fmt.Errorf("recent projects: %w", err)
	}
	return claims, nil
}

// readRecentProjects returns the recent-projects claims, or ok=false when the
// cookie is absent, the secret is unset, or the value is invalid.
func (s *Server) readRecentProjects(c echo.Context) (recentProjectsClaims, bool) {
	if s.cfg.SessionSecret == "" {
		return recentProjectsClaims{}, false
	}
	ck, err := c.Cookie(recentProjectsCookieName)
	if err != nil {
		return recentProjectsClaims{}, false
	}
	claims, err := verifyRecentProjects(s.cfg.SessionSecret, ck.Value)
	if err != nil {
		return recentProjectsClaims{}, false
	}
	return claims, true
}

// recordRecentProject prepends projectID to the account's recent-project list
// (deduping first) and re-issues the durable cookie. Lists are capped at 8
// entries. No-op when sub or projectID is empty (e.g. dev / API-key callers)
// or the secret is unset. The cookie is never cleared on sign-out.
func (s *Server) recordRecentProject(c echo.Context, sub, projectID string) {
	if sub == "" || projectID == "" || s.cfg.SessionSecret == "" {
		return
	}
	claims, ok := s.readRecentProjects(c)
	if !ok || claims.Recent == nil {
		claims = recentProjectsClaims{Recent: map[string][]string{}}
	}
	recents := claims.Recent[sub]
	cleaned := recents[:0] // dedupe: drop any prior occurrence of projectID
	for _, id := range recents {
		if id != projectID {
			cleaned = append(cleaned, id)
		}
	}
	recents = append([]string{projectID}, cleaned...)
	if len(recents) > 8 {
		recents = recents[:8]
	}
	claims.Recent[sub] = recents
	val, err := issueRecentProjects(s.cfg.SessionSecret, claims)
	if err != nil {
		return
	}
	c.SetCookie(&http.Cookie{
		Name:     recentProjectsCookieName,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure(c),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   31536000, // 1 year — durable like the install cookie
	})
}

// ensureInstallID returns the browser's install id, issuing a new one when
// absent.
func (s *Server) ensureInstallID(c echo.Context) (string, error) {
	if id, ok := s.readInstallID(c); ok {
		return id, nil
	}
	if s.cfg.SessionSecret == "" {
		return "", errors.New("install id: no SessionSecret configured")
	}
	raw, err := randomBase64URL(16)
	if err != nil {
		return "", err
	}
	val, err := issueInstallID(s.cfg.SessionSecret, raw)
	if err != nil {
		return "", err
	}
	c.SetCookie(&http.Cookie{
		Name:     installCookieName,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure(c),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   31536000, // 1 year
	})
	return raw, nil
}

// attachSession threads verified session credentials through the request
// context so the shared MemoryClient resolves per-request token/project/org
// (design D3/D6).
func (s *Server) attachSession(c echo.Context, claims *sessionClaims) {
	sc := &sessionContext{
		Token:             claims.AccessToken,
		RefreshToken:      claims.RefreshToken,
		ProjectID:         claims.ActiveProjectID,
		OrgID:             claims.OrgID,
		Name:              claims.Name,
		Email:             claims.Email,
		Picture:           claims.Picture,
		AvatarOverrideURL: claims.AvatarOverrideURL,
		Sub:               claims.Sub,
		ExpiresAt:         claims.ExpiresAt,
	}
	ctx := withSessionContext(c.Request().Context(), sc)
	c.SetRequest(c.Request().WithContext(ctx))
}

// --- token refresh (silent session renewal) ---

// sessionPayload reads the session cookie and verifies its SIGNATURE only
// (no expiry / claim-presence checks). It is the lenient parse used to
// recover a session whose access token has already expired.
func (s *Server) sessionPayload(c echo.Context) (*sessionClaims, error) {
	if s.cfg.SessionSecret == "" {
		return nil, errors.New("session: no SessionSecret configured")
	}
	ck, err := c.Cookie(sessionCookieName)
	if err != nil {
		return nil, err // http.ErrNoCookie when absent
	}
	return verifySessionSignature(s.cfg.SessionSecret, ck.Value)
}

// sessionRefreshToken recovers the refresh token from the session cookie
// without requiring the session to be unexpired (signature-only check) — the
// entry point for refreshing an expired session.
func (s *Server) sessionRefreshToken(c echo.Context) (string, error) {
	claims, err := s.sessionPayload(c)
	if err != nil {
		return "", err
	}
	if claims.RefreshToken == "" {
		return "", errors.New("session: cookie carries no refresh token")
	}
	return claims.RefreshToken, nil
}

// refreshSessionClaims refreshes the session via the single-flight coordinator
// and re-issues the cookie on this response.
func (s *Server) refreshSessionClaims(c echo.Context, old *sessionClaims) (*sessionClaims, error) {
	claims, err := s.refreshOnce(old)
	if err != nil {
		return nil, err
	}
	s.setSessionCookie(c, *claims)
	return claims, nil
}

// refreshOnce exchanges the refresh token for a fresh token set exactly once
// per token, sharing the result across concurrent requests. Rotating refresh
// tokens may be redeemed only once, so overlapping grace-window requests must
// not each call the token endpoint.
func (s *Server) refreshOnce(old *sessionClaims) (*sessionClaims, error) {
	v, err, _ := s.refreshGroup.Do(old.RefreshToken, func() (any, error) {
		// Detach from any single request's context: one caller disconnecting
		// must not cancel a refresh other waiters depend on.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tok, err := s.refreshSessionTokens(ctx, old.RefreshToken)
		if err != nil {
			return nil, err
		}
		if tok.AccessToken == "" {
			return nil, errors.New("session: token refresh returned no access_token")
		}
		now := time.Now()
		expiresIn := tok.ExpiresIn
		if expiresIn <= 0 {
			expiresIn = 3600
		}
		refreshToken := tok.RefreshToken
		if refreshToken == "" {
			refreshToken = old.RefreshToken // no rotation → keep the working one
		}
		idToken := tok.IDToken
		if idToken == "" {
			idToken = old.IDToken // refresh omitted one → keep the prior id_token_hint
		}
		claims := sessionClaims{
			AccessToken:       tok.AccessToken,
			RefreshToken:      refreshToken,
			IDToken:           idToken,
			ActiveProjectID:   old.ActiveProjectID,
			OrgID:             old.OrgID,
			Name:              old.Name,
			Email:             old.Email,
			Picture:           old.Picture,
			AvatarOverrideURL: old.AvatarOverrideURL,
			Sub:               old.Sub,
			ExpiresAt:         now.Add(time.Duration(expiresIn) * time.Second).Unix(),
		}
		return &claims, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*sessionClaims), nil
}

// ensureFreshSession returns the request's session claims, silently refreshing
// the access token when it is expired or within the 5-minute grace window and
// a refresh token is available. The cookie is re-issued on every refresh.
// When no live session can be established (missing/tampered cookie, or the
// refresh itself fails), it returns the original session error — the caller
// treats that as logged out.
func (s *Server) ensureFreshSession(c echo.Context) (*sessionClaims, error) {
	claims, err := s.currentSession(c)
	if err == nil {
		// Live session: only refresh inside the grace window so the access
		// token never dies mid-request (e.g. on a long-lived page).
		if claims.RefreshToken != "" && claims.ExpiresAt-time.Now().Unix() < 300 {
			return s.refreshSessionClaims(c, claims)
		}
		return claims, nil
	}
	// Not a live session: recover when the cookie is authentic (valid
	// signature) and still carries a refresh token.
	old, perr := s.sessionPayload(c)
	if perr != nil {
		return nil, err // missing/tampered — logged out
	}
	if old.RefreshToken == "" {
		return nil, err // authentic but refreshless — logged out
	}
	return s.refreshSessionClaims(c, old)
}

// Middlewares ---

// canonicalHostRedirect 302s browser requests arriving on a non-canonical Host
// to the PUBLIC_BASE_URL host (preserving path + query). Host-only cookies (the
// OAuth state cookie and the session cookie) are scoped to the request Host, so
// a mismatch between it and ZITADEL_REDIRECT_URI/PUBLIC_BASE_URL silently drops
// them — e.g. signing in via the short Tailscale name "alfred-dev" while the
// redirect_uri is pinned to "alfred-dev.tail0358fa.ts.net" ("missing oauth
// state"). No-op when PUBLIC_BASE_URL is unset. Skips programmatic/asset paths
// (iOS setup, internal worker, GitHub webhooks, health, static assets) so
// non-browser clients are never redirected.
func (s *Server) canonicalHostRedirect(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if s.cfg.PublicBaseURL == "" {
			return next(c)
		}
		u, err := url.Parse(s.cfg.PublicBaseURL)
		if err != nil || u.Host == "" {
			return next(c)
		}
		if c.Request().Host == u.Host {
			return next(c)
		}
		p := c.Request().URL.Path
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/internal/") ||
			strings.HasPrefix(p, "/webhooks/") ||
			strings.HasPrefix(p, "/static/") || strings.HasPrefix(p, "/assets/") {
			return next(c)
		}
		return c.Redirect(http.StatusFound, u.Scheme+"://"+u.Host+c.Request().URL.RequestURI())
	}
}

// requireSession gates UI (page) routes. In dev mode (AuthMode != "session")
// it is a no-op — the browser UI is open. In session mode it requires a valid
// session (refreshing the access token when near expiry or expired-but-
// recoverable), attaches the session credentials to the request context, and
// 302s to /auth/login when no live session can be established.
func (s *Server) requireSession(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if s.cfg.AuthMode != "session" {
			return next(c)
		}
		claims, err := s.ensureFreshSession(c)
		if err != nil {
			if !errors.Is(err, http.ErrNoCookie) {
				s.clearSessionCookie(c) // drop tampered/expired/unrecoverable cookies
			}
			return c.Redirect(http.StatusFound, "/auth/login")
		}
		s.attachSession(c, claims)
		return next(c)
	}
}

// requireSessionOrKey gates the /api routes. In dev mode it behaves exactly
// like requireClientKey (the programmatic path). In session mode it accepts a
// valid session (refreshed when needed, credentials attached) OR a valid
// X-API-Key, else 401.
func (s *Server) requireSessionOrKey(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if s.cfg.AuthMode != "session" {
			return s.requireClientKey(next)(c)
		}
		if claims, err := s.ensureFreshSession(c); err == nil {
			s.attachSession(c, claims)
			return next(c)
		}
		if !s.validAPIKeyValue(c) {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
		return next(c)
	}
}

// projectScopePath reports whether a request path targets a project-scoped
// page (needs an active project). Org, account, wizard, and the landing route
// are excluded.
func projectScopePath(p string) bool {
	switch {
	case p == "/", p == "":
		return false // landing redirect (uiRoot) resolves its own context
	case p == "/orgs", strings.HasPrefix(p, "/orgs/"):
		return false
	case p == "/members", strings.HasPrefix(p, "/members/"):
		return false
	case p == "/invites", strings.HasPrefix(p, "/invites/"):
		return false
	case p == "/profile", strings.HasPrefix(p, "/profile/"), strings.HasPrefix(p, "/partial/invite-user-search"):
		return false
	default:
		return true
	}
}

// resolveDefaultProject auto-selects the first available project for a
// project-scoped page when the session has none (e.g. a new user whose project
// exists but was never activated). It re-issues the session cookie and mirrors
// the project into the request context so the current render is already
// scoped. When there are no projects, it redirects to the org list so the user
// can create one. No-op in dev/API-key mode (no session), on org/account/wizard
// pages, and when a project is already active.
func (s *Server) resolveDefaultProject(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if s.cfg.AuthMode != "session" ||
			c.Request().Method != http.MethodGet ||
			!projectScopePath(c.Request().URL.Path) {
			return next(c)
		}
		sc, ok := sessionContextFrom(c.Request().Context())
		if !ok || sc.ProjectID != "" {
			return next(c)
		}
		projects, err := s.memory.ListProjects(c.Request().Context())
		if err != nil || len(projects) == 0 {
			// No project to auto-select: send to the org list to create one.
			return c.Redirect(http.StatusFound, "/orgs")
		}
		s.activateProjectInSession(c, projects[0].ID, projects[0].OrgID)
		// activateProjectInSession only re-issues the cookie; mirror the choice
		// into the request context so this render is already project-scoped.
		sc.ProjectID = projects[0].ID
		sc.OrgID = projects[0].OrgID
		return next(c)
	}
}

// publicAuthPath reports whether a request path needs no auth in session mode.
func publicAuthPath(p string) bool {
	if strings.HasPrefix(p, "/static/") || strings.HasPrefix(p, "/assets/") {
		return true
	}
	// Internal worker endpoints carry their own X-Worker-Key trust boundary.
	if strings.HasPrefix(p, "/internal/") {
		return true
	}
	switch p {
	case "/auth/login", "/auth/start", "/auth/add", "/auth/callback", "/auth/logout",
		"/auth/switch", "/api/setup", "/api/health",
		// GitHub webhook ingress authenticates via HMAC signature, not session.
		"/webhooks/github":
		return true
	}
	return false
}

// authDispatch is the single web-auth gate registered for every route.
//
// Dev mode (AuthMode != "session") preserves today's exact behavior: /api/*
// except /api/setup is gated by requireClientKey (open when no TOKEN_API_KEY
// is configured), and everything else is open.
//
// Session mode enforces the change's auth matrix: the publicAuthPath routes
// stay open, the rest of /api/* requires a valid session OR a valid X-API-Key,
// and every other route requires a valid session (302 to /auth/login).
func (s *Server) authDispatch(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		p := c.Request().URL.Path
		if s.cfg.AuthMode != "session" {
			if strings.HasPrefix(p, "/api/") && p != "/api/setup" {
				return s.requireClientKey(next)(c)
			}
			return next(c)
		}
		if publicAuthPath(p) {
			return next(c)
		}
		if strings.HasPrefix(p, "/api/") {
			return s.requireSessionOrKey(next)(c)
		}
		return s.requireSession(s.resolveDefaultProject(next))(c)
	}
}
