package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// oidcDiscovery is the subset of the Zitadel discovery document the gateway
// needs (GET {issuer}/.well-known/openid-configuration).
type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	EndSessionEndpoint    string `json:"end_session_endpoint"`
	JwksURI               string `json:"jwks_uri"`
}

var (
	discoveryMu   sync.Mutex
	discoveryDocs = map[string]*oidcDiscovery{} // keyed by issuer URL
	httpClient    = &http.Client{Timeout: 15 * time.Second}

	verifierMu       sync.Mutex
	idTokenVerifiers = map[string]*oidc.IDTokenVerifier{} // keyed by issuer URL
)

// discoverOIDC fetches (and caches per issuer) the Zitadel discovery document.
func (s *Server) discoverOIDC(ctx context.Context) (*oidcDiscovery, error) {
	issuer := strings.TrimSuffix(s.cfg.ZitadelIssuer, "/")
	if issuer == "" {
		return nil, errors.New("ZITADEL_ISSUER is not configured")
	}
	discoveryMu.Lock()
	if d, ok := discoveryDocs[issuer]; ok {
		discoveryMu.Unlock()
		return d, nil
	}
	discoveryMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("oidc discovery: %d: %s", resp.StatusCode, string(raw))
	}
	var d oidcDiscovery
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("oidc discovery: parse: %w", err)
	}
	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" {
		return nil, fmt.Errorf("oidc discovery: missing authorization/token endpoint")
	}
	discoveryMu.Lock()
	discoveryDocs[issuer] = &d
	discoveryMu.Unlock()
	return &d, nil
}

// verifyIDToken verifies an ID token's signature (via the provider JWKS) and
// its issuer/audience/expiry claims using go-oidc. The nonce is NOT checked by
// go-oidc and must be validated separately by the caller.
func (s *Server) verifyIDToken(ctx context.Context, doc *oidcDiscovery, rawIDToken string) (*oidc.IDToken, error) {
	if rawIDToken == "" {
		return nil, errors.New("id token: missing")
	}
	if doc == nil || doc.JwksURI == "" {
		return nil, errors.New("oidc discovery: missing jwks_uri")
	}
	issuer := strings.TrimSuffix(s.cfg.ZitadelIssuer, "/")
	verifierMu.Lock()
	v, ok := idTokenVerifiers[issuer]
	verifierMu.Unlock()
	if !ok {
		// The key set fetches JWKS lazily and refreshes on rotation. A
		// long-lived context keeps verification independent of the request.
		keySet := oidc.NewRemoteKeySet(context.Background(), doc.JwksURI)
		v = oidc.NewVerifier(issuer, keySet, &oidc.Config{ClientID: s.cfg.ZitadelClientID})
		verifierMu.Lock()
		idTokenVerifiers[issuer] = v
		verifierMu.Unlock()
	}
	verified, err := v.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("id token verify: %w", err)
	}
	return verified, nil
}

// oauthStateClaims is the short-lived flow-state carried in the memory_oauth
// cookie between the /auth/login redirect and the /auth/callback round trip.
type oauthStateClaims struct {
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
	Nonce        string `json:"nonce"`
	Prompt       string `json:"prompt,omitempty"` // "select_account" marks the add-account flow
	ExpiresAt    int64  `json:"exp"`              // unix seconds
}

// oauthStateTTL bounds how long a login flow's state cookie stays valid.
const oauthStateTTL = 10 * time.Minute

// issueOAuthState signs the OAuth flow state into a cookie value.
func issueOAuthState(secret, state, codeVerifier, nonce string, now time.Time) (string, error) {
	return issueOAuthStatePrompt(secret, state, codeVerifier, nonce, "", now)
}

// issueOAuthStatePrompt signs the OAuth flow state into a cookie value,
// recording the requested prompt so the callback can tell a plain sign-in from
// an "add another account" flow.
func issueOAuthStatePrompt(secret, state, codeVerifier, nonce, prompt string, now time.Time) (string, error) {
	payload, err := json.Marshal(oauthStateClaims{
		State:        state,
		CodeVerifier: codeVerifier,
		Nonce:        nonce,
		Prompt:       prompt,
		ExpiresAt:    now.Add(oauthStateTTL).Unix(),
	})
	if err != nil {
		return "", err
	}
	return signCookieValue(secret, payload), nil
}

// verifyOAuthState validates the OAuth flow cookie and returns its claims.
func verifyOAuthState(secret, value string, now time.Time) (*oauthStateClaims, error) {
	payload, err := verifyCookieValue(secret, value)
	if err != nil {
		return nil, err
	}
	var claims oauthStateClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("oauth state: %w", err)
	}
	if claims.State == "" {
		return nil, errors.New("oauth state: empty state")
	}
	if claims.ExpiresAt <= now.Unix() {
		return nil, errors.New("oauth state: expired")
	}
	return &claims, nil
}

// randomBase64URL returns n random bytes as a base64url string (used for the
// PKCE code_verifier; 32 bytes → 43 chars, within RFC 7636's 43–128 range).
func randomBase64URL(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// authLogin renders the standalone sign-in page (GET /auth/login). Its "Sign
// in" action links to authStart, which runs the actual OIDC redirect. In dev
// mode there is no session to protect, so it redirects back to the open UI.
func (s *Server) authLogin(c echo.Context) error {
	if s.cfg.AuthMode != "session" {
		return c.Redirect(http.StatusFound, "/")
	}
	render.RenderPage(c.Response().Writer, c.Request(), loginPage(s.cfg.SentryDSN, s.cfg.SentryEnvironment, s.cfg.SentryTracesSampleRate, s.cfg.SentryReplaySessionSampleRate, s.cfg.SentryReplayOnErrorSampleRate))
	return nil
}

// authStart begins the OIDC authorization-code + PKCE flow: it stores a
// short-lived signed {state, code_verifier, nonce} cookie, then 302s to the
// Zitadel authorization endpoint. In dev mode there is no session to protect,
// so it redirects back to the open UI. The login page's "Sign in" action
// points here (see authLogin).
func (s *Server) authStart(c echo.Context) error {
	return s.authStartWithPrompt(c, "")
}

// authAdd starts the "add another account" flow: the same OIDC redirect as
// authStart but with prompt=select_account, so Zitadel offers an account
// picker instead of silently reusing the existing SSO session (design D3).
func (s *Server) authAdd(c echo.Context) error {
	return s.authStartWithPrompt(c, "select_account")
}

func (s *Server) authStartWithPrompt(c echo.Context, prompt string) error {
	if s.cfg.AuthMode != "session" {
		return c.Redirect(http.StatusFound, "/")
	}
	if s.cfg.ZitadelClientID == "" || s.cfg.ZitadelRedirectURI == "" || s.cfg.SessionSecret == "" {
		return c.HTML(http.StatusInternalServerError, "<h1>Sign-in is not configured</h1>")
	}
	doc, err := s.discoverOIDC(c.Request().Context())
	if err != nil {
		return oidcErrorPage(c, http.StatusBadGateway, err.Error())
	}

	state := randomHex(32)
	codeVerifier, err := randomBase64URL(32)
	if err != nil {
		return oidcErrorPage(c, http.StatusInternalServerError, err.Error())
	}
	nonce := randomHex(32)

	val, err := issueOAuthStatePrompt(s.cfg.SessionSecret, state, codeVerifier, nonce, prompt, time.Now())
	if err != nil {
		return oidcErrorPage(c, http.StatusInternalServerError, err.Error())
	}
	c.SetCookie(&http.Cookie{
		Name:     oauthStateCookieName,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure(c),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(oauthStateTTL.Seconds()),
	})

	sum := sha256.Sum256([]byte(codeVerifier))
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", s.cfg.ZitadelClientID)
	q.Set("redirect_uri", s.cfg.ZitadelRedirectURI)
	q.Set("scope", "openid profile email offline_access")
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("nonce", nonce)
	if prompt != "" {
		q.Set("prompt", prompt)
	}

	return c.Redirect(http.StatusFound, doc.AuthorizationEndpoint+"?"+q.Encode())
}

// oidcTokenResponse is the subset of the token-endpoint response the gateway
// needs.
type oidcTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// decodeIDTokenClaims extracts the user's name/email/picture/sub from a Zitadel
// ID token's payload segment (a JWT). Signature is NOT verified here — the token
// was received directly from Zitadel over TLS. Name falls back to
// preferred_username when absent. sub is the stable account identity used for
// multi-account de-duplication. Decode/JSON errors are ignored: identity is
// best-effort and never blocks sign-in, so failures return empty strings.
func decodeIDTokenClaims(idToken string) (name, email, picture, sub string) {
	if idToken == "" {
		return "", "", "", ""
	}
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 || parts[1] == "" {
		return "", "", "", ""
	}
	payload, err := decodeBase64URLSegment(parts[1])
	if err != nil {
		return "", "", "", ""
	}
	var claims struct {
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
		Picture           string `json:"picture"`
		Sub               string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", "", "", ""
	}
	name = claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}
	return name, claims.Email, claims.Picture, claims.Sub
}

// fetchUserInfo fetches the signed-in user's name/email/picture from the OIDC
// userinfo endpoint (Authorization: Bearer <access token>). Userinfo is the
// source of truth for identity claims the ID token may omit (Zitadel's tokens
// carry only iss/sub/aud/...). Failures are tolerated exactly like
// decodeIDTokenClaims: identity is best-effort and never blocks sign-in, so a
// missing endpoint, HTTP error, or decode error returns empty strings.
func fetchUserInfo(ctx context.Context, userinfoEndpoint, accessToken string) (name, email, picture string) {
	if userinfoEndpoint == "" || accessToken == "" {
		return "", "", ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userinfoEndpoint, nil)
	if err != nil {
		return "", "", ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", ""
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode >= 400 {
		return "", "", ""
	}
	var claims struct {
		Name    string `json:"name"`
		Email   string `json:"email"`
		Picture string `json:"picture"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return "", "", ""
	}
	return claims.Name, claims.Email, claims.Picture
}

// decodeBase64URLSegment decodes a JWT segment, tolerating both padded and
// unpadded base64url.
func decodeBase64URLSegment(seg string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(seg); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(seg)
}

// authCallback handles the Zitadel redirect: it validates the state cookie,
// exchanges the code at the token endpoint, and establishes the session.
func (s *Server) authCallback(c echo.Context) error {
	if s.cfg.AuthMode != "session" {
		return c.Redirect(http.StatusFound, "/")
	}

	// Provider-side error (e.g. the user declined consent).
	if c.QueryParam("error") != "" {
		s.clearOAuthStateCookie(c)
		return oidcErrorPage(c, http.StatusBadRequest,
			"authorization failed: "+c.QueryParam("error")+" "+c.QueryParam("error_description"))
	}

	state := c.QueryParam("state")
	code := c.QueryParam("code")
	flowCookie, err := c.Cookie(oauthStateCookieName)
	if err != nil {
		s.clearOAuthStateCookie(c)
		return oidcErrorPage(c, http.StatusBadRequest, "missing oauth state")
	}
	flow, err := verifyOAuthState(s.cfg.SessionSecret, flowCookie.Value, time.Now())
	if err != nil || flow.State != state || state == "" {
		s.clearOAuthStateCookie(c)
		return oidcErrorPage(c, http.StatusBadRequest, "oauth state mismatch")
	}
	if code == "" {
		s.clearOAuthStateCookie(c)
		return oidcErrorPage(c, http.StatusBadRequest, "missing authorization code")
	}

	doc, err := s.discoverOIDC(c.Request().Context())
	if err != nil {
		return oidcErrorPage(c, http.StatusBadGateway, err.Error())
	}

	tok, err := s.exchangeCode(c.Request().Context(), doc.TokenEndpoint, code, flow.CodeVerifier)
	if err != nil {
		return oidcErrorPage(c, http.StatusBadGateway, err.Error())
	}
	if tok.AccessToken == "" {
		return oidcErrorPage(c, http.StatusBadGateway, "token endpoint returned no access_token")
	}
	verified, err := s.verifyIDToken(c.Request().Context(), doc, tok.IDToken)
	if err != nil {
		s.clearOAuthStateCookie(c)
		return oidcErrorPage(c, http.StatusBadRequest, "id token validation failed: "+err.Error())
	}
	if flow.Nonce != "" && verified.Nonce != flow.Nonce {
		s.clearOAuthStateCookie(c)
		return oidcErrorPage(c, http.StatusBadRequest, "id token nonce validation failed")
	}

	now := time.Now()
	expiresIn := tok.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600 // spec default when expires_in is missing/0
	}
	name, email, picture, sub := decodeIDTokenClaims(tok.IDToken)
	// The userinfo response is the authoritative identity source (Zitadel's
	// access/ID tokens may carry only iss/sub/aud/...); a userinfo value wins
	// over the ID token's, and an unhelpful userinfo response falls back to
	// the ID token decode above.
	if uName, uEmail, uPicture := fetchUserInfo(c.Request().Context(), doc.UserinfoEndpoint, tok.AccessToken); uName != "" || uEmail != "" || uPicture != "" {
		if uName != "" {
			name = uName
		}
		if uEmail != "" {
			email = uEmail
		}
		if uPicture != "" {
			picture = uPicture
		}
	}
	claims := sessionClaims{
		AccessToken:     tok.AccessToken,
		RefreshToken:    tok.RefreshToken,
		IDToken:         tok.IDToken,
		ActiveProjectID: "",
		Name:            name,
		Email:           email,
		Picture:         picture,
		Sub:             sub,
		ExpiresAt:       now.Add(time.Duration(expiresIn) * time.Second).Unix(),
	}
	// Resolve the avatar override (and any missing display name) from the
	// Memory profile, which is keyed by the bearer token just exchanged. A
	// non-empty AvatarUrl is the user's uploaded profile photo and wins over
	// the IdP picture in the UI.
	if sc, perr := s.memory.GetProfile(withSessionContext(c.Request().Context(), &sessionContext{Token: tok.AccessToken})); perr == nil && sc != nil {
		if sc.AvatarUrl != "" {
			claims.AvatarOverrideURL = sc.AvatarUrl
		}
		if claims.Name == "" {
			// Zitadel may not emit name/email claims in tokens for this user;
			// fall back to the Memory profile so the session still carries a
			// display name.
			claims.Name = profileDisplayNameOf(sc)
		}
	}
	s.registerAccount(c, flow, claims)
	s.setSessionCookie(c, claims)
	s.clearOAuthStateCookie(c)
	return c.Redirect(http.StatusFound, "/")
}

// profileDisplayNameOf returns a profile's display name (DisplayName, falling
// back to first+last name). Used to fill the session/UI display name when the
// OIDC tokens omit name/email claims.
func profileDisplayNameOf(p *UserProfileDto) string {
	if p == nil {
		return ""
	}
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return strings.TrimSpace(p.FirstName + " " + p.LastName)
}

// exchangeCode performs the authorization_code token exchange at Zitadel.
func (s *Server) exchangeCode(ctx context.Context, tokenEndpoint, code, codeVerifier string) (*oidcTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", s.cfg.ZitadelClientID)
	form.Set("redirect_uri", s.cfg.ZitadelRedirectURI)
	form.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("token exchange: %d: %s", resp.StatusCode, string(raw))
	}
	var tok oidcTokenResponse
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, fmt.Errorf("token exchange: parse: %w", err)
	}
	return &tok, nil
}

// refreshSessionTokens exchanges a refresh token for a fresh token set at the
// Zitadel token endpoint (grant_type=refresh_token).
func (s *Server) refreshSessionTokens(ctx context.Context, refreshToken string) (*oidcTokenResponse, error) {
	doc, err := s.discoverOIDC(ctx)
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", s.cfg.ZitadelClientID)
	form.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, doc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token refresh: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("token refresh: %d: %s", resp.StatusCode, string(raw))
	}
	var tok oidcTokenResponse
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, fmt.Errorf("token refresh: parse: %w", err)
	}
	return &tok, nil
}

// authLogout clears the session (and any leftover oauth flow) cookie and then
// redirects to Zitadel's RP-initiated logout (end_session) when an
// end_session_endpoint is available, terminating the SSO session too; otherwise
// it returns to the root. Local sign-out is unconditional and happens first, so
// it never depends on Zitadel being reachable. It signs out only the current
// account (design D5): other accounts remain in the registry. The install-id
// cookie is intentionally left in place so a re-login still sees the other
// cached accounts.
func (s *Server) authLogout(c echo.Context) error {
	// Capture the active session (best-effort) while its cookie is still
	// present: it supplies the id_token_hint and the sub to drop.
	var current *sessionClaims
	if cur, err := s.currentSession(c); err == nil {
		current = cur
	}
	// Defensive: the active account is normally cookie-only, but a switch/add
	// could have left a stale cached copy; drop it by sub.
	if installID, ok := s.readInstallID(c); ok && current != nil && current.Sub != "" {
		s.reg().remove(installID, current.Sub)
	}
	// Clear locally first — a Zitadel outage must not keep the user signed in.
	s.clearSessionCookie(c)
	s.clearOAuthStateCookie(c)

	if target := s.endSessionRedirectURL(c, current); target != "" {
		return c.Redirect(http.StatusFound, target)
	}
	return c.Redirect(http.StatusFound, "/")
}

// endSessionRedirectURL builds the Zitadel RP-initiated logout URL from the
// discovered end_session_endpoint, or "" when it is unset or discovery fails
// (the caller then falls back to "/"). id_token_hint is sent when the session
// retained the id token; otherwise client_id alone identifies the relying
// party. post_logout_redirect_uri is only added when PUBLIC_BASE_URL is set,
// because Zitadel rejects (or logs) an unregistered redirect URI.
func (s *Server) endSessionRedirectURL(c echo.Context, claims *sessionClaims) string {
	if s.cfg.ZitadelClientID == "" {
		return ""
	}
	doc, err := s.discoverOIDC(c.Request().Context())
	if err != nil || doc.EndSessionEndpoint == "" {
		return ""
	}
	q := url.Values{}
	q.Set("client_id", s.cfg.ZitadelClientID)
	if claims != nil && claims.IDToken != "" {
		q.Set("id_token_hint", claims.IDToken)
	}
	if base := strings.TrimRight(strings.TrimSpace(s.cfg.PublicBaseURL), "/"); base != "" {
		q.Set("post_logout_redirect_uri", base+"/")
	}
	return doc.EndSessionEndpoint + "?" + q.Encode()
}

// oidcErrorPage renders a minimal HTML error page and aborts the request.
func oidcErrorPage(c echo.Context, status int, msg string) error {
	body := "<!doctype html><html><head><meta charset=\"utf-8\"><title>Sign-in failed</title></head>" +
		"<body><h1>Sign-in failed</h1><p>" + htmlEscape(msg) + "</p>" +
		"<p><a href=\"/auth/login\">Try again</a></p></body></html>"
	return c.HTML(status, body)
}

// htmlEscape escapes a message for embedding in an HTML error page.
func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&#34;", "'", "&#39;").Replace(s)
}
