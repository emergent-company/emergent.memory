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
// end-to-end, this file pins the lower-level store/consume edge cases
// (freshness boundaries, rejection paths, QR image encoding).

// TestSetupTokenFreshBoundary pins the TTL decision: a zero timestamp is
// expired, a just-now / future timestamp is fresh, and one past the TTL is not.
func TestSetupTokenFreshBoundary(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name      string
		createdAt time.Time
		want      bool
	}{
		{"zero", time.Time{}, false},
		{"just now", now, true},
		{"a minute old", now.Add(-time.Minute), true},
		{"eleven minutes old", now.Add(-11 * time.Minute), false},
		{"future", now.Add(5 * time.Minute), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := setupTokenFresh(tc.createdAt); got != tc.want {
				t.Errorf("setupTokenFresh(%v) = %v, want %v", tc.createdAt, got, tc.want)
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

// TestConsumeSetupTokenRejects asserts unknown, missing-device-credential,
// expired, and already-used tokens all reject — and that consuming is a
// local-only lookup (no memory call).
func TestConsumeSetupTokenRejects(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{}, setupTokens: newSetupTokenStore()}

	// unknown token
	if _, _, _, _, ok := s.consumeSetupToken(strings.Repeat("a", 32)); ok {
		t.Error("unknown token must be rejected")
	}

	// missing device credential
	s.setupTokens.set(setupEntry{Token: "tok-nodev", CreatedAt: time.Now()})
	if _, _, _, _, ok := s.consumeSetupToken("tok-nodev"); ok {
		t.Error("token without a device credential must be rejected")
	}

	// expired
	s.setupTokens.set(setupEntry{
		Token:       "tok-old",
		DeviceToken: "emt_old",
		CreatedAt:   time.Now().Add(-11 * time.Minute),
	})
	if _, _, _, _, ok := s.consumeSetupToken("tok-old"); ok {
		t.Error("expired token must be rejected")
	}

	// used: consume once, then a second consume must fail
	s.setupTokens.set(setupEntry{Token: "tok-used", DeviceToken: "emt_used", CreatedAt: time.Now()})
	if _, _, _, _, ok := s.consumeSetupToken("tok-used"); !ok {
		t.Fatal("fresh token should be consumable")
	}
	if _, _, _, _, ok := s.consumeSetupToken("tok-used"); ok {
		t.Error("used token must be rejected")
	}
}

// TestConsumeSetupTokenReturnsIdentity asserts consume returns the device
// credential and the stored onboarding project identity, and marks the token used.
func TestConsumeSetupTokenReturnsIdentity(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{}, setupTokens: newSetupTokenStore()}
	s.setupTokens.set(setupEntry{
		Token:       "tok-id",
		OrgID:       "org-1",
		ProjectID:   "proj-1",
		ProjectName: "Project One",
		DeviceToken: "emt_device_credential",
		CreatedAt:   time.Now(),
	})
	deviceToken, orgID, projectID, projectName, ok := s.consumeSetupToken("tok-id")
	if !ok {
		t.Fatal("seeded token should be consumable")
	}
	if deviceToken != "emt_device_credential" {
		t.Errorf("device credential = %q, want emt_device_credential", deviceToken)
	}
	if orgID != "org-1" || projectID != "proj-1" || projectName != "Project One" {
		t.Errorf("identity = %q/%q/%q, want org-1/proj-1/Project One", orgID, projectID, projectName)
	}
	if e, _ := s.setupTokens.get("tok-id"); !e.Used {
		t.Error("token should be marked used after consume")
	}
}

// TestSetupTokenQRImage asserts setupQRImage produces a decodable PNG data URI.
func TestSetupTokenQRImage(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{}}
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

// TestSetupQRPayloadFailsClosedWithoutProjectScope asserts that in session mode
// a devices-page request with no resolved project does not mint a QR (fail
// closed, per the "Setup QR carries the onboarding project" requirement).
func TestSetupQRPayloadFailsClosedWithoutProjectScope(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "session"}, memory: &fakeMemory{}}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "memory.example.com:8080"
	c := e.NewContext(req, httptest.NewRecorder())
	if img := s.setupQRImage(c); img != "" {
		t.Errorf("session-mode QR without a resolved project = %q, want empty", img)
	}
}

// TestSetupTokenClientNoDeviceCredential asserts a setup token minted without a
// device credential (e.g. dev mode, no session) yields 401 at the exchange —
// fail closed, never an empty bearer — and is not marked used.
func TestSetupTokenClientNoDeviceCredential(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{}, setupTokens: newSetupTokenStore()}
	token := strings.Repeat("c", 32)
	s.setupTokens.set(setupEntry{Token: token, CreatedAt: time.Now()})

	e := echo.New()
	e.POST("/api/setup", s.setupClient)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"token":"`+token+`"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no device credential available)", rec.Code)
	}
	if e2, _ := s.setupTokens.get(token); e2.Used {
		t.Error("token should not be marked used when no device credential is returned")
	}
}

// TestSetupTokenClientReturnsDeviceCredential asserts the exchange returns the
// emt_* device credential minted at QR time plus the onboarding project
// identity, ignoring the optional manifest.
func TestSetupTokenClientReturnsDeviceCredential(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{}, setupTokens: newSetupTokenStore()}
	token := strings.Repeat("d", 32)
	s.setupTokens.set(setupEntry{
		Token:       token,
		OrgID:       "org-9",
		ProjectID:   "proj-9",
		ProjectName: "Project Nine",
		DeviceToken: "emt_device_credential_1234",
		CreatedAt:   time.Now(),
	})

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
	if got["orgID"] != "org-9" || got["projectID"] != "proj-9" || got["projectName"] != "Project Nine" {
		t.Errorf("identity = %q/%q/%q, want org-9/proj-9/Project Nine", got["orgID"], got["projectID"], got["projectName"])
	}
}

// TestMintSetupTokenDoesNotRevokeClaimedCredential guards against the reload
// regression: once a device claims its credential (consumeSetupToken flips
// used=true), a subsequent page reload (mintSetupToken rotation) must NOT revoke
// the now-live credential — only an unclaimed orphan is revoked on rotation.
func TestMintSetupTokenDoesNotRevokeClaimedCredential(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}

	token, err := s.mintSetupToken(t.Context(), "org-1", "proj-1", "Project One")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.apiTokens) != 1 {
		t.Fatalf("want 1 device credential after first mint, got %+v", f.apiTokens)
	}
	claimedID := f.apiTokens[0].ID

	if _, _, _, _, ok := s.consumeSetupToken(token); !ok {
		t.Fatal("freshly minted token should be consumable")
	}

	// Reload the devices page: mintSetupToken rotates (used=true) and mints a
	// fresh credential. The claimed credential must survive.
	if _, err := s.mintSetupToken(t.Context(), "org-1", "proj-1", "Project One"); err != nil {
		t.Fatal(err)
	}

	idx := fakeAPITokenFind(f.apiTokens, claimedID)
	if idx < 0 {
		t.Fatalf("claimed credential %s disappeared from the store", claimedID)
	}
	if f.apiTokens[idx].IsRevoked {
		t.Fatalf("claimed device credential %s was revoked by a page reload", claimedID)
	}
}

// TestMintSetupTokenRevokesUnclaimedOrphanOnRotation asserts the intended
// rotation behaviour: a credential minted at QR time but never claimed is
// revoked when the QR rotates (an abandoned QR must not leak a live credential).
func TestMintSetupTokenRevokesUnclaimedOrphanOnRotation(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}

	token, err := s.mintSetupToken(t.Context(), "org-1", "proj-1", "Project One")
	if err != nil {
		t.Fatal(err)
	}
	orphanID := f.apiTokens[0].ID

	// Force rotation without claiming: expire the setup token so the reuse
	// branch is skipped but used is still false.
	e, _ := s.setupTokens.get(token)
	e.CreatedAt = time.Now().Add(-11 * time.Minute)
	s.setupTokens.set(e)

	if _, err := s.mintSetupToken(t.Context(), "org-1", "proj-1", "Project One"); err != nil {
		t.Fatal(err)
	}

	idx := fakeAPITokenFind(f.apiTokens, orphanID)
	if idx < 0 {
		t.Fatalf("orphan credential %s disappeared from the store", orphanID)
	}
	if !f.apiTokens[idx].IsRevoked {
		t.Fatalf("unclaimed orphan credential %s should be revoked on rotation", orphanID)
	}
}
