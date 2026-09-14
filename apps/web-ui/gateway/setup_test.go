package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// This file complements auth_test.go: auth_test.go covers the setup lifecycle
// end-to-end, this file pins the lower-level edge cases (freshness boundaries,
// manifest mapping, memory-failure paths, QR image encoding).

// setupFailMemory wraps fakeMemory and fails SetProjectSetting for one chosen
// category only, so a lifecycle can succeed halfway and fail at a chosen step.
type setupFailMemory struct {
	*fakeMemory
	failSetCategory string
}

func (f *setupFailMemory) SetProjectSetting(ctx context.Context, category, key string, value map[string]any) error {
	if category == f.failSetCategory {
		return errTest
	}
	return f.fakeMemory.SetProjectSetting(ctx, category, key, value)
}

// TestSetupTokenFreshBoundary pins the TTL decision, including that empty and
// unparsable timestamps are expired and a future timestamp is fresh.
func TestSetupTokenFreshBoundary(t *testing.T) {
	cases := []struct {
		name      string
		createdAt string
		want      bool
	}{
		{"empty", "", false},
		{"unparsable", "not-a-timestamp", false},
		{"RFC3339 with space", time.Now().UTC().Format("2006-01-02 15:04:05"), false},
		{"just now", time.Now().UTC().Format(time.RFC3339), true},
		{"a minute old", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), true},
		{"eleven minutes old", time.Now().Add(-11 * time.Minute).UTC().Format(time.RFC3339), false},
		{"future", time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := setupTokenFresh(tc.createdAt); got != tc.want {
				t.Errorf("setupTokenFresh(%q) = %v, want %v", tc.createdAt, got, tc.want)
			}
		})
	}
}

// TestDeviceKeyManifestToMap asserts only non-empty fields are persisted, under
// their camelCase JSON keys, and that an empty manifest yields an empty map.
func TestDeviceKeyManifestToMap(t *testing.T) {
	if got := (deviceManifest{}).toMap(); len(got) != 0 {
		t.Errorf("empty manifest should map to no fields, got %+v", got)
	}
	full := deviceManifest{
		Platform:     "ios",
		FormFactor:   "phone",
		Name:         "John's iPhone",
		ModelID:      "iPhone15,2",
		ModelDisplay: "iPhone 14 Pro",
		OSName:       "iOS",
		OSVersion:    "18.3.1",
		AppVersion:   "1.2.0",
		AppBuild:     "42",
	}
	got := full.toMap()
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
	if len(got) != len(want) {
		t.Fatalf("got %d fields %+v, want %d", len(got), got, len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("toMap[%q] = %v, want %q", k, got[k], v)
		}
	}

	// whitespace-only values are non-empty strings and therefore persisted.
	partial := deviceManifest{Name: "  ", Platform: "macos"}
	pm := partial.toMap()
	if pm["name"] != "  " {
		t.Errorf("whitespace-only value should be persisted as-is, got %v", pm["name"])
	}
	if pm["platform"] != "macos" {
		t.Errorf("platform = %v, want macos", pm["platform"])
	}
	if _, present := pm["modelId"]; present {
		t.Errorf("unset field must not be persisted: %+v", pm)
	}
}

// TestDeviceKeyRegistryErrorPaths asserts memory failures degrade safely:
// reads report the error/lookup-miss, writes propagate it.
func TestDeviceKeyRegistryErrorPaths(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{settingErr: errTest}}

	if devs, err := s.deviceRegistry(t.Context()); err == nil || devs != nil {
		t.Errorf("deviceRegistry on error = (%v, %v), want (nil, err)", devs, err)
	}
	if s.deviceKeyValid(t.Context(), "any") {
		t.Error("deviceKeyValid must be false when the registry read fails")
	}
	if _, err := s.listDeviceKeys(t.Context()); err == nil {
		t.Error("listDeviceKeys must surface a registry read error")
	}
	if err := s.revokeDeviceKey(t.Context(), "any"); err == nil {
		t.Error("revokeDeviceKey must surface a registry read error")
	}
	if _, err := s.issueDeviceKey(t.Context(), nil); err == nil {
		t.Error("issueDeviceKey must surface a registry read error")
	}
}

// TestSetupTokenConsumeUnsetAndMemoryError asserts an absent setting and a
// memory error both reject consumption (and minting surfaces the error).
func TestSetupTokenConsumeUnsetAndMemoryError(t *testing.T) {
	empty := &Server{cfg: Config{}, memory: &fakeMemory{}}
	if empty.consumeSetupToken(t.Context(), strings.Repeat("a", 32)) {
		t.Error("consume with no stored token must be false")
	}

	broken := &Server{cfg: Config{}, memory: &fakeMemory{settingErr: errTest}}
	if broken.consumeSetupToken(t.Context(), strings.Repeat("a", 32)) {
		t.Error("consume must be false when memory read fails")
	}
	if _, err := broken.mintSetupToken(t.Context()); err == nil {
		t.Error("mintSetupToken must surface the memory read error")
	}
}

// TestSetupTokenMetadataPreservedOnConsume asserts the consume write-back keeps
// the original token + createdAt and only flips used to true.
func TestSetupTokenMetadataPreservedOnConsume(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	token := strings.Repeat("b", 32)
	createdAt := time.Now().UTC().Format(time.RFC3339)
	if err := f.SetProjectSetting(t.Context(), setupTokenCategory, setupTokenKey, map[string]any{
		"token":     token,
		"createdAt": createdAt,
		"used":      false,
	}); err != nil {
		t.Fatal(err)
	}
	if !s.consumeSetupToken(t.Context(), token) {
		t.Fatal("freshly stored token should be consumable")
	}
	stored := f.settings[setupTokenCategory][setupTokenKey]
	if stored["token"] != token {
		t.Errorf("token rewritten: %v", stored["token"])
	}
	if stored["createdAt"] != createdAt {
		t.Errorf("createdAt rewritten: %v", stored["createdAt"])
	}
	if used, _ := stored["used"].(bool); !used {
		t.Errorf("used flag not set after consume: %v", stored["used"])
	}
}

// TestSetupTokenQRImage asserts setupQRImage produces a decodable PNG data URI
// and that the embedded token is a single-use setup token.
func TestSetupTokenQRImage(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "memory.example.com:8080"
	c := e.NewContext(req, httptest.NewRecorder())

	img := s.setupQRImage(c)
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(img, prefix) {
		t.Fatalf("image = %q, want %q prefix", img, prefix)
	}
	png, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(img, prefix))
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if !strings.HasPrefix(string(png), "\x89PNG\r\n\x1a\n") {
		t.Errorf("decoded bytes are not a PNG (len=%d)", len(png))
	}
}

// TestSetupTokenClientIssueMemoryError asserts a device-key write failure
// after a valid token exchange yields 502 and does not register a device.
func TestSetupTokenClientIssueMemoryError(t *testing.T) {
	f := &setupFailMemory{fakeMemory: &fakeMemory{}, failSetCategory: deviceKeyRegistryCategory}
	s := &Server{cfg: Config{}, memory: f}
	token := strings.Repeat("c", 32)
	if err := f.SetProjectSetting(t.Context(), setupTokenCategory, setupTokenKey, map[string]any{
		"token":     token,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
		"used":      false,
	}); err != nil {
		t.Fatal(err)
	}

	e := echo.New()
	e.POST("/api/setup", s.setupClient)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"token":"`+token+`"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "memory service unavailable") {
		t.Errorf("body = %s, want memory-service error", rec.Body.String())
	}
	// the token was consumed before the failure, and no registry entry remains.
	if used, _ := f.settings[setupTokenCategory][setupTokenKey]["used"].(bool); !used {
		t.Error("token should have been marked used before the device-key write")
	}
	if _, ok := f.settings[deviceKeyRegistryCategory][deviceKeyRegistryKey]; ok {
		t.Error("no registry entry should exist after the failed write")
	}
}

// TestSetupTokenClientAcceptsSerializedDevice verifies the handler binds the
// optional device manifest JSON into the issued registry entry.
func TestSetupTokenClientAcceptsSerializedDevice(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	token := strings.Repeat("d", 32)
	if err := f.SetProjectSetting(t.Context(), setupTokenCategory, setupTokenKey, map[string]any{
		"token":     token,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
		"used":      false,
	}); err != nil {
		t.Fatal(err)
	}

	e := echo.New()
	e.POST("/api/setup", s.setupClient)
	rec := httptest.NewRecorder()
	body := `{"token":"` + token + `","device":{"platform":"ios","name":"Phone"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	entry, ok := f.settings[deviceKeyRegistryCategory][deviceKeyRegistryKey]["devices"].(map[string]any)[got["apiKey"]].(map[string]any)
	if !ok {
		t.Fatalf("issued key not registered: %+v", f.settings)
	}
	dev, ok := entry["device"].(map[string]any)
	if !ok || dev["platform"] != "ios" || dev["name"] != "Phone" {
		t.Errorf("device manifest not persisted: %+v", entry)
	}
}
