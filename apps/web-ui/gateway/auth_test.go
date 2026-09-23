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

// newAuthEcho mirrors main.go's route layout: the /api group is gated by
// requireClientKey, and POST /api/setup sits outside the group (no auth).
func newAuthEcho(f *fakeMemory, cfg Config) (*Server, *echo.Echo) {
	s := &Server{cfg: cfg, memory: f}
	e := echo.New()
	api := e.Group("/api", s.requireClientKey)
	api.GET("/health", s.health)
	e.POST("/api/setup", s.setupClient)
	return s, e
}

// --- requireClientKey ---

func TestRequireClientKeyNoHeader(t *testing.T) {
	_, e := newAuthEcho(&fakeMemory{}, Config{ClientAPIKey: "admin-secret"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestRequireClientKeyUnknownKey(t *testing.T) {
	_, e := newAuthEcho(&fakeMemory{}, Config{ClientAPIKey: "admin-secret"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("X-API-Key", strings.Repeat("0", 32))
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRequireClientKeyDevModeOpen(t *testing.T) {
	// No TOKEN_API_KEY configured (dev) → /api is open, so the browser UI
	// works without a per-device key.
	_, e := newAuthEcho(&fakeMemory{}, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (dev mode open)", rec.Code)
	}
}

func TestRequireClientKeyAdminKey(t *testing.T) {
	_, e := newAuthEcho(&fakeMemory{}, Config{ClientAPIKey: "admin-secret"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("X-API-Key", "admin-secret")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (admin key should pass)", rec.Code)
	}
}

func TestRequireClientKeyRejectsRetiredRegistryKey(t *testing.T) {
	// The 64-hex registry key path was retired with the scoped device
	// credential (#848): a bare registry key is no longer a valid client key.
	_, e := newAuthEcho(&fakeMemory{}, Config{ClientAPIKey: "admin-secret"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("X-API-Key", strings.Repeat("0", 64))
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (registry key retired)", rec.Code)
	}
}

// --- device credentials (web-device-credential) ---

// TestDeviceTokenLifecycle covers mint (server-side) → list → revoke → relist
// on the token store. Device credentials are project tokens carrying the
// device:api marker; the devices UI lists and revokes them via the shared
// project-token surface.
func TestDeviceTokenLifecycle(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}

	// empty store lists nothing
	devices, err := s.listDeviceTokens(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 0 {
		t.Fatalf("empty store should list nothing, got %+v", devices)
	}

	// two device credentials minted server-side
	if _, err := f.CreateDeviceToken(t.Context(), "device"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.CreateDeviceToken(t.Context(), "device"); err != nil {
		t.Fatal(err)
	}
	// a non-device programmatic token must NOT appear in the device list
	if _, err := f.CreateAPIToken(t.Context(), "programmatic", []string{"data:read"}); err != nil {
		t.Fatal(err)
	}

	devices, err = s.listDeviceTokens(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("want 2 device credentials (programmatic excluded), got %d: %+v", len(devices), devices)
	}
	for _, d := range devices {
		if d.CreatedAt == "" || d.ID == "" {
			t.Errorf("device %+v missing id/createdAt", d)
		}
	}

	// revoke one: list shrinks
	id := devices[0].ID
	if err := s.revokeDeviceToken(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	devices, _ = s.listDeviceTokens(t.Context())
	if len(devices) != 1 {
		t.Errorf("after revoke, want 1 device, got %d: %+v", len(devices), devices)
	}
	for _, d := range devices {
		if d.ID == id {
			t.Errorf("revoked device %s still listed", id)
		}
	}
}

// TestListDeviceTokensNewestFirst asserts deterministic newest-first ordering.
func TestListDeviceTokensNewestFirst(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	f.apiTokens = []APIToken{
		{ID: "aaa", Name: "device", Scopes: []string{"device:api", "agents:read", "data:read"}, CreatedAt: "2026-08-01T10:00:00Z"},
		{ID: "bbb", Name: "device", Scopes: []string{"device:api", "agents:read", "data:read"}, CreatedAt: "2026-08-02T10:00:00Z"},
		{ID: "ccc", Name: "device", Scopes: []string{"device:api", "agents:read", "data:read"}, CreatedAt: "2026-08-03T10:00:00Z"},
		{ID: "zzz", Name: "programmatic", Scopes: []string{"data:read"}, CreatedAt: "2026-08-04T10:00:00Z"},
	}
	devices, err := s.listDeviceTokens(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 3 {
		t.Fatalf("want 3 devices, got %+v", devices)
	}
	want := []string{"ccc", "bbb", "aaa"}
	for i, w := range want {
		if devices[i].ID != w {
			t.Errorf("position %d = %s, want %s", i, devices[i].ID, w)
		}
	}
}

// TestUIRevokeDevice exercises the settings revoke form (PRG → 303 redirect,
// credential revoked afterwards).
func TestUIRevokeDevice(t *testing.T) {
	f := &fakeMemory{}
	s, e := newAuthEcho(f, Config{})
	tok, err := f.CreateDeviceToken(t.Context(), "device")
	if err != nil {
		t.Fatal(err)
	}
	e.POST("/settings/devices/:id/revoke", s.uiRevokeDevice)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/devices/"+tok.ID+"/revoke", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings/devices?updated=1" {
		t.Errorf("redirect = %q, want /settings/devices?updated=1", loc)
	}
	if !f.apiTokens[fakeAPITokenFind(f.apiTokens, tok.ID)].IsRevoked {
		t.Error("device credential should be revoked after the handler runs")
	}
}

// --- setupClient ---

func TestSetupClientEmptyBody(t *testing.T) {
	_, e := newAuthEcho(&fakeMemory{}, Config{})
	for _, body := range []string{"", "{}", `{"token":""}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"error"`) {
			t.Errorf("body %q: response = %s", body, rec.Body.String())
		}
	}
}

func TestSetupClientInvalidJSON(t *testing.T) {
	_, e := newAuthEcho(&fakeMemory{}, Config{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`not json`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSetupClientUnknownToken(t *testing.T) {
	_, e := newAuthEcho(&fakeMemory{}, Config{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"token":"`+strings.Repeat("1", 32)+`"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestSetupClientSuccess(t *testing.T) {
	f := &fakeMemory{}
	s, e := newAuthEcho(f, Config{
		LiveKitPublicURL: "wss://lk.example.com",
		LiveKitURL:       "ws://internal:7880",
		DefaultAgent:     "memory",
		TTSProvider:      "cartesia",
	})
	token, err := s.mintSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"token":"`+token+`"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Host = "memory.example.com:8080"
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"serverURL":     "wss://lk.example.com",
		"tokenEndpoint": "http://memory.example.com:8080/api/token",
		"apiBaseURL":    "http://memory.example.com:8080/",
		"ttsStrategy":   "server",
	}
	if len(got) != 5 {
		t.Fatalf("setup response must have exactly 5 fields, got %d: %+v", len(got), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("setup %s = %q, want %q", k, got[k], v)
		}
	}
	apiKey := got["apiKey"]
	if !strings.HasPrefix(apiKey, "emt_") {
		t.Errorf("apiKey = %q, want an emt_* device credential", apiKey)
	}

	// the returned credential is a device credential minted server-side at QR
	// time; the setup flow stores it and returns it verbatim.
	if len(f.apiTokens) != 1 || !hasDeviceScope(f.apiTokens[0].Scopes) {
		t.Errorf("expected exactly one device credential minted, got %+v", f.apiTokens)
	}
}

func TestSetupClientTokenSingleUse(t *testing.T) {
	s, e := newAuthEcho(&fakeMemory{}, Config{})
	token, err := s.mintSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	post := func() int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"token":"`+token+`"}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		e.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(); code != http.StatusOK {
		t.Fatalf("first use = %d, want 200", code)
	}
	if code := post(); code != http.StatusUnauthorized {
		t.Fatalf("second use = %d, want 401 (single-use token)", code)
	}
}

func TestSetupClientServerURLFallback(t *testing.T) {
	s, e := newAuthEcho(&fakeMemory{}, Config{LiveKitURL: "ws://lk:7880", DefaultAgent: "diane"})
	token, err := s.mintSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"token":"`+token+`"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["serverURL"] != "ws://lk:7880" {
		t.Errorf("serverURL = %q, want LiveKitURL fallback", got["serverURL"])
	}
}

// TestSetupClientAcceptsDeviceManifest asserts the optional device manifest is
// persisted with the issued key: after a POST /api/setup carrying a device
// object, the registry entry holds the reported fields under "device".
// TestSetupClientAcceptsDeviceManifest asserts the optional device manifest is
// still accepted in the request body (back-compat with pre-credential iOS
// clients) and ignored — the credential was already minted server-side at QR
// time and is returned verbatim.
func TestSetupClientAcceptsDeviceManifest(t *testing.T) {
	f := &fakeMemory{}
	s, e := newAuthEcho(f, Config{})
	token, err := s.mintSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	body := `{"token":"` + token + `","device":{` +
		`"platform":"ios","formFactor":"phone","name":"John's iPhone",` +
		`"modelId":"iPhone15,2","modelDisplay":"iPhone 14 Pro",` +
		`"osName":"iOS","osVersion":"18.3.1","appVersion":"1.2.0","appBuild":"42"}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	key := got["apiKey"]
	if !strings.HasPrefix(key, "emt_") {
		t.Fatalf("apiKey = %q, want an emt_* device credential", key)
	}
	// exactly one device credential minted at QR time; the manifest is not a
	// separate registry entry.
	if len(f.apiTokens) != 1 {
		t.Errorf("want 1 device credential, got %+v", f.apiTokens)
	}
	if _, has := f.settings["ios_device_keys"]; has {
		t.Errorf("ios_device_keys registry must be retired: %+v", f.settings["ios_device_keys"])
	}
}

// TestSetupClientWithoutManifest asserts a POST /api/setup with no device
// object still returns the device credential (the manifest is optional and no
// longer persisted).
func TestSetupClientWithoutManifest(t *testing.T) {
	f := &fakeMemory{}
	s, e := newAuthEcho(f, Config{})
	token, err := s.mintSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"token":"`+token+`"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	key := got["apiKey"]
	if !strings.HasPrefix(key, "emt_") {
		t.Fatalf("apiKey = %q, want an emt_* device credential", key)
	}
}

// --- mintSetupToken / consumeSetupToken ---

func TestConsumeSetupTokenExpired(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	stale := strings.Repeat("a", 32)

	// token older than the TTL → rejected
	_ = f.SetProjectSetting(t.Context(), setupTokenCategory, setupTokenKey, map[string]any{
		"token":     stale,
		"createdAt": time.Now().Add(-11 * time.Minute).UTC().Format(time.RFC3339),
		"used":      false,
	})
	if _, ok := s.consumeSetupToken(t.Context(), stale); ok {
		t.Fatal("expired token must not be consumable")
	}

	// unparsable createdAt → rejected (treated as expired)
	_ = f.SetProjectSetting(t.Context(), setupTokenCategory, setupTokenKey, map[string]any{
		"token":     stale,
		"createdAt": "not-a-timestamp",
		"used":      false,
	})
	if _, ok := s.consumeSetupToken(t.Context(), stale); ok {
		t.Fatal("token with unparsable createdAt must not be consumable")
	}

	// empty createdAt → rejected
	_ = f.SetProjectSetting(t.Context(), setupTokenCategory, setupTokenKey, map[string]any{
		"token": stale,
		"used":  false,
	})
	if _, ok := s.consumeSetupToken(t.Context(), stale); ok {
		t.Fatal("token with missing createdAt must not be consumable")
	}
}

func TestConsumeSetupTokenFresh(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{}}
	token, err := s.mintSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.consumeSetupToken(t.Context(), token); !ok {
		t.Fatal("fresh minted token should be consumable")
	}
	if _, ok := s.consumeSetupToken(t.Context(), token); ok {
		t.Fatal("token must be single-use")
	}
	if _, ok := s.consumeSetupToken(t.Context(), strings.Repeat("0", 32)); ok {
		t.Fatal("unknown token must be rejected")
	}
}

// TestMintSetupTokenReusesValid asserts the stored unexpired token is reused
// (the QR stays stable across page reloads within the TTL), and that a fresh
// token is minted once the stored one is used.
func TestMintSetupTokenReusesValid(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{}}
	t1, err := s.mintSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t2, err := s.mintSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if t1 != t2 {
		t.Errorf("mintSetupToken should reuse the valid stored token, got %s vs %s", t1, t2)
	}
	if _, ok := s.consumeSetupToken(t.Context(), t1); !ok {
		t.Fatal("token should be consumable")
	}
	t3, err := s.mintSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if t3 == t1 {
		t.Error("mintSetupToken must mint a fresh token after the previous one is used")
	}
}

// TestSetupTokenSurvivesRestart proves tokens live in the memory backend, not
// the process: a NEW Server sharing the same fakeMemory consumes a token
// minted by the previous Server — the `air` hot-reload restart scenario.
func TestSetupTokenSurvivesRestart(t *testing.T) {
	f := &fakeMemory{}
	s1 := &Server{cfg: Config{}, memory: f}
	token, err := s1.mintSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	s2 := &Server{cfg: Config{}, memory: f} // "restarted" process, same backend
	if _, ok := s2.consumeSetupToken(t.Context(), token); !ok {
		t.Fatal("token minted before restart must survive and be consumable")
	}
	if _, ok := s2.consumeSetupToken(t.Context(), token); ok {
		t.Fatal("token must still be single-use after restart")
	}
}

// --- setup QR payload ---

func TestSetupQRPayloadShape(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{}}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "memory.example.com:8080"
	c := e.NewContext(req, httptest.NewRecorder())
	payload, err := s.setupQRPayload(c)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		SetupURL string `json:"setupURL"`
		Token    string `json:"token"`
	}
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatal(err)
	}
	if got.SetupURL != "http://memory.example.com:8080/api/setup" {
		t.Errorf("setupURL = %q, want http://memory.example.com:8080/api/setup", got.SetupURL)
	}
	if len(got.Token) != 64 {
		t.Errorf("token = %q, want 64 lowercase hex chars (randomHex(32))", got.Token)
	}
	// the token in the QR is a valid one-time setup token
	if _, ok := s.consumeSetupToken(t.Context(), got.Token); !ok {
		t.Error("QR token should be consumable exactly once")
	}
	if _, ok := s.consumeSetupToken(t.Context(), got.Token); ok {
		t.Error("QR token must be single-use")
	}
}

// --- publicBaseURL ---

func TestPublicBaseURLOverride(t *testing.T) {
	s := &Server{cfg: Config{PublicBaseURL: "https://gw.example.com"}, memory: &fakeMemory{}}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "internal:8095" // must be ignored when PUBLIC_BASE_URL is set
	c := e.NewContext(req, httptest.NewRecorder())

	if got := s.publicBaseURL(c); got != "https://gw.example.com" {
		t.Errorf("publicBaseURL = %q, want https://gw.example.com", got)
	}

	// setupQRPayload uses the pinned base
	payload, err := s.setupQRPayload(c)
	if err != nil {
		t.Fatal(err)
	}
	var qr struct {
		SetupURL string `json:"setupURL"`
	}
	if err := json.Unmarshal([]byte(payload), &qr); err != nil {
		t.Fatal(err)
	}
	if qr.SetupURL != "https://gw.example.com/api/setup" {
		t.Errorf("setupURL = %q, want https://gw.example.com/api/setup", qr.SetupURL)
	}
}

func TestPublicBaseURLTrimmed(t *testing.T) {
	s := &Server{cfg: Config{PublicBaseURL: "https://gw.example.com/"}, memory: &fakeMemory{}}
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())
	if got := s.publicBaseURL(c); got != "https://gw.example.com" {
		t.Errorf("publicBaseURL = %q, want trailing slash trimmed", got)
	}
}

// TestCookieSecure locks the Secure-flag decision: explicit PUBLIC_BASE_URL
// pins it, otherwise derive from the request scheme so plain-HTTP dev hosts
// drop Secure (the "missing oauth state" bug) while TLS proxies keep it.
func TestCookieSecure(t *testing.T) {
	e := echo.New()
	newCtx := func(headers map[string]string) echo.Context {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return e.NewContext(req, httptest.NewRecorder())
	}
	server := func(authMode, baseURL string) *Server {
		return &Server{cfg: Config{AuthMode: authMode, PublicBaseURL: baseURL}, memory: &fakeMemory{}}
	}

	// dev mode never sets Secure, regardless of base URL or scheme.
	c := newCtx(map[string]string{"X-Forwarded-Proto": "https"})
	if server("dev", "").cookieSecure(c) {
		t.Error("cookieSecure = true in dev mode, want false")
	}

	// Explicit http:// base URL disables Secure even behind a TLS proxy.
	c = newCtx(map[string]string{"X-Forwarded-Proto": "https"})
	if server("session", "http://gw.example.com").cookieSecure(c) {
		t.Error("cookieSecure = true for http:// PUBLIC_BASE_URL, want false")
	}

	// Explicit https:// base URL enables Secure.
	c = newCtx(nil)
	if !server("session", "https://gw.example.com").cookieSecure(c) {
		t.Error("cookieSecure = false for https:// PUBLIC_BASE_URL, want true")
	}

	// Empty base URL + plain HTTP request → false (regression guard).
	c = newCtx(nil)
	if server("session", "").cookieSecure(c) {
		t.Error("cookieSecure = true for empty PUBLIC_BASE_URL over HTTP, want false")
	}

	// Empty base URL + TLS-terminated proxy → true.
	c = newCtx(map[string]string{"X-Forwarded-Proto": "https"})
	if !server("session", "").cookieSecure(c) {
		t.Error("cookieSecure = false for empty PUBLIC_BASE_URL over HTTPS proxy, want true")
	}
}

// TestCanonicalHostRedirect locks the host-normalization middleware: a browser
// request on a non-canonical Host (e.g. the short Tailscale name) is 302'd to
// the PUBLIC_BASE_URL host, so host-only cookies match the pinned redirect_uri.
// Programmatic/asset paths and an unset PUBLIC_BASE_URL pass through untouched.
func TestCanonicalHostRedirect(t *testing.T) {
	e := echo.New()
	newReq := func(method, target, host string) (echo.Context, *httptest.ResponseRecorder) {
		req := httptest.NewRequest(method, target, nil)
		req.Host = host
		rec := httptest.NewRecorder()
		return e.NewContext(req, rec), rec
	}
	// next handler returns 200 "ok" to prove pass-through.
	next := func(c echo.Context) error { return c.String(http.StatusOK, "ok") }

	// Host mismatch on a browser path → 302 to canonical host, path+query kept.
	s := &Server{cfg: Config{PublicBaseURL: "http://alfred-dev.tail0358fa.ts.net:8095"}, memory: &fakeMemory{}}
	c, rec := newReq(http.MethodGet, "/settings?tab=voice", "alfred-dev:8095")
	if err := s.canonicalHostRedirect(next)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	want := "http://alfred-dev.tail0358fa.ts.net:8095/settings?tab=voice"
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}

	// Host already canonical → pass-through.
	c, rec = newReq(http.MethodGet, "/settings", "alfred-dev.tail0358fa.ts.net:8095")
	if err := s.canonicalHostRedirect(next)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 pass-through", rec.Code)
	}

	// API path with a mismatched host → no redirect (programmatic clients).
	c, rec = newReq(http.MethodGet, "/api/health", "alfred-dev:8095")
	if err := s.canonicalHostRedirect(next)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 pass-through for /api", rec.Code)
	}

	// Unset PUBLIC_BASE_URL → no redirect regardless of host.
	s = &Server{cfg: Config{}, memory: &fakeMemory{}}
	c, rec = newReq(http.MethodGet, "/settings", "alfred-dev:8095")
	if err := s.canonicalHostRedirect(next)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 pass-through with empty PUBLIC_BASE_URL", rec.Code)
	}
}

// TestSetupClientPublicBaseURL asserts the setup response URLs use the pinned
// PUBLIC_BASE_URL (host header ignored), with no double trailing slash.
func TestSetupClientPublicBaseURL(t *testing.T) {
	s, e := newAuthEcho(&fakeMemory{}, Config{
		PublicBaseURL: "https://gw.example.com/", // trailing slash must be trimmed
		LiveKitURL:    "ws://lk:7880",
	})
	token, err := s.mintSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"token":"`+token+`"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Host = "internal:8095"
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["tokenEndpoint"] != "https://gw.example.com/api/token" {
		t.Errorf("tokenEndpoint = %q, want https://gw.example.com/api/token", got["tokenEndpoint"])
	}
	if got["apiBaseURL"] != "https://gw.example.com/" {
		t.Errorf("apiBaseURL = %q, want https://gw.example.com/", got["apiBaseURL"])
	}
}

// --- ttsStrategy ---

func TestTTSStrategy(t *testing.T) {
	cases := []struct {
		provider string
		want     string
	}{
		{"", "client"}, // unset in tests / empty config
		{"cartesia", "server"},
		{"CARTESIA", "server"}, // lowercased before compare
		{"none", "client"},
		{"client", "client"},
		{" client ", "client"}, // trimmed before compare
		{"other", "server"},
	}
	for _, c := range cases {
		s := &Server{cfg: Config{TTSProvider: c.provider}}
		if got := s.ttsStrategy(); got != c.want {
			t.Errorf("ttsStrategy(%q) = %q, want %q", c.provider, got, c.want)
		}
	}
}

// TestSetupClientTTSStrategy asserts the setup response carries ttsStrategy.
func TestSetupClientTTSStrategy(t *testing.T) {
	s, e := newAuthEcho(&fakeMemory{}, Config{TTSProvider: "none", LiveKitURL: "ws://lk:7880"})
	token, err := s.mintSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"token":"`+token+`"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["ttsStrategy"] != "client" {
		t.Errorf("ttsStrategy = %q, want client for TTS_PROVIDER=none", got["ttsStrategy"])
	}
}
