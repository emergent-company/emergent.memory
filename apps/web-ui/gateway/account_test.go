package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// addInstallCookie attaches a signed memory_install cookie for the given id.
func addInstallCookie(t *testing.T, req *http.Request, secret, id string) {
	t.Helper()
	val, err := issueInstallID(secret, id)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: installCookieName, Value: val, Path: "/"})
}

// addOAuthCookiePrompt attaches a signed memory_oauth flow cookie carrying the
// given prompt ("" for a plain sign-in, "select_account" for add-account).
func addOAuthCookiePrompt(t *testing.T, req *http.Request, secret, state, verifier, nonce, prompt string) {
	t.Helper()
	val, err := issueOAuthStatePrompt(secret, state, verifier, nonce, prompt, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", cookieHeader(oauthStateCookieName, val))
}

// carryCookies copies the Set-Cookie values from a prior response onto the next
// request (used to thread session + install cookies across callback steps).
func carryCookies(t *testing.T, req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()
	for _, ck := range rec.Result().Cookies() {
		req.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value, Path: ck.Path})
	}
}

// --- install-id cookie (task 2.1) ---

func TestInstallIDRoundTrip(t *testing.T) {
	val, err := issueInstallID("secret", "install-abc")
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifyInstallID("secret", val)
	if err != nil {
		t.Fatal(err)
	}
	if got != "install-abc" {
		t.Errorf("install id = %q, want %q", got, "install-abc")
	}
}

func TestInstallIDTamperRejected(t *testing.T) {
	val, err := issueInstallID("secret", "install-abc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyInstallID("secret", mutateB64(val, 0)); err == nil {
		t.Error("tampered install id: want error, got nil")
	}
	if _, err := verifyInstallID("wrong-secret", val); err == nil {
		t.Error("wrong key install id: want error, got nil")
	}
}

// --- account registry (task 2.2) ---

func TestAccountRegistryPutListRemove(t *testing.T) {
	r := newAccountRegistry()
	r.put("inst", accountSession{Sub: "a", Name: "Ada"})
	r.put("inst", accountSession{Sub: "b", Name: "Bob"})
	if got := r.list("inst"); len(got) != 2 {
		t.Fatalf("list len = %d, want 2", len(got))
	}

	// put with the same sub replaces in place (no duplicate)
	r.put("inst", accountSession{Sub: "a", Name: "Ada Updated"})
	list := r.list("inst")
	if len(list) != 2 {
		t.Fatalf("after replace list len = %d, want 2", len(list))
	}
	for _, a := range list {
		if a.Sub == "a" && a.Name != "Ada Updated" {
			t.Errorf("replace did not update sub a: %+v", a)
		}
	}

	if got, ok := r.get("inst", "b"); !ok || got.Name != "Bob" {
		t.Errorf("get b = %+v, %v", got, ok)
	}

	r.remove("inst", "a")
	if _, ok := r.get("inst", "a"); ok {
		t.Error("remove a: still present")
	}
	if got := r.list("inst"); len(got) != 1 || got[0].Sub != "b" {
		t.Errorf("after remove list = %+v", got)
	}

	// removing the last account drops the install key
	r.remove("inst", "b")
	if got := r.list("inst"); len(got) != 0 {
		t.Errorf("after removing last, list = %+v", got)
	}
}

func TestAccountRegistryNilSafe(t *testing.T) {
	var r *accountRegistry
	if r.list("inst") != nil {
		t.Error("nil list should return nil")
	}
	r.put("inst", accountSession{Sub: "a"}) // must not panic
	if _, ok := r.get("inst", "a"); ok {
		t.Error("nil get should return ok=false")
	}
	r.remove("inst", "a") // must not panic
}

func TestAccountRegistryConcurrent(t *testing.T) {
	r := newAccountRegistry()
	var wg sync.WaitGroup
	for i := range 200 {
		wg.Go(func() {
			r.put("inst", accountSession{Sub: fmt.Sprintf("s%d", i%10)})
		})
	}
	wg.Wait()
	if got := r.list("inst"); len(got) != 10 {
		t.Errorf("concurrent put: list len = %d, want 10 unique subs", len(got))
	}
}

// --- /auth/add prompt (task 3.1) ---

func TestAuthAddPromptSelectAccount(t *testing.T) {
	z, _ := newFakeZitadel(t)
	defer z.Close()
	s := &Server{cfg: oidcCfg(z.URL), memory: &fakeMemory{}, registry: newAccountRegistry()}
	e := sessionServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/add", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("prompt"); got != "select_account" {
		t.Errorf("prompt = %q, want select_account", got)
	}
}

func TestAuthStartNoPrompt(t *testing.T) {
	z, _ := newFakeZitadel(t)
	defer z.Close()
	s := &Server{cfg: oidcCfg(z.URL), memory: &fakeMemory{}, registry: newAccountRegistry()}
	e := sessionServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/start", nil))
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("prompt"); got != "" {
		t.Errorf("prompt = %q, want empty on the normal path", got)
	}
}

// --- /auth/switch (task 3.2) ---

func TestAuthSwitchRotatesAccount(t *testing.T) {
	s := &Server{
		cfg:      Config{AuthMode: "session", SessionSecret: "test-secret"},
		memory:   &fakeMemory{},
		registry: newAccountRegistry(),
	}
	// Inactive account B cached in the registry under install "inst".
	s.registry.put("inst", accountSession{
		Sub: "sub-b", Name: "Bob", Email: "bob@x.io",
		AccessToken: "at-b", RefreshToken: "rt-b",
		ActiveProjectID: "proj-b", ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPost, "/auth/switch", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.PostForm = url.Values{"sub": {"sub-b"}}
	// Active account A (cookie only).
	addSessionCookie(t, req, "test-secret", sessionClaims{
		AccessToken: "at-a", RefreshToken: "rt-a",
		ActiveProjectID: "proj-a",
		Name:            "Ada", Email: "ada@x.io", Sub: "sub-a",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	addInstallCookie(t, req, "test-secret", "inst")

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (%s)", rec.Code, rec.Body.String())
	}

	// Active cookie now carries account B (identity + project restored).
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not set")
	}
	claims, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims.Sub != "sub-b" || claims.Name != "Bob" || claims.ActiveProjectID != "proj-b" {
		t.Errorf("active claims = %+v, want sub-b/Bob/proj-b", claims)
	}

	// A rotated into the registry; B no longer there.
	if _, ok := s.registry.get("inst", "sub-b"); ok {
		t.Error("sub-b should be removed from the registry (now active)")
	}
	if got, ok := s.registry.get("inst", "sub-a"); !ok || got.Name != "Ada" {
		t.Errorf("sub-a should be rotated in, got %+v/%v", got, ok)
	}
}

// --- /auth/logout current-account-only (task 3.3) ---

func TestAuthLogoutKeepsOtherAccounts(t *testing.T) {
	s := &Server{
		cfg:      Config{AuthMode: "session", SessionSecret: "test-secret"},
		memory:   &fakeMemory{},
		registry: newAccountRegistry(),
	}
	// Another account B cached (must survive logout of A).
	s.registry.put("inst", accountSession{Sub: "sub-b", Name: "Bob", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	addSessionCookie(t, req, "test-secret", sessionClaims{
		AccessToken: "at-a", Name: "Ada", Sub: "sub-a",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	addInstallCookie(t, req, "test-secret", "inst")

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	// session cookie cleared
	if ck := findCookie(rec, sessionCookieName); ck != nil && ck.MaxAge >= 0 {
		t.Errorf("session cookie should be cleared, got %+v", ck)
	}
	// other account B still cached
	if _, ok := s.registry.get("inst", "sub-b"); !ok {
		t.Error("sub-b must remain in the registry after logging out account A")
	}
}

// --- dedup on callback (task 3.4) ---

// newSubZitadel serves discovery + JWKS and a token endpoint whose id_token
// `sub` is driven by the mutable sub value, so one callback after another can
// simulate different accounts.
func newSubZitadel(t *testing.T, sub *string) *httptest.Server {
	t.Helper()
	return newSignedZitadel(t, func(srvURL string, idp *testIDP) map[string]any {
		s := *sub
		return map[string]any{
			"access_token":  "at-" + s,
			"refresh_token": "rt-" + s,
			"token_type":    "Bearer",
			"expires_in":    3600,
			"id_token":      idp.sign(t, testClaims(srvURL, "nonce-1", map[string]any{"sub": s, "email": s + "@x.io"})),
		}
	})
}

func TestAuthCallbackAddAndDedup(t *testing.T) {
	sub := "sub-a"
	z := newSubZitadel(t, &sub)
	defer z.Close()
	s := &Server{cfg: oidcCfg(z.URL), memory: &fakeMemory{}, registry: newAccountRegistry()}
	e := sessionServer(s)

	// 1. Plain sign-in → account A active.
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/auth/callback?state=s1&code=c1", nil)
	addOAuthCookiePrompt(t, req1, "test-secret", "s1", "v3r1f13r-0123456789", "nonce-1", "")
	e.ServeHTTP(rec1, req1)
	if ck := findCookie(rec1, sessionCookieName); ck == nil {
		t.Fatal("step 1: session cookie not set")
	}
	if id := findCookie(rec1, installCookieName); id == nil {
		t.Fatal("step 1: install cookie not set")
	}
	instID, err := verifyInstallID("test-secret", findCookie(rec1, installCookieName).Value)
	if err != nil {
		t.Fatalf("step 1: install id: %v", err)
	}

	// 2. Add another account (B) while A is active.
	sub = "sub-b"
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/auth/callback?state=s2&code=c2", nil)
	addOAuthCookiePrompt(t, req2, "test-secret", "s2", "v3r1f13r-0123456789", "nonce-1", "select_account")
	carryCookies(t, req2, rec1)
	e.ServeHTTP(rec2, req2)
	ck2 := findCookie(rec2, sessionCookieName)
	if ck2 == nil {
		t.Fatal("step 2: session cookie not set")
	}
	claims2, _ := verifySession("test-secret", ck2.Value, time.Now())
	if claims2.Sub != "sub-b" {
		t.Fatalf("step 2: active sub = %q, want sub-b", claims2.Sub)
	}
	// A now in the registry, exactly one entry.
	if list := s.registry.list(instID); len(list) != 1 || list[0].Sub != "sub-a" {
		t.Fatalf("step 2: registry = %+v, want [sub-a]", list)
	}
	_ = rec2

	// 3. Re-add account A → dedup: no duplicate, A becomes active again, B cached.
	sub = "sub-a"
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/auth/callback?state=s3&code=c3", nil)
	addOAuthCookiePrompt(t, req3, "test-secret", "s3", "v3r1f13r-0123456789", "nonce-1", "select_account")
	carryCookies(t, req3, rec2)
	addInstallCookie(t, req3, "test-secret", instID) // rec2 does not re-send the install cookie
	e.ServeHTTP(rec3, req3)
	ck3 := findCookie(rec3, sessionCookieName)
	if ck3 == nil {
		t.Fatal("step 3: session cookie not set")
	}
	claims3, _ := verifySession("test-secret", ck3.Value, time.Now())
	if claims3.Sub != "sub-a" {
		t.Fatalf("step 3: active sub = %q, want sub-a", claims3.Sub)
	}
	// B rotated in; A active (removed from registry). No duplicate of A.
	list := s.registry.list(instID)
	if len(list) != 1 || list[0].Sub != "sub-b" {
		t.Fatalf("step 3: registry = %+v, want [sub-b]", list)
	}
}

// --- resolveDefaultProject middleware ---

// TestResolveDefaultProjectAutoPicks asserts a project-scoped page auto-selects
// the first project when the session has none (new-user case).
func TestResolveDefaultProjectAutoPicks(t *testing.T) {
	f := &fakeMemory{
		projects: []ProjectRef{{ID: "p1", Name: "Main", OrgID: "o1"}},
		orgs:     []Org{{ID: "o1", Name: "Acme"}},
	}
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	addSessionCookie(t, req, "test-secret", sessionClaims{
		AccessToken: "at",
		Sub:         "sub-a",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
		// no ActiveProjectID
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session /agents = %d, want 200", rec.Code)
	}
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not re-issued with the auto-picked project")
	}
	claims, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims.ActiveProjectID != "p1" {
		t.Errorf("ActiveProjectID = %q, want auto-picked p1", claims.ActiveProjectID)
	}
}

// TestResolveDefaultProjectRedirectsWithoutProjects asserts a project-scoped
// page redirects to /orgs when the user has no projects to pick.
func TestResolveDefaultProjectRedirectsWithoutProjects(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: &fakeMemory{}}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	addSessionCookie(t, req, "test-secret", sessionClaims{
		AccessToken: "at",
		Sub:         "sub-a",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/orgs" {
		t.Errorf("Location = %q, want /orgs", loc)
	}
}

// TestResolveDefaultProjectNoopWithProject asserts no redirect/re-pick when the
// session already has an active project.
func TestResolveDefaultProjectNoopWithProject(t *testing.T) {
	f := &fakeMemory{projects: []ProjectRef{{ID: "p2", Name: "Other", OrgID: "o2"}}}
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	addSessionCookie(t, req, "test-secret", sessionClaims{
		AccessToken:     "at",
		ActiveProjectID: "p2",
		Sub:             "sub-a",
		ExpiresAt:       time.Now().Add(time.Hour).Unix(),
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no redirect when project is active)", rec.Code)
	}
}
