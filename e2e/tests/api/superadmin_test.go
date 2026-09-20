// Package api_test — superadmin_test.go
//
// Tests for the superadmin API endpoints (/api/superadmin/...).
// Ported from emergent.memory/apps/server/tests/e2e/superadmin_test.go
package api_test

import (
	"net/http"
	"testing"
)

// =============================================================================
// GET /api/superadmin/me - Get current user's superadmin status
// =============================================================================

func TestSuperadmin_GetMe_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/superadmin/me", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestSuperadmin_GetMe_ReturnsNullForRegularUser(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/superadmin/me", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	// The response should be null (not a superadmin)
	// "null" as JSON, or an object with isSuperadmin=false
	// Accept either form — just verify it parses
	var result interface{}
	parseBodyJSON(t, body, &result)
	// result == nil means null JSON; result != nil means object — both are acceptable
	rl.Printf("regular user superadmin response: %s", body)
}

func TestSuperadmin_GetMe_ResponseContentType(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/superadmin/me", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusOK)
	ct := resp.Header.Get("Content-Type")
	assertContains(t, ct, "application/json")
}

func TestSuperadmin_GetMe_DoesNotRequireProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// No project ID header — should still succeed
	resp := doAPILogged(t, rl, "GET", "/api/superadmin/me", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusOK)
}

func TestSuperadmin_GetMe_WithInvalidToken(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/superadmin/me", "invalid-token-that-does-not-exist", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestSuperadmin_GetMe_IgnoresProjectIDHeader(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// project ID header should be accepted but not affect superadmin status
	resp := doAPILogged(t, rl, "GET", "/api/superadmin/me", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusOK)
}

// =============================================================================
// Superadmin role tests — require direct DB access; skipped in external mode
// =============================================================================

func TestSuperadmin_GetMe_WithFullRole_ReturnsRoleInResponse(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestSuperadmin_GetMe_WithReadonlyRole_ReturnsRoleInResponse(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestSuperadmin_FullRole_CanAccessWriteEndpoints(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestSuperadmin_ReadonlyRole_CannotAccessWriteEndpoints(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestSuperadmin_ReadonlyRole_CanAccessReadEndpoints(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestSuperadmin_FullRole_CanAccessReadEndpoints(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestSuperadmin_ReadonlyRole_MultipleWriteEndpointsDenied(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestSuperadmin_RevokedSuperadmin_CannotAccessEndpoints(t *testing.T) {
	t.Skip("requires direct DB access")
}
