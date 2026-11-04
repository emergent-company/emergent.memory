package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// testIDP is a self-contained RSA signing identity for the fake Zitadel. It
// serves a JWKS and mints RS256 ID tokens so the gateway's signature/iss/aud/exp
// verification (verifyIDToken) has something real to check.
type testIDP struct {
	key *rsa.PrivateKey
	kid string
}

func newTestIDP(t *testing.T) *testIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &testIDP{key: key, kid: "test-kid"}
}

// jwksJSON returns the JWKS document for the test key.
func (idp *testIDP) jwksJSON(t *testing.T) []byte {
	t.Helper()
	n := base64.RawURLEncoding.EncodeToString(idp.key.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(idp.key.E)).Bytes())
	doc := map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA", "kid": idp.kid, "use": "sig", "alg": "RS256", "n": n, "e": e,
		}},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// sign returns an RS256-signed JWT with the given claims and the test kid.
func (idp *testIDP) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": idp.kid})
	pl, _ := json.Marshal(claims)
	signing := base64.RawURLEncoding.EncodeToString(hdr) + "." + base64.RawURLEncoding.EncodeToString(pl)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, idp.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// testClaims builds the required registered claims (iss/aud/exp) for a signed
// test ID token, merging any extra claims (e.g. sub/name/email).
func testClaims(issuer, nonce string, extra map[string]any) map[string]any {
	m := map[string]any{
		"iss": issuer,
		"aud": "client-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	if nonce != "" {
		m["nonce"] = nonce
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// tamperSignature flips a byte of the JWT signature segment, keeping it valid
// base64 but breaking the cryptographic signature.
func tamperSignature(t *testing.T, token string) string {
	t.Helper()
	i := strings.LastIndexByte(token, '.')
	if i < 0 {
		t.Fatalf("tamperSignature: token has no signature segment")
	}
	sig, err := base64.RawURLEncoding.DecodeString(token[i+1:])
	if err != nil || len(sig) == 0 {
		t.Fatalf("tamperSignature: invalid signature segment: %v", err)
	}
	sig[0] ^= 0xff
	return token[:i+1] + base64.RawURLEncoding.EncodeToString(sig)
}

// newSignedZitadel starts a fake Zitadel that serves discovery + JWKS and
// answers every other path with the token response returned by build (given the
// server URL and the signing identity). build may return nil for a canned,
// nonce-1 response. Use this for tests that need to vary the id_token claims.
func newSignedZitadel(t *testing.T, build func(srvURL string, idp *testIDP) map[string]any) *httptest.Server {
	t.Helper()
	idp := newTestIDP(t)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
				srv.URL+"/authorize", srv.URL+"/token", srv.URL+"/jwks")
		case "/jwks":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(idp.jwksJSON(t))
		default:
			resp := build(srv.URL, idp)
			if resp == nil {
				resp = map[string]any{
					"access_token": "at-oidc",
					"token_type":   "Bearer",
					"id_token":     idp.sign(t, testClaims(srv.URL, "nonce-1", nil)),
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newFakeZitadel serves the discovery document and a token endpoint that
// records the exchange form and returns a canned token response.
func newFakeZitadel(t *testing.T) (*httptest.Server, *tokenExchangeSeen) {
	t.Helper()
	seen := &tokenExchangeSeen{}
	idp := newTestIDP(t)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{
				"authorization_endpoint": %q,
				"token_endpoint": %q,
				"end_session_endpoint": %q,
				"jwks_uri": %q
			}`, srv.URL+"/authorize", srv.URL+"/token", srv.URL+"/logout", srv.URL+"/jwks")
		case "/jwks":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(idp.jwksJSON(t))
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("token: parse form: %v", err)
			}
			seen.GrantType = r.Form.Get("grant_type")
			seen.Code = r.Form.Get("code")
			seen.ClientID = r.Form.Get("client_id")
			seen.RedirectURI = r.Form.Get("redirect_uri")
			seen.CodeVerifier = r.Form.Get("code_verifier")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "at-oidc",
				"refresh_token": "rt-oidc",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"id_token":      idp.sign(t, testClaims(srv.URL, "nonce-1", nil)),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, seen
}

// tokenExchangeSeen records the token-endpoint form fields the gateway sent.
type tokenExchangeSeen struct {
	GrantType    string
	Code         string
	ClientID     string
	RedirectURI  string
	CodeVerifier string
}

func oidcCfg(issuer string) Config {
	return Config{
		AuthMode:           "session",
		SessionSecret:      "test-secret",
		ZitadelIssuer:      issuer,
		ZitadelClientID:    "client-1",
		ZitadelRedirectURI: "http://gw.test/auth/callback",
	}
}

// addOAuthCookie attaches a signed memory_oauth flow cookie for the given
// state/verifier.
func addOAuthCookie(t *testing.T, req *http.Request, secret, state, verifier, nonce string) {
	t.Helper()
	val, err := issueOAuthState(secret, state, verifier, nonce, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", cookieHeader(oauthStateCookieName, val))
}

// TestAuthStartRedirectsToAuthorize asserts /auth/start (the sign-in page's
// action) 302s to the Zitadel authorization endpoint with the full PKCE
// parameter set.
func TestAuthStartRedirectsToAuthorize(t *testing.T) {
	z, _ := newFakeZitadel(t)
	defer z.Close()
	s := &Server{cfg: oidcCfg(z.URL), memory: &fakeMemory{}}
	e := sessionServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/start", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, z.URL+"/authorize?") {
		t.Fatalf("Location = %q, want %s/authorize?...", loc, z.URL)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"response_type":         "code",
		"client_id":             "client-1",
		"redirect_uri":          "http://gw.test/auth/callback",
		"scope":                 "openid profile email offline_access",
		"code_challenge_method": "S256",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("query %s = %q, want %q", k, got, want)
		}
	}
	if cc := q.Get("code_challenge"); len(cc) != 43 {
		t.Errorf("code_challenge = %q, want a 43-char base64url SHA-256", cc)
	}
	if q.Get("state") == "" || q.Get("nonce") == "" {
		t.Errorf("state/nonce missing: %v", q)
	}
	// A short-lived oauth flow cookie is set; no session cookie yet.
	if ck := findCookie(rec, oauthStateCookieName); ck == nil || ck.MaxAge <= 0 {
		t.Errorf("memory_oauth cookie missing: %+v", ck)
	}
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		t.Errorf("session cookie must not be set by login: %+v", ck)
	}
}

// TestAuthCallbackEstablishesSession asserts a valid state+code exchange sets
// the session cookie and redirects home.
func TestAuthCallbackEstablishesSession(t *testing.T) {
	z, seen := newFakeZitadel(t)
	defer z.Close()
	s := &Server{cfg: oidcCfg(z.URL), memory: &fakeMemory{}}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=st-valid&code=the-code", nil)
	addOAuthCookie(t, req, "test-secret", "st-valid", "v3r1f13r-0123456789", "nonce-1")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (%s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want /", loc)
	}
	// token exchange form is correct
	if seen.GrantType != "authorization_code" || seen.Code != "the-code" ||
		seen.ClientID != "client-1" || seen.RedirectURI != "http://gw.test/auth/callback" ||
		seen.CodeVerifier != "v3r1f13r-0123456789" {
		t.Errorf("token exchange = %+v", seen)
	}
	// session cookie established with the exchanged tokens
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not set")
	}
	claims, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatalf("verify session: %v", err)
	}
	if claims.AccessToken != "at-oidc" || claims.RefreshToken != "rt-oidc" {
		t.Errorf("claims tokens = %+v", claims)
	}
	wantExp := time.Now().Add(3600 * time.Second).Unix()
	if claims.ExpiresAt < wantExp-10 || claims.ExpiresAt > wantExp+10 {
		t.Errorf("ExpiresAt = %d, want ~%d (now + expires_in)", claims.ExpiresAt, wantExp)
	}
	// oauth flow cookie cleared
	if oc := findCookie(rec, oauthStateCookieName); oc == nil || oc.MaxAge >= 0 {
		t.Errorf("oauth flow cookie should be cleared, got %+v", oc)
	}
}

// TestAuthCallbackRejectsMismatchedState asserts a state mismatch does not
// establish a session.
func TestAuthCallbackRejectsMismatchedState(t *testing.T) {
	z, _ := newFakeZitadel(t)
	defer z.Close()
	s := &Server{cfg: oidcCfg(z.URL), memory: &fakeMemory{}}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=wrong&code=the-code", nil)
	addOAuthCookie(t, req, "test-secret", "st-valid", "v3r1f13r", "nonce-1")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		t.Errorf("session cookie must not be set on state mismatch: %+v", ck)
	}
}

// TestAuthCallbackErrorParamNoSession asserts a provider error param does not
// establish a session.
func TestAuthCallbackErrorParamNoSession(t *testing.T) {
	z, _ := newFakeZitadel(t)
	defer z.Close()
	s := &Server{cfg: oidcCfg(z.URL), memory: &fakeMemory{}}
	e := sessionServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/callback?error=access_denied&error_description=user+said+no", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "access_denied") {
		t.Errorf("error page should surface the provider error: %s", rec.Body.String())
	}
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		t.Errorf("session cookie must not be set on provider error: %+v", ck)
	}
}

// TestAuthCallbackMissingStateCookieNoSession asserts a callback without the
// flow cookie is rejected.
func TestAuthCallbackMissingStateCookieNoSession(t *testing.T) {
	z, _ := newFakeZitadel(t)
	defer z.Close()
	s := &Server{cfg: oidcCfg(z.URL), memory: &fakeMemory{}}
	e := sessionServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/callback?state=st-valid&code=the-code", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		t.Errorf("session cookie must not be set without an oauth flow cookie: %+v", ck)
	}
}

// TestAuthStartDevModeRedirectsHome asserts /auth/start is a no-op in dev
// mode (no session to protect).
func TestAuthStartDevModeRedirectsHome(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "dev"}, memory: &fakeMemory{}}
	e := sessionServer(s)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/start", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("dev /auth/start = %d (Location %q), want 302 /", rec.Code, rec.Header().Get("Location"))
	}
}

// TestAuthCallbackUsesOfflineRefreshToken asserts a token response without a
// refresh token still establishes the session (refresh is optional).
func TestAuthCallbackNoRefreshToken(t *testing.T) {
	srv := newSignedZitadel(t, func(srvURL string, idp *testIDP) map[string]any {
		return map[string]any{
			"access_token": "at-only",
			"token_type":   "Bearer",
			"id_token":     idp.sign(t, testClaims(srvURL, "nonce-1", nil)),
		} // no expires_in, no refresh_token
	})
	s := &Server{cfg: oidcCfg(srv.URL), memory: &fakeMemory{}}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=st-valid&code=the-code", nil)
	addOAuthCookie(t, req, "test-secret", "st-valid", "v3r1f13r", "nonce-1")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not set")
	}
	claims, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims.AccessToken != "at-only" {
		t.Errorf("AccessToken = %q", claims.AccessToken)
	}
	// expires_in missing → default 3600s.
	wantExp := time.Now().Add(3600 * time.Second).Unix()
	if claims.ExpiresAt < wantExp-10 || claims.ExpiresAt > wantExp+10 {
		t.Errorf("ExpiresAt = %d, want ~%d", claims.ExpiresAt, wantExp)
	}
}

// TestAuthCallbackRejectsNonceMismatch asserts an ID token whose nonce does not
// match the one sent in the auth request is rejected (no session established).
func TestAuthCallbackRejectsNonceMismatch(t *testing.T) {
	// Correctly signed token, but its nonce does not match the flow cookie.
	srv := newSignedZitadel(t, func(srvURL string, idp *testIDP) map[string]any {
		return map[string]any{
			"access_token": "at-oidc",
			"token_type":   "Bearer",
			"id_token":     idp.sign(t, testClaims(srvURL, "attacker-nonce", nil)),
		}
	})
	s := &Server{cfg: oidcCfg(srv.URL), memory: &fakeMemory{}}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=st-valid&code=the-code", nil)
	addOAuthCookie(t, req, "test-secret", "st-valid", "v3r1f13r", "nonce-1")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		t.Errorf("session cookie must not be set on nonce mismatch: %+v", ck)
	}
}

// logoutSessionClaims is a valid session that retains an id_token, as the
// callback now does.
func logoutSessionClaims(idToken string) sessionClaims {
	return sessionClaims{
		AccessToken: "at-logout",
		IDToken:     idToken,
		Sub:         "sub-active",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}
}

// TestAuthLogoutRedirectsToEndSession asserts logout clears the local cookies
// and then 302s to the discovered end_session_endpoint with id_token_hint +
// client_id when the id token was retained.
func TestAuthLogoutRedirectsToEndSession(t *testing.T) {
	z, _ := newFakeZitadel(t)
	defer z.Close()
	s := &Server{cfg: oidcCfg(z.URL), memory: &fakeMemory{}}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	addSessionCookie(t, req, "test-secret", logoutSessionClaims("id-tok-123"))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, z.URL+"/logout?") {
		t.Fatalf("Location = %q, want %s/logout?...", loc, z.URL)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != "client-1" {
		t.Errorf("client_id = %q, want client-1", q.Get("client_id"))
	}
	if q.Get("id_token_hint") != "id-tok-123" {
		t.Errorf("id_token_hint = %q, want id-tok-123", q.Get("id_token_hint"))
	}
	// PUBLIC_BASE_URL unset → no unregistered post_logout_redirect_uri.
	if q.Has("post_logout_redirect_uri") {
		t.Errorf("post_logout_redirect_uri must be absent without PUBLIC_BASE_URL: %v", q)
	}
	// Local cookies are cleared regardless of the Zitadel redirect.
	if ck := findCookie(rec, sessionCookieName); ck == nil || ck.MaxAge >= 0 {
		t.Errorf("session cookie should be cleared, got %+v", ck)
	}
	if ck := findCookie(rec, oauthStateCookieName); ck == nil || ck.MaxAge >= 0 {
		t.Errorf("oauth cookie should be cleared, got %+v", ck)
	}
}

// TestAuthLogoutEndSessionAddsPostLogoutRedirect asserts
// post_logout_redirect_uri is added (base + "/") when PUBLIC_BASE_URL is set.
func TestAuthLogoutEndSessionAddsPostLogoutRedirect(t *testing.T) {
	z, _ := newFakeZitadel(t)
	defer z.Close()
	s := &Server{cfg: func() Config {
		c := oidcCfg(z.URL)
		c.PublicBaseURL = "https://gw.example.com/"
		return c
	}(), memory: &fakeMemory{}}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	addSessionCookie(t, req, "test-secret", logoutSessionClaims("id-tok-123"))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("post_logout_redirect_uri"); got != "https://gw.example.com/" {
		t.Errorf("post_logout_redirect_uri = %q, want https://gw.example.com/", got)
	}
}

// TestAuthLogoutWithoutIDTokenSendsClientIDOnly asserts logout falls back to a
// client_id-only end_session URL when no id token was retained.
func TestAuthLogoutWithoutIDTokenSendsClientIDOnly(t *testing.T) {
	z, _ := newFakeZitadel(t)
	defer z.Close()
	s := &Server{cfg: oidcCfg(z.URL), memory: &fakeMemory{}}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	addSessionCookie(t, req, "test-secret", logoutSessionClaims("")) // no id_token retained
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != "client-1" {
		t.Errorf("client_id = %q, want client-1", q.Get("client_id"))
	}
	if q.Has("id_token_hint") {
		t.Errorf("id_token_hint should be absent without a retained id token: %v", q)
	}
}

// TestAuthLogoutFallsBackHomeWithoutEndSession asserts logout redirects to "/"
// when discovery has no end_session_endpoint or is unreachable.
func TestAuthLogoutFallsBackHomeWithoutEndSession(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"authorization_endpoint":"%s/authorize","token_endpoint":"%s/token"}`, srv.URL, srv.URL)
	}))
	defer srv.Close()

	s := &Server{cfg: oidcCfg(srv.URL), memory: &fakeMemory{}}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	addSessionCookie(t, req, "test-secret", logoutSessionClaims("id-tok-123"))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("logout = %d (Location %q), want 302 /", rec.Code, rec.Header().Get("Location"))
	}
	if ck := findCookie(rec, sessionCookieName); ck == nil || ck.MaxAge >= 0 {
		t.Errorf("session cookie should still be cleared on fallback, got %+v", ck)
	}
}

// TestAuthLogoutKeepsInstallIDAndCachedAccounts asserts sign-out ends only the
// active account: the install-id cookie is untouched and other cached accounts
// survive (design D5).
func TestAuthLogoutKeepsInstallIDAndCachedAccounts(t *testing.T) {
	z, _ := newFakeZitadel(t)
	defer z.Close()
	s := &Server{cfg: oidcCfg(z.URL), memory: &fakeMemory{}, registry: newAccountRegistry()}
	e := sessionServer(s)

	installVal, err := issueInstallID("test-secret", "install-1")
	if err != nil {
		t.Fatal(err)
	}
	s.reg().put("install-1", accountSession{Sub: "sub-other", AccessToken: "at-other"})

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: installCookieName, Value: installVal, Path: "/"})
	addSessionCookie(t, req, "test-secret", logoutSessionClaims("id-tok-123"))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if ck := findCookie(rec, installCookieName); ck != nil {
		t.Errorf("install-id cookie must not be cleared on logout, got %+v", ck)
	}
	remaining := s.reg().list("install-1")
	if len(remaining) != 1 || remaining[0].Sub != "sub-other" {
		t.Errorf("cached accounts = %+v, want only sub-other intact", remaining)
	}
}

// TestSetSessionCookieShedsOversizedIDToken locks the cookie-size tradeoff:
// an id_token that would push the signed cookie past the browser limit is
// dropped (logout then falls back to client_id), while a small one is kept.
func TestSetSessionCookieShedsOversizedIDToken(t *testing.T) {
	t.Run("oversized id_token dropped", func(t *testing.T) {
		s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}}
		claims := logoutSessionClaims(strings.Repeat("x", maxSessionCookieValueBytes+200))
		e := echo.New()
		rec := httptest.NewRecorder()
		s.setSessionCookie(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec), claims)

		ck := findCookie(rec, sessionCookieName)
		if ck == nil {
			t.Fatal("session cookie not set")
		}
		if len(ck.Value) > maxSessionCookieValueBytes {
			t.Errorf("cookie value = %d bytes, want <= %d", len(ck.Value), maxSessionCookieValueBytes)
		}
		got, err := verifySession("test-secret", ck.Value, time.Now())
		if err != nil {
			t.Fatalf("verify session: %v", err)
		}
		if got.IDToken != "" {
			t.Errorf("IDToken = %q, want dropped", got.IDToken)
		}
		if got.AccessToken != "at-logout" {
			t.Errorf("AccessToken = %q, want preserved", got.AccessToken)
		}
	})

	t.Run("small id_token retained", func(t *testing.T) {
		s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}}
		e := echo.New()
		rec := httptest.NewRecorder()
		s.setSessionCookie(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec), logoutSessionClaims("small-id-tok"))

		ck := findCookie(rec, sessionCookieName)
		if ck == nil {
			t.Fatal("session cookie not set")
		}
		got, err := verifySession("test-secret", ck.Value, time.Now())
		if err != nil {
			t.Fatalf("verify session: %v", err)
		}
		if got.IDToken != "small-id-tok" {
			t.Errorf("IDToken = %q, want small-id-tok retained", got.IDToken)
		}
	})
}

// TestAuthCallbackRejectsTamperedSignature asserts a token whose signature does
// not verify against the provider JWKS is rejected.
func TestAuthCallbackRejectsTamperedSignature(t *testing.T) {
	srv := newSignedZitadel(t, func(srvURL string, idp *testIDP) map[string]any {
		return map[string]any{
			"access_token": "at-oidc",
			"token_type":   "Bearer",
			"id_token":     tamperSignature(t, idp.sign(t, testClaims(srvURL, "nonce-1", nil))),
		}
	})
	s := &Server{cfg: oidcCfg(srv.URL), memory: &fakeMemory{}}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=st-valid&code=the-code", nil)
	addOAuthCookie(t, req, "test-secret", "st-valid", "v3r1f13r", "nonce-1")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		t.Errorf("session cookie must not be set on signature failure: %+v", ck)
	}
}

// TestAuthCallbackFallsBackToProfileName asserts that when the Zitadel ID token
// carries a sub but no name/email claims, the callback populates the session's
// display name from the Memory profile.
func TestAuthCallbackFallsBackToProfileName(t *testing.T) {
	srv := newSignedZitadel(t, func(srvURL string, idp *testIDP) map[string]any {
		return map[string]any{
			"access_token":  "at-oidc",
			"refresh_token": "rt-oidc",
			"token_type":    "Bearer",
			"expires_in":    3600,
			// ID token has a sub but no name/email/preferred_username.
			"id_token": idp.sign(t, testClaims(srvURL, "nonce-1", map[string]any{"sub": "389329982813372426"})),
		}
	})

	s := &Server{
		cfg:      oidcCfg(srv.URL),
		memory:   &fakeMemory{profile: &UserProfileDto{DisplayName: "Maciej Kucharz", FirstName: "Maciej", LastName: "Kucharz"}},
		registry: newAccountRegistry(),
	}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=st-valid&code=the-code", nil)
	addOAuthCookie(t, req, "test-secret", "st-valid", "v3r1f13r-0123456789", "nonce-1")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not set")
	}
	claims, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims.Name != "Maciej Kucharz" {
		t.Errorf("Name = %q, want profile displayName fallback 'Maciej Kucharz'", claims.Name)
	}
	if claims.Sub != "389329982813372426" {
		t.Errorf("Sub = %q, want 389329982813372426", claims.Sub)
	}
}
