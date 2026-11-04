// Package api_test — auth_test.go
//
// Tests for authentication and authorization behavior.
// Ported from emergent.memory/apps/server/tests/e2e/auth_test.go
// Note: tests using /api/test/* endpoints (in-process only) are omitted.
package api_test

import (
	"net/http"
	"testing"
)

// TestAuth_MissingTokenReturns401 verifies that protected endpoints return 401
// when no Authorization header / X-API-Key is provided.
func TestAuth_MissingTokenReturns401(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /api/auth/me without token")
	resp := doAPILogged(t, rl, "GET", "/api/auth/me", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

// TestAuth_InvalidTokenReturns401 verifies a random invalid token is rejected.
func TestAuth_InvalidTokenReturns401(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/auth/me", "random-invalid-token", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

// TestAuth_ValidTokenAuthenticates verifies a valid token returns 200 on /api/auth/me.
func TestAuth_ValidTokenAuthenticates(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/auth/me", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["user_id"] == "" || result["user_id"] == nil {
		t.Error("expected non-empty user_id")
	}
	rl.Printf("authenticated OK, user_id=%v", result["user_id"])
}

// TestAuth_APIToken_ValidAuthenticates verifies a project-scoped API token (emt_*)
// can authenticate against the server.
func TestAuth_APIToken_ValidAuthenticates(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	_, rawToken := createToken(t, projectID, uniqueName("auth-test"), []string{"data:read"})

	resp := doAPILogged(t, rl, "GET", "/api/auth/me", rawToken, "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["user_id"] == "" || result["user_id"] == nil {
		t.Error("expected non-empty user_id")
	}
	rl.Printf("API token auth OK")
}

// TestAuth_APIToken_InvalidReturns401 verifies a non-existent emt_ token returns 401.
func TestAuth_APIToken_InvalidReturns401(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/auth/me", "emt_nonexistent_token00000000000000000000000000000000000000000000000000", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

// TestAuth_APIToken_RevokedReturns401 verifies a revoked emt_ token returns 401.
func TestAuth_APIToken_RevokedReturns401(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name": uniqueName("revoke-auth"), "scopes": []string{"data:read"},
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, body, &created)
	tokenID, _ := created["id"].(string)
	rawToken, _ := created["token"].(string)

	doAPILogged(t, rl, "DELETE", "/api/projects/"+projectID+"/tokens/"+tokenID, e2eTestToken(), "", nil)

	resp2 := doAPILogged(t, rl, "GET", "/api/auth/me", rawToken, "", nil)
	mustStatus(t, resp2, http.StatusUnauthorized)
	rl.Printf("revoked token correctly rejected")
}

// TestAuth_ScopedToken_EnforcesScope verifies that a token with limited scopes
// gets 403 on endpoints requiring other scopes.
func TestAuth_ScopedToken_EnforcesScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	// Create a token with only schema:read — no documents:read
	_, schemaToken := createToken(t, projectID, uniqueName("schema-only"), []string{"schema:read"})

	rl.Section("GET /api/documents with schema:read-only token")
	resp := doAPILogged(t, rl, "GET", "/api/documents", schemaToken, projectID, nil)
	// Should get 403 (has auth, but missing documents:read scope)
	mustStatus(t, resp, http.StatusForbidden)
	rl.AssertionStep("documents rejects schema-only token", http.StatusForbidden, resp.StatusCode, nil)
}

// TestAuth_Documents_MissingAuthReturns401 verifies POST /api/documents requires auth.
func TestAuth_Documents_MissingAuthReturns401(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPIWithOrg(t, "POST", "/api/documents", "", projectID, "", jsonBody(map[string]any{
		"filename": "test.txt",
		"content":  "test content",
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

// TestAuth_Documents_MissingProjectHeaderReturns400 verifies POST /api/documents
// requires X-Project-ID header.
func TestAuth_Documents_MissingProjectHeaderReturns400(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), "", jsonBody(map[string]any{
		"filename": "test.txt",
		"content":  "test content",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

// TestAuth_Documents_LimitedScopeReturns403 verifies a token missing documents:write
// is rejected with 403 on POST /api/documents.
func TestAuth_Documents_LimitedScopeReturns403(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	_, schemaToken := createToken(t, projectID, uniqueName("schema-scope"), []string{"schema:read"})

	resp := doAPILogged(t, rl, "POST", "/api/documents", schemaToken, projectID, jsonBody(map[string]any{
		"filename": "test.txt",
		"content":  "test content",
	}))
	mustStatus(t, resp, http.StatusForbidden)
	rl.AssertionStep("documents:write rejects limited-scope token", http.StatusForbidden, resp.StatusCode, nil)
}

// TestAuth_CachedIntrospection verifies repeated requests with the same token all succeed.
func TestAuth_CachedIntrospection(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	_, rawToken := createToken(t, projectID, uniqueName("cache-test"), []string{"data:read"})

	rl.Section("repeated requests with same token")
	for i := 0; i < 3; i++ {
		resp := doAPILogged(t, rl, "GET", "/api/auth/me", rawToken, "", nil)
		body := mustStatus(t, resp, http.StatusOK)
		var result map[string]any
		parseBodyJSON(t, body, &result)
		if result["user_id"] == "" || result["user_id"] == nil {
			t.Errorf("request %d: expected non-empty user_id", i+1)
		}
	}
	rl.Printf("3 repeated requests all authenticated consistently")
}
