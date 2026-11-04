// Package api_test — authinfo_test.go
//
// Tests for auth info endpoints (/api/auth/me, /api/auth/issuer).
// Ported from emergent.memory/apps/server/tests/e2e/authinfo_test.go
package api_test

import (
	"net/http"
	"testing"
)

func TestAuthInfo_MeReturnsUserInfo(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /api/auth/me")
	resp := doAPILogged(t, rl, "GET", "/api/auth/me", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	userID, ok := result["user_id"].(string)
	if !ok || userID == "" {
		t.Errorf("expected non-empty user_id string, got %v", result["user_id"])
	}
	tokenType, ok := result["type"].(string)
	if !ok || tokenType == "" {
		t.Errorf("expected non-empty type string, got %v", result["type"])
	}
	rl.Printf("/api/auth/me returned user_id=%s type=%s", userID, tokenType)
}

func TestAuthInfo_MeRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /api/auth/me without token")
	resp := doAPILogged(t, rl, "GET", "/api/auth/me", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
	rl.AssertionStep("auth/me returns 401 without auth", http.StatusUnauthorized, resp.StatusCode, nil)
}

func TestAuthInfo_IssuerIsPublic(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /api/auth/issuer (no auth)")
	resp := doAPILogged(t, rl, "GET", "/api/auth/issuer", "", "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if _, ok := result["standalone"]; !ok {
		t.Errorf("expected 'standalone' field in /api/auth/issuer response")
	}
	rl.Printf("/api/auth/issuer returned standalone=%v", result["standalone"])
}
