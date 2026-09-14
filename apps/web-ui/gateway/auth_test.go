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

func TestRequireClientKeyDeviceKey(t *testing.T) {
	f := &fakeMemory{}
	s, e := newAuthEcho(f, Config{})
	key, err := s.issueDeviceKey(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("X-API-Key", key)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (registered device key should pass)", rec.Code)
	}
	if len(f.settingWrites) != 1 || f.settingWrites[0].Category != "ios_device_keys" || f.settingWrites[0].Key != "registry" {
		t.Errorf("device key not persisted to ios_device_keys/registry: %+v", f.settingWrites)
	}
}

// --- device-key registry ---

// TestDeviceKeyRegistryShape asserts the single-registry storage shape:
// ios_device_keys/registry = {"devices": {"<key>": {"createdAt": "<RFC3339>",
// "device": {<manifest>}}}}. With a manifest supplied the entry carries the
// non-empty manifest fields under "device"; without one the entry is exactly
// {"createdAt": ...}.
func TestDeviceKeyRegistryShape(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	key, err := s.issueDeviceKey(t.Context(), &deviceManifest{
		Platform:   "ios",
		Name:       "John's iPhone",
		ModelID:    "iPhone15,2",
		OSName:     "iOS",
		OSVersion:  "18.3.1",
		AppVersion: "1.2.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := f.settings["ios_device_keys"]["registry"]
	if reg == nil {
		t.Fatal("registry setting missing after issueDeviceKey")
	}
	devices, ok := reg["devices"].(map[string]any)
	if !ok {
		t.Fatalf("registry devices = %v (%T), want map[string]any", reg["devices"], reg["devices"])
	}
	entry, ok := devices[key].(map[string]any)
	if !ok {
		t.Fatalf("device entry = %v (%T), want map[string]any", devices[key], devices[key])
	}
	createdAt, ok := entry["createdAt"].(string)
	if !ok || createdAt == "" {
		t.Errorf("device entry createdAt = %v, want RFC3339 string", entry["createdAt"])
	}
	dev, ok := entry["device"].(map[string]any)
	if !ok {
		t.Fatalf("device entry device = %v (%T), want map[string]any", entry["device"], entry["device"])
	}
	want := map[string]string{
		"platform":   "ios",
		"name":       "John's iPhone",
		"modelId":    "iPhone15,2",
		"osName":     "iOS",
		"osVersion":  "18.3.1",
		"appVersion": "1.2.0",
	}
	for k, v := range want {
		if gotV, ok := dev[k].(string); !ok || gotV != v {
			t.Errorf("device[%q] = %v (%T), want %q", k, dev[k], dev[k], v)
		}
	}
	if _, present := dev["appBuild"]; present {
		t.Errorf("empty manifest fields must not be persisted, got %+v", dev)
	}
}

// TestDeviceKeyRegistryLifecycle covers issue → valid → list → revoke → invalid
// on the registry storage.
func TestDeviceKeyRegistryLifecycle(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}

	// empty registry lists nothing
	devices, err := s.listDeviceKeys(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 0 {
		t.Fatalf("empty registry should list nothing, got %+v", devices)
	}

	k1, err := s.issueDeviceKey(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := s.issueDeviceKey(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if !s.deviceKeyValid(t.Context(), k1) || !s.deviceKeyValid(t.Context(), k2) {
		t.Fatal("issued keys should be valid via the registry")
	}
	if s.deviceKeyValid(t.Context(), strings.Repeat("0", 64)) {
		t.Fatal("unregistered key must be invalid")
	}

	// both listed, createdAt populated
	devices, err = s.listDeviceKeys(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("want 2 devices, got %d: %+v", len(devices), devices)
	}
	for _, d := range devices {
		if d.CreatedAt == "" {
			t.Errorf("device %s has empty createdAt", d.Key)
		}
	}

	// revoke k1: k1 invalid, k2 unaffected, list shrinks
	if err := s.revokeDeviceKey(t.Context(), k1); err != nil {
		t.Fatal(err)
	}
	if s.deviceKeyValid(t.Context(), k1) {
		t.Error("revoked key must be invalid")
	}
	if !s.deviceKeyValid(t.Context(), k2) {
		t.Error("revoking one key must not affect others")
	}
	devices, _ = s.listDeviceKeys(t.Context())
	if len(devices) != 1 || devices[0].Key != k2 {
		t.Errorf("after revoke, want only %s, got %+v", k2, devices)
	}

	// revoking an absent key is a no-op
	if err := s.revokeDeviceKey(t.Context(), k1); err != nil {
		t.Fatalf("re-revoke should be a no-op, got %v", err)
	}
	devices, _ = s.listDeviceKeys(t.Context())
	if len(devices) != 1 {
		t.Errorf("no-op revoke should not change the registry, got %+v", devices)
	}
}

// TestListDeviceKeysPopulatesMetadata asserts listDeviceKeys reads the stored
// "device" manifest back into the device struct: an entry with a manifest
// yields populated metadata fields, a legacy entry (no "device") yields empty
// metadata fields while keeping Key/CreatedAt.
func TestListDeviceKeysPopulatesMetadata(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	if err := f.SetProjectSetting(t.Context(), "ios_device_keys", "registry", map[string]any{
		"devices": map[string]any{
			// modern entry: self-reported manifest
			"meta": map[string]any{
				"createdAt": "2026-08-02T10:00:00Z",
				"device": map[string]any{
					"platform":     "macos",
					"name":         "MacBook Pro",
					"modelId":      "Mac14,2",
					"modelDisplay": "MacBook Pro (M2)",
					"osName":       "macOS",
					"osVersion":    "15.0",
					"appVersion":   "1.2.0",
				},
			},
			// legacy entry: no device field at all
			"legacy": map[string]any{
				"createdAt": "2026-08-01T10:00:00Z",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	devices, err := s.listDeviceKeys(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("want 2 devices, got %+v", devices)
	}

	var meta, legacy *device
	for i := range devices {
		switch devices[i].Key {
		case "meta":
			meta = &devices[i]
		case "legacy":
			legacy = &devices[i]
		}
	}
	if meta == nil || legacy == nil {
		t.Fatalf("both seeded devices must be listed, got %+v", devices)
	}

	// modern entry: all reported fields surfaced
	fields := []struct {
		name string
		get  func(d device) string
	}{
		{"Platform", func(d device) string { return d.Platform }},
		{"Name", func(d device) string { return d.Name }},
		{"ModelID", func(d device) string { return d.ModelID }},
		{"ModelDisplay", func(d device) string { return d.ModelDisplay }},
		{"OSName", func(d device) string { return d.OSName }},
		{"OSVersion", func(d device) string { return d.OSVersion }},
	}
	wantMeta := map[string]string{
		"Platform":     "macos",
		"Name":         "MacBook Pro",
		"ModelID":      "Mac14,2",
		"ModelDisplay": "MacBook Pro (M2)",
		"OSName":       "macOS",
		"OSVersion":    "15.0",
	}
	for _, f := range fields {
		if got := f.get(*meta); got != wantMeta[f.name] {
			t.Errorf("meta device %s = %q, want %q", f.name, got, wantMeta[f.name])
		}
	}
	if meta.CreatedAt != "2026-08-02T10:00:00Z" {
		t.Errorf("meta device createdAt = %q, want seeded value", meta.CreatedAt)
	}

	// legacy entry: key + createdAt kept, metadata empty
	if legacy.CreatedAt != "2026-08-01T10:00:00Z" {
		t.Errorf("legacy device createdAt = %q, want seeded value", legacy.CreatedAt)
	}
	for _, f := range fields {
		if got := f.get(*legacy); got != "" {
			t.Errorf("legacy device %s = %q, want empty", f.name, got)
		}
	}
}

// TestListDeviceKeysNewestFirst asserts deterministic newest-first ordering
// (registry seeded directly with distinct timestamps).
func TestListDeviceKeysNewestFirst(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	if err := f.SetProjectSetting(t.Context(), "ios_device_keys", "registry", map[string]any{
		"devices": map[string]any{
			"aaa": map[string]any{"createdAt": "2026-08-01T10:00:00Z"},
			"bbb": map[string]any{"createdAt": "2026-08-02T10:00:00Z"},
			"ccc": map[string]any{"createdAt": "2026-08-03T10:00:00Z"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	devices, err := s.listDeviceKeys(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 3 {
		t.Fatalf("want 3 devices, got %+v", devices)
	}
	want := []string{"ccc", "bbb", "aaa"}
	for i, w := range want {
		if devices[i].Key != w {
			t.Errorf("position %d = %s, want %s", i, devices[i].Key, w)
		}
	}
}

// TestUIRevokeDevice exercises the settings revoke form (PRG → 303 redirect,
// key no longer valid afterwards).
func TestUIRevokeDevice(t *testing.T) {
	f := &fakeMemory{}
	s, e := newAuthEcho(f, Config{})
	key, err := s.issueDeviceKey(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	e.POST("/settings/devices/:key/revoke", s.uiRevokeDevice)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/devices/"+key+"/revoke", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings/devices?updated=1" {
		t.Errorf("redirect = %q, want /settings/devices?updated=1", loc)
	}
	if s.deviceKeyValid(t.Context(), key) {
		t.Error("device key should be revoked after the handler runs")
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
	s, e := newAuthEcho(&fakeMemory{}, Config{
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
	if len(apiKey) != 64 {
		t.Errorf("apiKey = %q, want 64 lowercase hex chars (randomHex(32))", apiKey)
	}
	if strings.Trim(apiKey, "0123456789abcdef") != "" {
		t.Errorf("apiKey = %q, want hex only", apiKey)
	}

	// the returned key is a registered device key: it passes requireClientKey
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req2.Header.Set("X-API-Key", apiKey)
	e.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("issued key should authenticate /api/health, got %d", rec2.Code)
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
	if key == "" {
		t.Fatal("setup response missing apiKey")
	}

	// the issued key's registry entry carries the manifest under "device"
	reg := f.settings["ios_device_keys"]["registry"]
	if reg == nil {
		t.Fatal("registry setting missing after setup")
	}
	devices, _ := reg["devices"].(map[string]any)
	entry, ok := devices[key].(map[string]any)
	if !ok {
		t.Fatalf("device entry = %v (%T), want map[string]any", devices[key], devices[key])
	}
	dev, ok := entry["device"].(map[string]any)
	if !ok {
		t.Fatalf("entry device = %v (%T), want map[string]any", entry["device"], entry["device"])
	}
	want := map[string]string{
		"platform":     "ios",
		"formFactor":   "phone",
		"name":         "John's iPhone",
		"modelId":      "iPhone15,2",
		"modelDisplay": "iPhone 14 Pro",
		"osName":       "iOS",
		"osVersion":    "18.3.1",
		"appVersion":   "1.2.0",
		"appBuild":     "42",
	}
	for k, v := range want {
		if gotV, ok := dev[k].(string); !ok || gotV != v {
			t.Errorf("device[%q] = %v (%T), want %q", k, dev[k], dev[k], v)
		}
	}
	if _, ok := entry["createdAt"].(string); !ok {
		t.Errorf("device entry createdAt = %v, want RFC3339 string", entry["createdAt"])
	}
}

// TestSetupClientWithoutManifest asserts a legacy POST /api/setup with no
// device object still issues a key and records the registration WITHOUT a
// device field (backward compatible with pre-manifest clients).
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
	if key == "" {
		t.Fatal("setup without a manifest must still issue a key")
	}

	// the entry is registered with the legacy shape: no device field
	reg := f.settings["ios_device_keys"]["registry"]
	if reg == nil {
		t.Fatal("registry setting missing after setup")
	}
	devices, _ := reg["devices"].(map[string]any)
	entry, ok := devices[key].(map[string]any)
	if !ok {
		t.Fatalf("device entry = %v (%T), want map[string]any", devices[key], devices[key])
	}
	if _, present := entry["device"]; present {
		t.Errorf("registry entry must have no device field without a manifest, got %+v", entry)
	}
	if _, ok := entry["createdAt"].(string); !ok {
		t.Errorf("device entry createdAt = %v, want RFC3339 string", entry["createdAt"])
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
	if s.consumeSetupToken(t.Context(), stale) {
		t.Fatal("expired token must not be consumable")
	}

	// unparsable createdAt → rejected (treated as expired)
	_ = f.SetProjectSetting(t.Context(), setupTokenCategory, setupTokenKey, map[string]any{
		"token":     stale,
		"createdAt": "not-a-timestamp",
		"used":      false,
	})
	if s.consumeSetupToken(t.Context(), stale) {
		t.Fatal("token with unparsable createdAt must not be consumable")
	}

	// empty createdAt → rejected
	_ = f.SetProjectSetting(t.Context(), setupTokenCategory, setupTokenKey, map[string]any{
		"token": stale,
		"used":  false,
	})
	if s.consumeSetupToken(t.Context(), stale) {
		t.Fatal("token with missing createdAt must not be consumable")
	}
}

func TestConsumeSetupTokenFresh(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{}}
	token, err := s.mintSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !s.consumeSetupToken(t.Context(), token) {
		t.Fatal("fresh minted token should be consumable")
	}
	if s.consumeSetupToken(t.Context(), token) {
		t.Fatal("token must be single-use")
	}
	if s.consumeSetupToken(t.Context(), strings.Repeat("0", 32)) {
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
	if !s.consumeSetupToken(t.Context(), t1) {
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
	if !s2.consumeSetupToken(t.Context(), token) {
		t.Fatal("token minted before restart must survive and be consumable")
	}
	if s2.consumeSetupToken(t.Context(), token) {
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
	if !s.consumeSetupToken(t.Context(), got.Token) {
		t.Error("QR token should be consumable exactly once")
	}
	if s.consumeSetupToken(t.Context(), got.Token) {
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
