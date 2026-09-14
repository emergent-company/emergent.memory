package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// --- helpers shared by the web-auth tests ---

// testSessionClaims returns valid session claims (future expiry).
func testSessionClaims() sessionClaims {
	return sessionClaims{
		AccessToken:     "at-session",
		RefreshToken:    "rt-session",
		ActiveProjectID: "proj-sess",
		OrgID:           "org-sess",
		ExpiresAt:       time.Now().Add(time.Hour).Unix(),
	}
}

// addSessionCookie signs claims into the memory_session cookie on a request.
func addSessionCookie(t *testing.T, req *http.Request, secret string, claims sessionClaims) {
	t.Helper()
	val, err := issueSession(secret, claims, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: val, Path: "/"})
}

// findCookie returns a Set-Cookie from the recorder by name, or nil.
func findCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == name {
			return ck
		}
	}
	return nil
}

// cookieHeader builds the "Cookie: name=value" request header value.
func cookieHeader(name, value string) string { return name + "=" + value }

// sessionServer mirrors main.go's session-mode wiring: the authDispatch
// middleware, public auth routes, a small /api group, and a UI route.
func sessionServer(s *Server) *echo.Echo {
	e := echo.New()
	e.Use(s.authDispatch)
	e.GET("/auth/login", s.authLogin)
	e.GET("/auth/start", s.authStart)
	e.GET("/auth/add", s.authAdd)
	e.GET("/auth/callback", s.authCallback)
	e.POST("/auth/logout", s.authLogout)
	e.POST("/auth/switch", s.authSwitch)
	api := e.Group("/api")
	api.GET("/health", s.health)
	api.GET("/agents", s.listAgents)
	api.GET("/orgs", s.listOrgs)
	api.POST("/orgs", s.createOrg)
	api.GET("/projects", s.listProjects)
	api.POST("/projects", s.createProject)
	api.POST("/projects/:id/activate", s.activateProject)
	api.GET("/members", s.listMembers)
	api.DELETE("/members/:userId", s.removeMember)
	api.GET("/invites", s.listInvites)
	api.POST("/invites", s.createInvite)
	api.GET("/invites/pending", s.listPendingInvites)
	api.POST("/invites/accept", s.acceptInvite)
	api.POST("/invites/:id/decline", s.declineInvite)
	api.DELETE("/invites/:id", s.cancelInvite)
	api.GET("/users/search", s.searchUsers)
	api.GET("/user/profile", s.getProfile)
	api.PUT("/user/profile", s.updateProfile)
	e.GET("/", func(c echo.Context) error { return c.Redirect(http.StatusFound, "/agents") })
	e.GET("/agents", s.uiAgents)
	return e
}

func sessionCfg() Config {
	return Config{AuthMode: "session", SessionSecret: "test-secret"}
}

// TestSetSessionCookieMaxAgeDecoupled verifies the session cookie's browser
// lifetime no longer tracks the short-lived access token. With SessionMaxAge
// set, the cookie outlives the access token (so the refresh token survives and
// silently renews it); without it, it falls back to the access-token lifetime.
func TestSetSessionCookieMaxAgeDecoupled(t *testing.T) {
	claims := testSessionClaims() // ExpiresAt = now + 1h

	t.Run("configured max age", func(t *testing.T) {
		s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret", SessionMaxAge: 30 * 24 * time.Hour}}
		e := echo.New()
		rec := httptest.NewRecorder()
		s.setSessionCookie(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec), claims)

		ck := findCookie(rec, sessionCookieName)
		if ck == nil {
			t.Fatal("session cookie not set")
		}
		if ck.MaxAge != int((30 * 24 * time.Hour).Seconds()) {
			t.Errorf("MaxAge = %d, want 30 days (%d)", ck.MaxAge, int((30 * 24 * time.Hour).Seconds()))
		}
	})

	t.Run("zero max age falls back to access-token lifetime", func(t *testing.T) {
		s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}}
		e := echo.New()
		rec := httptest.NewRecorder()
		s.setSessionCookie(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec), claims)

		ck := findCookie(rec, sessionCookieName)
		if ck == nil {
			t.Fatal("session cookie not set")
		}
		if ck.MaxAge <= 0 || ck.MaxAge > int(time.Hour.Seconds()) {
			t.Errorf("MaxAge = %d, want within (0, 3600] (access-token lifetime)", ck.MaxAge)
		}
	})
}

// --- requireSession ---

func TestRequireSessionRedirectsWhenUnauthenticated(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()
	e.GET("/protected", func(c echo.Context) error { return c.String(http.StatusOK, "ok") }, s.requireSession)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/protected", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/auth/login" {
		t.Errorf("Location = %q, want /auth/login", loc)
	}
}

func TestRequireSessionServesAndAttachesContext(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()
	e.GET("/protected", func(c echo.Context) error {
		sc, ok := sessionContextFrom(c.Request().Context())
		if !ok {
			return c.String(http.StatusInternalServerError, "no session context")
		}
		return c.JSON(http.StatusOK, map[string]string{"token": sc.Token, "project": sc.ProjectID, "org": sc.OrgID})
	}, s.requireSession)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["token"] != "at-session" || got["project"] != "proj-sess" || got["org"] != "org-sess" {
		t.Errorf("session context = %+v", got)
	}
}

func TestRequireSessionTamperedCookieRedirects(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()
	e.GET("/protected", func(c echo.Context) error { return c.String(http.StatusOK, "ok") }, s.requireSession)

	// Flip a byte in the signature segment: valid b64 shape, wrong signature.
	val, err := issueSession("test-secret", testSessionClaims(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tampered := mutateB64(val, 1)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Cookie", cookieHeader(sessionCookieName, tampered))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if ck := findCookie(rec, sessionCookieName); ck == nil || ck.MaxAge >= 0 {
		t.Errorf("tampered session cookie should be cleared, got %+v", ck)
	}
}

// requireSession is a no-op outside session mode (dev UI stays open).
func TestRequireSessionDevModeOpen(t *testing.T) {
	for _, mode := range []string{"", "dev"} {
		s := &Server{cfg: Config{AuthMode: mode}, memory: &fakeMemory{}}
		e := echo.New()
		e.GET("/protected", func(c echo.Context) error { return c.String(http.StatusOK, "ok") }, s.requireSession)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/protected", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("AuthMode %q: status = %d, want 200 (UI open)", mode, rec.Code)
		}
	}
}

// --- requireSessionOrKey ---

func TestRequireSessionOrKey401WithoutSessionOrKey(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()
	e.GET("/api/ping", func(c echo.Context) error { return c.String(http.StatusOK, "ok") }, s.requireSessionOrKey)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ping", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("body = %s, want JSON error", rec.Body.String())
	}
}

func TestRequireSessionOrKeyServesWithAPIKey(t *testing.T) {
	s := &Server{cfg: func() Config { c := sessionCfg(); c.ClientAPIKey = "admin-secret"; return c }(), memory: &fakeMemory{}}
	e := echo.New()
	e.GET("/api/ping", func(c echo.Context) error { return c.String(http.StatusOK, "ok") }, s.requireSessionOrKey)
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	req.Header.Set("X-API-Key", "admin-secret")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (valid X-API-Key)", rec.Code)
	}
}

func TestRequireSessionOrKeyServesWithSession(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()
	e.GET("/api/ping", func(c echo.Context) error {
		sc, ok := sessionContextFrom(c.Request().Context())
		if !ok {
			return c.String(http.StatusInternalServerError, "no session context")
		}
		return c.JSON(http.StatusOK, map[string]string{"token": sc.Token})
	}, s.requireSessionOrKey)
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (valid session)", rec.Code)
	}
	var got map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["token"] != "at-session" {
		t.Errorf("token = %q, want at-session (session context attached)", got["token"])
	}
}

func TestRequireSessionOrKeyInvalidSessionFallsBackToKey(t *testing.T) {
	s := &Server{cfg: func() Config { c := sessionCfg(); c.ClientAPIKey = "admin-secret"; return c }(), memory: &fakeMemory{}}
	e := echo.New()
	e.GET("/api/ping", func(c echo.Context) error { return c.String(http.StatusOK, "ok") }, s.requireSessionOrKey)
	// Tampered session + valid key → key path wins.
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	req.Header.Set("Cookie", cookieHeader(sessionCookieName, "tampered.value"))
	req.Header.Set("X-API-Key", "admin-secret")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (key fallback)", rec.Code)
	}
}

// In dev mode requireSessionOrKey behaves exactly like requireClientKey.
func TestRequireSessionOrKeyDevModeKeyGated(t *testing.T) {
	for _, mode := range []string{"", "dev"} {
		s := &Server{cfg: Config{AuthMode: mode, ClientAPIKey: "admin-secret"}, memory: &fakeMemory{}}
		e := echo.New()
		e.GET("/api/ping", func(c echo.Context) error { return c.String(http.StatusOK, "ok") }, s.requireSessionOrKey)

		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ping", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("AuthMode %q no key: status = %d, want 401", mode, rec.Code)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
		req.Header.Set("X-API-Key", "admin-secret")
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("AuthMode %q with key: status = %d, want 200", mode, rec.Code)
		}
	}
}

// --- authDispatch (session mode) ---

func TestDispatchSessionModeMatrix(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := sessionServer(s)

	// Public: /api/health needs no auth.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/health = %d, want 200 (public)", rec.Code)
	}

	// UI route without a session → 302 to login.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/auth/login" {
		t.Fatalf("/agents without session = %d (Location %q), want 302 /auth/login", rec.Code, rec.Header().Get("Location"))
	}

	// UI route with a session → 200.
	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/agents with session = %d, want 200", rec.Code)
	}

	// /api route without session or key → 401 JSON.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/api/agents without auth = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("/api/agents body = %s", rec.Body.String())
	}

	// /api route with a valid X-API-Key → 200.
	s2 := &Server{cfg: func() Config { c := sessionCfg(); c.ClientAPIKey = "admin-secret"; return c }(), memory: &fakeMemory{}}
	e2 := sessionServer(s2)
	req = httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("X-API-Key", "admin-secret")
	rec = httptest.NewRecorder()
	e2.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/agents with key = %d, want 200", rec.Code)
	}

	// /api route with a valid session → 200.
	req = httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/agents with session = %d, want 200", rec.Code)
	}
}

// --- authDispatch (dev mode preserves today's behavior) ---

func TestDispatchDevModePreservesBehavior(t *testing.T) {
	// TOKEN_API_KEY configured: /api/* (except /api/setup) key-gated, UI open.
	s := &Server{cfg: Config{ClientAPIKey: "admin-secret"}, memory: &fakeMemory{}}
	e := sessionServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("dev /api/agents no key = %d, want 401", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("X-API-Key", "admin-secret")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dev /api/agents with key = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dev /agents (no session) = %d, want 200 (UI open)", rec.Code)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("dev /api/health no key = %d, want 401 (key-gated like today)", rec.Code)
	}
}

func TestDispatchDevModeOpenWhenNoKeyConfigured(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{}} // AuthMode "" == dev, no TOKEN_API_KEY
	e := sessionServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/agents = %d, want 200 (open in dev without key)", rec.Code)
	}
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/agents = %d, want 200", rec.Code)
	}
}

// --- logout ---

func TestAuthLogoutClearsSession(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("logout = %d (Location %q), want 302 /", rec.Code, rec.Header().Get("Location"))
	}
	if ck := findCookie(rec, sessionCookieName); ck == nil || ck.MaxAge >= 0 {
		t.Errorf("session cookie should be cleared on logout, got %+v", ck)
	}
	if ck := findCookie(rec, oauthStateCookieName); ck == nil || ck.MaxAge >= 0 {
		t.Errorf("oauth cookie should be cleared on logout, got %+v", ck)
	}
}
