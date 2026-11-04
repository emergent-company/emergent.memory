package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// --- decodeIDTokenClaims ---

// b64url returns the unpadded base64url of b.
func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// fakeIDToken builds a 3-segment JWT with the given payload JSON.
func fakeIDToken(payloadJSON string) string {
	return b64url([]byte(`{"alg":"none"}`)) + "." + b64url([]byte(payloadJSON)) + ".sig"
}

func TestDecodeIDTokenClaims(t *testing.T) {
	payload := `{"name":"Ada Lovelace","preferred_username":"ada","email":"ada@example.com","picture":"https://example.com/ada.png","sub":"zitadel-user-1"}`
	name, email, picture, sub := decodeIDTokenClaims(fakeIDToken(payload))
	if name != "Ada Lovelace" || email != "ada@example.com" || picture != "https://example.com/ada.png" {
		t.Errorf("got (%q, %q, %q)", name, email, picture)
	}
	if sub != "zitadel-user-1" {
		t.Errorf("sub = %q, want %q", sub, "zitadel-user-1")
	}
}

func TestDecodeIDTokenClaimsFallsBackToPreferredUsername(t *testing.T) {
	payload := `{"preferred_username":"ada","email":"ada@example.com"}`
	name, email, _, sub := decodeIDTokenClaims(fakeIDToken(payload))
	if name != "ada" {
		t.Errorf("name = %q, want preferred_username fallback 'ada'", name)
	}
	if email != "ada@example.com" {
		t.Errorf("email = %q", email)
	}
	if sub != "" {
		t.Errorf("sub = %q, want empty when absent", sub)
	}
}

func TestDecodeIDTokenClaimsToleratesPadding(t *testing.T) {
	// Same payload, but the middle segment is padded base64url.
	payload := `{"name":"Padded User"}`
	padded := b64url([]byte(`{"alg":"none"}`)) + "." +
		base64.URLEncoding.EncodeToString([]byte(payload)) + ".sig"
	name, _, _, _ := decodeIDTokenClaims(padded)
	if name != "Padded User" {
		t.Errorf("name = %q, want 'Padded User' (padded segment tolerated)", name)
	}
}

func TestDecodeIDTokenClaimsMalformed(t *testing.T) {
	cases := []string{
		"",            // empty
		"onlyone",     // no dots
		"a.",          // empty payload segment
		"a.%%%.b",     // not base64
		"a.notjson.b", // base64 but not JSON
		"a.b",         // fewer than 3 segments still parses the middle
	}
	for _, c := range cases {
		name, email, picture, sub := decodeIDTokenClaims(c)
		if name != "" || email != "" || picture != "" || sub != "" {
			t.Errorf("value %q: got (%q, %q, %q, %q), want empty strings", c, name, email, picture, sub)
		}
	}
}

// --- refreshSessionTokens ---

// refreshSeen records token-endpoint refresh form fields.
type refreshSeen struct {
	hits        int
	GrantType   string
	ClientID    string
	RefreshTok  string
	FailWith    int // HTTP status to fail with, 0 = success
	AccessToken string
	Rotate      bool // whether the response rotates the refresh token
}

// newRefreshZitadel serves discovery + a refresh-capable token endpoint.
func newRefreshZitadel(t *testing.T, seen *refreshSeen) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"authorization_endpoint":"%s/authorize","token_endpoint":"%s/token"}`, srv.URL, srv.URL)
		case "/token":
			seen.hits++
			if err := r.ParseForm(); err != nil {
				t.Errorf("token: parse form: %v", err)
			}
			seen.GrantType = r.Form.Get("grant_type")
			seen.ClientID = r.Form.Get("client_id")
			seen.RefreshTok = r.Form.Get("refresh_token")
			if seen.FailWith != 0 {
				w.WriteHeader(seen.FailWith)
				_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
				return
			}
			access := seen.AccessToken
			if access == "" {
				access = "at-fresh"
			}
			var sb strings.Builder
			sb.WriteString(`{"access_token":"` + access + `"`)
			if seen.Rotate {
				sb.WriteString(`,"refresh_token":"rt-rotated"`)
			}
			sb.WriteString(`,"token_type":"Bearer","expires_in":3600}`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, sb.String())
		default:
			http.NotFound(w, r)
		}
	}))
	return srv
}

func TestRefreshSessionTokens(t *testing.T) {
	seen := &refreshSeen{Rotate: true}
	z := newRefreshZitadel(t, seen)
	defer z.Close()
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test", ZitadelIssuer: z.URL, ZitadelClientID: "client-1"}}

	tok, err := s.refreshSessionTokens(t.Context(), "rt-1")
	if err != nil {
		t.Fatal(err)
	}
	if seen.hits != 1 {
		t.Fatalf("token endpoint hits = %d, want 1", seen.hits)
	}
	if seen.GrantType != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", seen.GrantType)
	}
	if seen.ClientID != "client-1" {
		t.Errorf("client_id = %q, want client-1", seen.ClientID)
	}
	if seen.RefreshTok != "rt-1" {
		t.Errorf("refresh_token = %q, want rt-1", seen.RefreshTok)
	}
	if tok.AccessToken != "at-fresh" || tok.RefreshToken != "rt-rotated" {
		t.Errorf("tokens = %+v", tok)
	}
}

// --- ensureFreshSession ---

// freshProtectedEcho mounts a session-required stub route.
func freshProtectedEcho(s *Server) *echo.Echo {
	e := echo.New()
	e.GET("/protected", func(c echo.Context) error { return c.String(http.StatusOK, "ok") }, s.requireSession)
	return e
}

func refreshCfg(issuer string) Config {
	return Config{
		AuthMode:        "session",
		SessionSecret:   "test-secret",
		ZitadelIssuer:   issuer,
		ZitadelClientID: "client-1",
	}
}

func TestEnsureFreshSessionFreshTokenNoRefresh(t *testing.T) {
	seen := &refreshSeen{}
	z := newRefreshZitadel(t, seen)
	defer z.Close()
	s := &Server{cfg: refreshCfg(z.URL), memory: &fakeMemory{}}
	e := freshProtectedEcho(s)

	claims := sessionClaims{
		AccessToken: "at-live", RefreshToken: "rt-live",
		ActiveProjectID: "proj-sess", OrgID: "org-sess",
		Name: "Ada", Email: "ada@example.com",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	addSessionCookie(t, req, "test-secret", claims)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if seen.hits != 0 {
		t.Errorf("token endpoint hits = %d, want 0 (fresh token not refreshed)", seen.hits)
	}
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		t.Errorf("no cookie re-issue expected for a fresh session: %+v", ck)
	}
}

func TestEnsureFreshSessionNearExpiryRefreshes(t *testing.T) {
	seen := &refreshSeen{Rotate: true}
	z := newRefreshZitadel(t, seen)
	defer z.Close()
	s := &Server{cfg: refreshCfg(z.URL), memory: &fakeMemory{}}
	e := freshProtectedEcho(s)

	claims := sessionClaims{
		AccessToken: "at-old", RefreshToken: "rt-old",
		ActiveProjectID: "proj-sess", OrgID: "org-sess",
		Name: "Ada", Email: "ada@example.com", Picture: "https://example.com/ada.png",
		ExpiresAt: time.Now().Add(4 * time.Minute).Unix(), // inside the 5-min grace window
	}
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	addSessionCookie(t, req, "test-secret", claims)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if seen.hits != 1 {
		t.Fatalf("token endpoint hits = %d, want 1 (grace-window refresh)", seen.hits)
	}
	// cookie re-issued with the fresh tokens; identity + project preserved.
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not re-issued after refresh")
	}
	got, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "at-fresh" || got.RefreshToken != "rt-rotated" {
		t.Errorf("tokens = %+v", got)
	}
	if got.Name != "Ada" || got.Email != "ada@example.com" || got.Picture != "https://example.com/ada.png" {
		t.Errorf("identity not preserved: %+v", got)
	}
	if got.ActiveProjectID != "proj-sess" || got.OrgID != "org-sess" {
		t.Errorf("project/org not preserved: %+v", got)
	}
	wantExp := time.Now().Add(3600 * time.Second).Unix()
	if got.ExpiresAt < wantExp-10 || got.ExpiresAt > wantExp+10 {
		t.Errorf("ExpiresAt = %d, want ~%d", got.ExpiresAt, wantExp)
	}
}

func TestEnsureFreshSessionExpiredRecoversViaRefresh(t *testing.T) {
	seen := &refreshSeen{Rotate: true}
	z := newRefreshZitadel(t, seen)
	defer z.Close()
	s := &Server{cfg: refreshCfg(z.URL), memory: &fakeMemory{}}
	e := freshProtectedEcho(s)

	claims := sessionClaims{
		AccessToken: "at-expired", RefreshToken: "rt-still-good",
		ActiveProjectID: "proj-sess", OrgID: "org-sess",
		Name: "Ada", Email: "ada@example.com",
		ExpiresAt: time.Now().Add(-time.Minute).Unix(), // expired
	}
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	addSessionCookie(t, req, "test-secret", claims)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (recovered via refresh)", rec.Code)
	}
	if seen.hits != 1 {
		t.Fatalf("token endpoint hits = %d, want 1", seen.hits)
	}
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not re-issued after expired-session refresh")
	}
	got, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "at-fresh" || got.RefreshToken != "rt-rotated" {
		t.Errorf("tokens = %+v", got)
	}
	if got.Name != "Ada" || got.ActiveProjectID != "proj-sess" || got.OrgID != "org-sess" {
		t.Errorf("identity/project not preserved: %+v", got)
	}
}

func TestEnsureFreshSessionNoRefreshTokenRedirectsLogin(t *testing.T) {
	seen := &refreshSeen{}
	z := newRefreshZitadel(t, seen)
	defer z.Close()
	s := &Server{cfg: refreshCfg(z.URL), memory: &fakeMemory{}}
	e := freshProtectedEcho(s)

	claims := sessionClaims{
		AccessToken: "at-expired", // no refresh token
		ExpiresAt:   time.Now().Add(-time.Minute).Unix(),
	}
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	addSessionCookie(t, req, "test-secret", claims)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/auth/login" {
		t.Fatalf("status = %d (Location %q), want 302 /auth/login", rec.Code, rec.Header().Get("Location"))
	}
	if seen.hits != 0 {
		t.Errorf("token endpoint hits = %d, want 0 (no refresh token to use)", seen.hits)
	}
	if ck := findCookie(rec, sessionCookieName); ck == nil || ck.MaxAge >= 0 {
		t.Errorf("dead session cookie should be cleared: %+v", ck)
	}
}

func TestEnsureFreshSessionRefreshFailsRedirectsLogin(t *testing.T) {
	seen := &refreshSeen{FailWith: http.StatusBadRequest, Rotate: true}
	z := newRefreshZitadel(t, seen)
	defer z.Close()
	s := &Server{cfg: refreshCfg(z.URL), memory: &fakeMemory{}}
	e := freshProtectedEcho(s)

	claims := sessionClaims{
		AccessToken: "at-expired", RefreshToken: "rt-revoked",
		ExpiresAt: time.Now().Add(-time.Minute).Unix(),
	}
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	addSessionCookie(t, req, "test-secret", claims)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/auth/login" {
		t.Fatalf("status = %d (Location %q), want 302 /auth/login", rec.Code, rec.Header().Get("Location"))
	}
	if seen.hits != 1 {
		t.Errorf("token endpoint hits = %d, want 1 (refresh attempted, failed)", seen.hits)
	}
}

// TestSessionRefreshTokenRecoversFromExpired asserts the signature-only helper
// returns the refresh token from an expired-but-authentic cookie.
func TestSessionRefreshTokenRecoversFromExpired(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: &fakeMemory{}}
	claims := sessionClaims{
		AccessToken: "at", RefreshToken: "rt-hidden",
		ExpiresAt: time.Now().Add(-time.Hour).Unix(),
	}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	addSessionCookie(t, req, "test-secret", claims)
	c := e.NewContext(req, httptest.NewRecorder())

	rt, err := s.sessionRefreshToken(c)
	if err != nil {
		t.Fatal(err)
	}
	if rt != "rt-hidden" {
		t.Errorf("refresh token = %q, want rt-hidden", rt)
	}

	// Tampered cookie → no recovery.
	val, _ := issueSession("test-secret", claims, time.Now())
	tampered := mutateB64(val, 0)
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("Cookie", cookieHeader(sessionCookieName, tampered))
	c2 := e.NewContext(req2, httptest.NewRecorder())
	if _, err := s.sessionRefreshToken(c2); err == nil {
		t.Error("tampered cookie: want error from sessionRefreshToken")
	}
}

// TestAuthCallbackCarriesIdentity asserts the ID token's name/email/picture
// land in the session cookie after the code exchange.
func TestAuthCallbackCarriesIdentity(t *testing.T) {
	srv := newSignedZitadel(t, func(srvURL string, idp *testIDP) map[string]any {
		return map[string]any{
			"access_token":  "at-1",
			"refresh_token": "rt-1",
			"token_type":    "Bearer",
			"id_token": idp.sign(t, testClaims(srvURL, "nonce-1", map[string]any{
				"name":    "Ada Lovelace",
				"email":   "ada@example.com",
				"picture": "https://example.com/ada.png",
			})),
		}
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
	if claims.Name != "Ada Lovelace" || claims.Email != "ada@example.com" || claims.Picture != "https://example.com/ada.png" {
		t.Errorf("identity claims = %+v", claims)
	}
}

// TestRefreshSingleFlight asserts concurrent refreshes of the same refresh
// token hit the token endpoint exactly once (rotating tokens are single-use).
func TestRefreshSingleFlight(t *testing.T) {
	seen := &refreshSeen{}
	z := newRefreshZitadel(t, seen)
	defer z.Close()
	s := &Server{cfg: oidcCfg(z.URL), memory: &fakeMemory{}}
	old := &sessionClaims{RefreshToken: "rt-shared", Sub: "sub-1", ExpiresAt: time.Now().Add(-time.Hour).Unix()}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := s.refreshOnce(old); err != nil {
				t.Errorf("refreshOnce: %v", err)
			}
		})
	}
	wg.Wait()
	if seen.hits != 1 {
		t.Errorf("token endpoint hits = %d, want 1 (single-flight)", seen.hits)
	}
}
