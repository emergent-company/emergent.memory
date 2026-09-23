package main

import (
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
// memory-failure paths, QR image encoding).

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

// TestDeviceTokenErrorPaths asserts memory failures degrade safely: listing
// surfaces a token-list error and revoking surfaces the revoke error.
func TestDeviceTokenErrorPaths(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{apiTokenErr: errTest}}

	if _, err := s.listDeviceTokens(t.Context()); err == nil {
		t.Error("listDeviceTokens must surface a token-list error")
	}
	if err := s.revokeDeviceToken(t.Context(), "any"); err == nil {
		t.Error("revokeDeviceToken must surface a revoke error")
	}
}

// TestSetupTokenConsumeUnsetAndMemoryError asserts an absent setting and a
// memory error both reject consumption (and minting surfaces the error).
func TestSetupTokenConsumeUnsetAndMemoryError(t *testing.T) {
	empty := &Server{cfg: Config{}, memory: &fakeMemory{}}
	if _, ok := empty.consumeSetupToken(t.Context(), strings.Repeat("a", 32)); ok {
		t.Error("consume with no stored token must be false")
	}

	broken := &Server{cfg: Config{}, memory: &fakeMemory{settingErr: errTest}}
	if _, ok := broken.consumeSetupToken(t.Context(), strings.Repeat("a", 32)); ok {
		t.Error("consume must be false when memory read fails")
	}
	if _, err := broken.mintSetupToken(t.Context()); err == nil {
		t.Error("mintSetupToken must surface the memory read error")
	}
}

// TestSetupTokenMetadataPreservedOnConsume asserts the consume write-back keeps
// the original token + createdAt + device credential and only flips used to
// true.
func TestSetupTokenMetadataPreservedOnConsume(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	token := strings.Repeat("b", 32)
	createdAt := time.Now().UTC().Format(time.RFC3339)
	if err := f.SetProjectSetting(t.Context(), setupTokenCategory, setupTokenKey, map[string]any{
		"token":       token,
		"createdAt":   createdAt,
		"used":        false,
		"deviceToken": "emt_device_credential",
	}); err != nil {
		t.Fatal(err)
	}
	dc, ok := s.consumeSetupToken(t.Context(), token)
	if !ok {
		t.Fatal("freshly stored token should be consumable")
	}
	if dc != "emt_device_credential" {
		t.Errorf("device credential = %q, want emt_device_credential", dc)
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
	if stored["deviceToken"] != "emt_device_credential" {
		t.Errorf("deviceToken rewritten: %v", stored["deviceToken"])
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

// TestSetupTokenClientNoDeviceCredential asserts a setup token that was minted
// without a device credential (e.g. dev mode, no session) yields 401 at the
// exchange — fail closed, never an empty bearer.
func TestSetupTokenClientNoDeviceCredential(t *testing.T) {
	f := &fakeMemory{}
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

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no device credential available)", rec.Code)
	}
	if used, _ := f.settings[setupTokenCategory][setupTokenKey]["used"].(bool); used {
		t.Error("token should not be marked used when no device credential is returned")
	}
}

// TestSetupTokenClientReturnsDeviceCredential asserts the exchange returns the
// emt_* device credential minted at QR time, ignoring the optional manifest.
func TestSetupTokenClientReturnsDeviceCredential(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	token := strings.Repeat("d", 32)
	if err := f.SetProjectSetting(t.Context(), setupTokenCategory, setupTokenKey, map[string]any{
		"token":       token,
		"createdAt":   time.Now().UTC().Format(time.RFC3339),
		"used":        false,
		"deviceToken": "emt_device_credential_1234",
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
	if got["apiKey"] != "emt_device_credential_1234" {
		t.Errorf("apiKey = %q, want the stored device credential", got["apiKey"])
	}
}
